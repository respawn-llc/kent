package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"core/cli/tui/ongoing"
	"core/internal/testharness/httpclient"
	"core/internal/testharness/runtimewirefixture"
	"core/server/llm"
	"core/server/session"
	"core/server/session/sessiontest"
	"core/server/tools"
	patchtool "core/server/tools/patch"
	"core/shared/config"
	"core/shared/modelcontract"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/textutil"
	"core/shared/toolspec"
	"github.com/coder/websocket"
)

func nativeResponsesSocketHandler(t *testing.T, respond func(json.RawMessage) []string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer socket.CloseNow()
		for {
			_, raw, err := socket.Read(t.Context())
			if err != nil {
				return
			}
			for _, event := range respond(raw) {
				if err := socket.Write(t.Context(), websocket.MessageText, []byte(event)); err != nil {
					return
				}
			}
		}
	})
}

func nativeResponseTestClient(t *testing.T, handler http.Handler) llm.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	registration, err := llm.ResolveConnectionVariant(config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value("https://api.openai.com/v1")})
	if err != nil {
		t.Fatal(err)
	}
	client, err := llm.NewProviderClient(llm.ProviderClientOptions{
		Registration: registration, Auth: transportStaticAuth{},
		HTTPClient:          &http.Client{Transport: httpclient.NewURLRewriteTransport(target, server.Client().Transport, "")},
		ContextWindowTokens: 200_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestAcceptedNativeAnswerContinuesWithoutReconstruction(t *testing.T) {
	t.Parallel()
	native := json.RawMessage(`{"type":"message","id":"msg_native","role":"assistant","phase":"final_answer","status":"completed","content":[{"type":"output_text","text":"<answer>&","annotations":[]}]}`)
	var calls atomic.Int32
	client := nativeResponseTestClient(t, nativeResponsesSocketHandler(t, func(raw json.RawMessage) []string {
		if calls.Add(1) == 2 {
			var create struct {
				Input    []json.RawMessage `json:"input"`
				Previous *string           `json:"previous_response_id"`
			}
			if err := json.Unmarshal(raw, &create); err != nil {
				t.Error(err)
			}
			if create.Previous == nil || *create.Previous != "resp_native" || len(create.Input) != 1 {
				t.Errorf("accepted answer was retransmitted rather than continued: %s", raw)
			}
		}
		return []string{fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_native","status":"completed","output":[%s],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`, native)}
	}))
	store := mustCreateTestSession(t)
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-6-sol"})
	if _, err := engine.SubmitUserMessage(t.Context(), "first"); err != nil {
		t.Fatal(err)
	}
	assertNative := func(engine *Engine) {
		t.Helper()
		request := buildActiveTurnRequestForTest(t, engine, nil, true)
		for _, item := range request.Items {
			if item.Role != nil && *item.Role == llm.RoleAssistant && item.Content != nil && *item.Content == "<answer>&" {
				if !bytes.Equal(item.Raw, native) {
					t.Fatalf("accepted output reconstructed:\n got %s\nwant %s", item.Raw, native)
				}
				return
			}
		}
		t.Fatal("accepted native answer missing from prepared context")
	}
	assertNative(engine)
	if _, err := engine.SubmitUserMessage(t.Context(), "next human turn"); err != nil {
		t.Fatal(err)
	}
	assertNative(engine)
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), client, tools.NewRegistry(), Config{Model: "gpt-6-sol"})
	assertNative(reopened)
}

func TestAcceptedNativeReasoningToolOutputSurvivesResultsAndReload(t *testing.T) {
	t.Parallel()
	native := []json.RawMessage{
		json.RawMessage(`{"type":"reasoning","id":"rs_native","summary":[{"type":"summary_text","text":"<plan>&"}],"encrypted_content":"opaque_ciphertext"}`),
		json.RawMessage(`{"type":"future_provider_item","id":"opaque_native","data":{"text":"<opaque>&"}}`),
		json.RawMessage(`{"type":"function_call","id":"item_native","call_id":"call_native","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}","status":"completed"}`),
		json.RawMessage(`{"type":"message","id":"after_call","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"after the call","annotations":[]}]}`),
	}
	var calls atomic.Int32
	var toolResults atomic.Int32
	client := nativeResponseTestClient(t, nativeResponsesSocketHandler(t, func(raw json.RawMessage) []string {
		var request struct {
			Input    []json.RawMessage `json:"input"`
			Previous *string           `json:"previous_response_id"`
		}
		if err := json.Unmarshal(raw, &request); err != nil {
			t.Error(err)
			return nil
		}
		var events []string
		output := bytes.Join(nativeBytes(native), []byte(","))
		if calls.Add(1) > 1 {
			for _, raw := range request.Input {
				var item struct {
					ID     string `json:"id"`
					Type   string `json:"type"`
					CallID string `json:"call_id"`
				}
				if err := json.Unmarshal(raw, &item); err != nil {
					t.Error(err)
				}
				if item.Type == "function_call_output" && item.CallID == "call_native" {
					toolResults.Add(1)
				}
			}
			if request.Previous == nil || *request.Previous != "resp_native" || len(request.Input) != 1 {
				t.Errorf("tool continuation retransmitted provider output: %s", raw)
			}
			output = []byte(`{"type":"message","id":"msg_final","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"done","annotations":[]}]}`)
		} else {
			events = append(events,
				fmt.Sprintf(`{"type":"response.output_item.added","output_index":2,"item":%s}`, native[2]),
				fmt.Sprintf(`{"type":"response.output_item.added","output_index":3,"item":%s}`, native[3]),
				`{"type":"response.output_text.delta","output_index":3,"delta":"after the call"}`)
		}
		return append(events, fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_native","status":"completed","output":[%s],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`, output))
	}))
	store := mustCreateTestSession(t)
	registry := newTestToolRegistry(t, tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: fakeTool{name: toolspec.ToolExecCommand}})
	var startedCalls []llm.ToolCall
	var prematureCommentary bool
	cfg := Config{Model: "gpt-6-sol", EnabledTools: []toolspec.ID{toolspec.ToolExecCommand},
		OnEvent: func(event Event) {
			if event.Kind == EventAssistantDelta && event.AssistantStreamMetadata != nil && event.AssistantStreamMetadata.OutputIndex != nil && *event.AssistantStreamMetadata.OutputIndex == 3 {
				prematureCommentary = true
			}
			if event.Kind == EventToolCallStarted && event.ToolCall != nil {
				startedCalls = append(startedCalls, *event.ToolCall)
			}
		},
	}
	engine := mustNewTestEngine(t, store, client, registry, cfg)
	if _, err := engine.SubmitUserMessage(t.Context(), "use the tool"); err != nil {
		t.Fatal(err)
	}
	if toolResults.Load() != 1 {
		t.Fatalf("actual tool results sent = %d", toolResults.Load())
	}
	if prematureCommentary {
		t.Fatal("commentary after an unaccepted local call appeared before acceptance")
	}
	if len(startedCalls) != 1 || startedCalls[0].OutputIndex == nil || *startedCalls[0].OutputIndex != 2 {
		t.Fatalf("local tool start lost its native position: %+v", startedCalls)
	}
	records, err := sessiontest.CollectRecords(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		payload, err := record.Payload()
		if err != nil {
			t.Fatal(err)
		}
		if local, ok := payload.(session.LocalEntryRecord); ok && local.Role == "reasoning" {
			t.Fatal("native reasoning was saved again as a local text entry")
		}
	}
	assertNative := func(engine *Engine) {
		t.Helper()
		next := 0
		for _, item := range buildActiveTurnRequestForTest(t, engine, nil, true).Items {
			if next < len(native) && bytes.Equal(item.Raw, native[next]) {
				if item.Type == llm.ResponseItemTypeReasoning && (item.Attribution == nil || item.Attribution.Type == nil || *item.Attribution.Type != modelcontract.ReasoningTypeOpenAI) {
					t.Fatal("native reasoning lost its producing-provider attribution")
				}
				next++
			}
		}
		if next != len(native) {
			t.Fatalf("prepared context retained %d/%d native items", next, len(native))
		}
	}
	assertNative(engine)
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), client, registry, cfg)
	assertNative(reopened)
}

func nativeBytes(items []json.RawMessage) [][]byte {
	result := make([][]byte, len(items))
	for i, item := range items {
		result[i] = item
	}
	return result
}

func TestNativeCustomPatchExecutesOnceAndContinuesWithOneResult(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	patch, err := patchtool.New(runtimewirefixture.FilesystemContext(t, workspace))
	if err != nil {
		t.Fatal(err)
	}
	patchInput := "*** Begin Patch\n*** Add File: marker.txt\n+marker\n*** End Patch\n"
	raw, err := json.Marshal(map[string]any{
		"type": "custom_tool_call", "id": "patch_item", "call_id": "patch_call",
		"name": "patch", "input": patchInput,
	})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	var started atomic.Int32
	client := nativeResponseTestClient(t, nativeResponsesSocketHandler(t, func(create json.RawMessage) []string {
		if calls.Add(1) == 1 {
			return []string{runtimeCompletedResponseEvent([]byte("[" + string(raw) + "]"))}
		}
		var request struct {
			Input []struct {
				Type   string `json:"type"`
				CallID string `json:"call_id"`
			} `json:"input"`
		}
		if err := json.Unmarshal(create, &request); err != nil {
			t.Error(err)
		}
		if len(request.Input) != 1 || request.Input[0].Type != "custom_tool_call_output" || request.Input[0].CallID != "patch_call" {
			t.Errorf("custom patch continuation lost its single result: %s", create)
		}
		return []string{runtimeFinalAnswerEvent("done")}
	}))
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client,
		newTestToolRegistry(t, tools.HandlerRegistration{ID: toolspec.ToolPatch, Handler: patch}),
		Config{Model: "gpt-6-sol", TranscriptWorkingDir: workspace, EnabledTools: []toolspec.ID{toolspec.ToolPatch}, OnEvent: func(event Event) {
			if event.Kind == EventToolCallStarted && event.ToolCall != nil && event.ToolCall.ID == "patch_call" {
				started.Add(1)
			}
		}})
	if _, err := engine.SubmitUserMessage(t.Context(), "patch"); err != nil {
		t.Fatal(err)
	}
	if started.Load() != 1 || calls.Load() != 2 {
		t.Fatalf("custom patch starts=%d requests=%d", started.Load(), calls.Load())
	}
	marker, err := os.ReadFile(filepath.Join(workspace, "marker.txt"))
	if err != nil || string(marker) != "marker\n" {
		t.Fatalf("custom patch effect=%q err=%v", marker, err)
	}
}

func TestStopCancelsResponsesSocketRetry(t *testing.T) {
	withGenerateRetryDelays(t, []time.Duration{0})
	retrying := make(chan struct{})
	disconnected := make(chan struct{})
	var openings atomic.Int32
	client := nativeResponseTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer socket.CloseNow()
		if _, _, err := socket.Read(t.Context()); err != nil {
			return
		}
		if openings.Add(1) == 1 {
			_ = socket.Write(t.Context(), websocket.MessageText, []byte(`{"type":"error","status":503,"error":{"code":"server_error","message":"retry"}}`))
			return
		}
		close(retrying)
		_, _, err = socket.Read(t.Context())
		if err == nil {
			t.Error("retry socket did not terminate")
		}
		close(disconnected)
	}))
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{Model: "gpt-6-sol"})
	result := make(chan error, 1)
	go func() {
		_, err := engine.SubmitUserMessage(t.Context(), "retry then stop")
		result <- err
	}()
	select {
	case <-retrying:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	if interrupted, err := engine.TryInterruptActiveRun(); err != nil || !interrupted {
		t.Fatalf("retry live Stop interrupted=%t err=%v", interrupted, err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("retry Stop result = %v", err)
	}
	<-disconnected
}

func TestNativeStreamOnlyReasoningRemainsReadableAfterReload(t *testing.T) {
	t.Parallel()
	native := json.RawMessage(`{"type":"reasoning","id":"rs_empty","summary":[],"encrypted_content":"opaque"}`)
	answer := json.RawMessage(`{"type":"message","id":"msg_answer","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"done","annotations":[]}]}`)
	client := nativeResponseTestClient(t, nativeResponsesSocketHandler(t, func(json.RawMessage) []string {
		return []string{
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_empty","summary":[]}}`,
			`{"type":"response.reasoning_summary_text.delta","output_index":0,"summary_index":0,"item_id":"rs_empty","delta":"visual only"}`,
			fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_empty","status":"completed","output":[%s,%s]}}`, native, answer),
		}
	}))
	store := mustCreateTestSession(t)
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-6-sol"})
	if _, err := engine.SubmitUserMessage(t.Context(), "first"); err != nil {
		t.Fatal(err)
	}
	check := func(engine *Engine) {
		t.Helper()
		mustEngineNewestSegmentPage(t, engine)
		var reasoning []llm.ResponseItem
		for _, item := range buildActiveTurnRequestForTest(t, engine, nil, true).Items {
			if item.Type == llm.ResponseItemTypeReasoning {
				reasoning = append(reasoning, item)
			}
		}
		if len(reasoning) != 1 || !bytes.Equal(reasoning[0].Raw, native) {
			t.Fatalf("stream-only summary changed native API input: %+v", reasoning)
		}
	}
	check(engine)
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	check(mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), client, tools.NewRegistry(), Config{Model: "gpt-6-sol"}))
}

func TestNativeLeadingWhitespacePresentationSurvivesReload(t *testing.T) {
	t.Parallel()
	output := []json.RawMessage{
		json.RawMessage(`{"type":"message","id":"msg_first","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"\n\nHello world","annotations":[]}]}`),
		json.RawMessage(`{"type":"message","id":"msg_second","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"\n  unchanged","annotations":[]}]}`),
	}
	client := nativeResponseTestClient(t, nativeResponsesSocketHandler(t, func(json.RawMessage) []string {
		return []string{
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_first","role":"assistant","phase":"commentary","content":[]}}`,
			`{"type":"response.output_text.delta","output_index":0,"delta":"\n\nHello"}`,
			`{"type":"response.output_text.delta","output_index":0,"delta":" world"}`,
			fmt.Sprintf(`{"type":"response.output_item.done","output_index":0,"item":%s}`, output[0]),
			fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_whitespace","status":"completed","output":[%s]}}`, bytes.Join(nativeBytes(output), []byte(","))),
		}
	}))
	store := mustCreateTestSession(t)
	var streamed string
	surface := ongoing.NewSurface(&bytes.Buffer{})
	frame := ongoing.FrameInput{Size: ongoing.Size{Width: 80, Height: 24}}
	apply := func(event *transcriptpb.Event) {
		t.Helper()
		if _, err := surface.ApplyTerminalMessage(&transcriptpb.Message{Event: event}, frame); err != nil {
			t.Fatal(err)
		}
	}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model: "gpt-6-sol", OnEvent: func(event Event) {
			if event.Kind == EventAssistantDelta {
				streamed += event.AssistantDelta
				apply(&transcriptpb.Event{Payload: &transcriptpb.Event_AssistantDelta{AssistantDelta: &transcriptpb.AssistantDelta{
					StreamId: event.AssistantTranscriptStreamID.String(), Delta: event.AssistantDelta,
					Phase: transcriptpb.AssistantPhase_ASSISTANT_PHASE_COMMENTARY,
				}}})
			}
			for _, row := range TranscriptCommittedRowFactsFromEvent(event) {
				if row.Assistant != nil && row.Assistant.StreamID != nil {
					id := row.Assistant.StreamID.String()
					apply(&transcriptpb.Event{Payload: &transcriptpb.Event_CommittedRow{CommittedRow: &transcriptpb.CommittedRow{
						Row: &transcriptpb.CommittedRow_Assistant{Assistant: &transcriptpb.AssistantRow{
							StreamId: &id, Text: row.Assistant.Text, Phase: transcriptpb.AssistantPhase_ASSISTANT_PHASE_COMMENTARY,
						}},
					}}})
				}
			}
		},
	})
	if _, err := engine.SubmitUserMessage(t.Context(), "first"); err != nil {
		t.Fatal(err)
	}
	check := func(engine *Engine) {
		t.Helper()
		var answers []string
		for _, row := range TranscriptCommittedRowFactsFromSnapshot(mustEngineNewestSegmentPage(t, engine).Snapshot) {
			if row.Assistant != nil {
				answers = append(answers, row.Assistant.Text)
			}
		}
		if !reflect.DeepEqual(answers, []string{streamed, "\n  unchanged"}) {
			t.Fatalf("native per-item display changed streamed prefix or unstreamed whitespace: %q", answers)
		}
		next := 0
		for _, item := range buildActiveTurnRequestForTest(t, engine, nil, true).Items {
			if next < len(output) && bytes.Equal(item.Raw, output[next]) {
				next++
			}
		}
		if next != len(output) {
			t.Fatal("presentation changed native API bytes")
		}
	}
	check(engine)
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	check(mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), client, tools.NewRegistry(), Config{Model: "gpt-6-sol"}))
}

func TestNativeOutputPresentationKeepsInterleavedHostedCalls(t *testing.T) {
	t.Parallel()
	output := []json.RawMessage{
		json.RawMessage(`{"type":"message","id":"before","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"before","annotations":[]}]}`),
		json.RawMessage(`{"type":"reasoning","id":"rs_middle","summary":[{"type":"summary_text","text":"consider search"}]}`),
		json.RawMessage(`{"type":"web_search_call","id":"search_native","status":"completed","action":{"type":"search","query":"sample"}}`),
		json.RawMessage(`{"type":"message","id":"after","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"after","annotations":[]}]}`),
		json.RawMessage(`{"type":"message","id":"final","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"final","annotations":[]}]}`),
	}
	client := nativeResponseTestClient(t, nativeResponsesSocketHandler(t, func(json.RawMessage) []string {
		return []string{fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_ordered","status":"completed","output":[%s],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`, bytes.Join(nativeBytes(output), []byte(",")))}
	}))
	store := mustCreateTestSession(t)
	cfg := Config{Model: "gpt-6-sol", WebSearchMode: "native", EnabledTools: []toolspec.ID{toolspec.ToolWebSearch}}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), cfg)
	if _, err := engine.SubmitUserMessage(t.Context(), "search"); err != nil {
		t.Fatal(err)
	}
	assertOrder := func(engine *Engine) {
		t.Helper()
		var actual []string
		snapshot := mustEngineNewestSegmentPage(t, engine).Snapshot
		for _, entry := range snapshot.Entries {
			if entry.Role == "assistant" {
				actual = append(actual, entry.Text)
			} else if entry.Role == "reasoning" {
				actual = append(actual, "reasoning")
			} else if entry.ToolCallID == "search_native" && entry.Role == "tool_call" {
				actual = append(actual, entry.ToolCallID)
			}
		}
		if !reflect.DeepEqual(actual, []string{"before", "reasoning", "search_native", "after", "final"}) {
			t.Fatalf("provider item presentation order changed: %v", actual)
		}
		var committedOrder []string
		for _, row := range TranscriptCommittedRowFactsFromSnapshot(snapshot) {
			if row.Assistant != nil {
				committedOrder = append(committedOrder, row.Assistant.Text)
			} else if row.Tool != nil {
				committedOrder = append(committedOrder, row.Tool.ToolCallID)
			} else if row.ReasoningTrace != nil {
				committedOrder = append(committedOrder, "reasoning")
			}
		}
		if !reflect.DeepEqual(committedOrder, actual) {
			t.Fatalf("client-facing committed projection changed native order: %v", committedOrder)
		}
		if err := engine.WithTranscriptHydrationSnapshot(func(snapshot TranscriptHydrationSnapshot) error {
			var hydrated []string
			for _, row := range snapshot.CommittedRows {
				if row.Assistant != nil {
					hydrated = append(hydrated, row.Assistant.Text)
				} else if row.Tool != nil {
					hydrated = append(hydrated, row.Tool.ToolCallID)
				} else if row.ReasoningTrace != nil {
					hydrated = append(hydrated, "reasoning")
				}
			}
			if !reflect.DeepEqual(hydrated, actual) {
				return fmt.Errorf("hydrated committed projection changed native order: %v", hydrated)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		items := buildActiveTurnRequestForTest(t, engine, nil, true).Items
		next := 0
		for _, item := range items {
			if next < len(output) && bytes.Equal(item.Raw, output[next]) {
				next++
			}
		}
		if next != len(output) {
			t.Fatalf("native output retained %d/%d ordered items", next, len(output))
		}
	}
	assertOrder(engine)
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), client, tools.NewRegistry(), cfg)
	assertOrder(reopened)
}

func TestAcceptedInvisibleNativeOutputContinuesWithoutVisibleRows(t *testing.T) {
	t.Parallel()
	opaque := json.RawMessage(`{"type":"future_provider_item","id":"invisible_native","data":"<opaque>&"}`)
	final := json.RawMessage(`{"type":"message","id":"final","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"done","annotations":[]}]}`)
	var calls atomic.Int32
	client := nativeResponseTestClient(t, nativeResponsesSocketHandler(t, func(raw json.RawMessage) []string {
		output := opaque
		if calls.Add(1) > 1 {
			var request struct {
				Input    []json.RawMessage `json:"input"`
				Previous *string           `json:"previous_response_id"`
			}
			if err := json.Unmarshal(raw, &request); err != nil {
				t.Error(err)
			}
			if request.Previous == nil || *request.Previous != "resp_native" || len(request.Input) != 0 {
				t.Errorf("invisible native output not retained in provider prefix: %s", raw)
			}
			output = final
		}
		return []string{fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_native","status":"completed","output":[%s],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`, output)}
	}))
	store := mustCreateTestSession(t)
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-6-sol"})
	if _, err := engine.SubmitUserMessage(t.Context(), "continue"); err != nil {
		t.Fatal(err)
	}
	assertInvisible := func(engine *Engine) {
		t.Helper()
		items := buildActiveTurnRequestForTest(t, engine, nil, true).Items
		if !slices.ContainsFunc(items, func(item llm.ResponseItem) bool { return bytes.Equal(item.Raw, opaque) }) {
			t.Fatal("invisible native output was not saved")
		}
		var assistantRows int
		for _, row := range hydrationSnapshot(t, engine).CommittedRows {
			if row.Assistant != nil {
				assistantRows++
			}
		}
		if assistantRows != 1 {
			t.Fatalf("invisible output created an assistant row: %d", assistantRows)
		}
	}
	assertInvisible(engine)
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), client, tools.NewRegistry(), Config{Model: "gpt-6-sol"})
	assertInvisible(reopened)
}

func TestNativeStreamingKeepsMessageIdentityAcrossHostedCalls(t *testing.T) {
	t.Parallel()
	output := []json.RawMessage{
		json.RawMessage(`{"type":"message","id":"before","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"before complete","annotations":[]}]}`),
		json.RawMessage(`{"type":"web_search_call","id":"search_native","status":"completed","action":{"type":"search","query":"sample"}}`),
		json.RawMessage(`{"type":"message","id":"after","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"after","annotations":[]}]}`),
		json.RawMessage(`{"type":"message","id":"final","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"final","annotations":[]}]}`),
	}
	client := nativeResponseTestClient(t, nativeResponsesSocketHandler(t, func(json.RawMessage) []string {
		var events []string
		for index, raw := range output {
			events = append(events, fmt.Sprintf(`{"type":"response.output_item.added","output_index":%d,"item":%s}`, index, raw))
			var item struct {
				Type    string `json:"type"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			if err := json.Unmarshal(raw, &item); err != nil {
				t.Error(err)
			}
			if item.Type == "message" {
				text := item.Content[0].Text
				if index == 0 {
					text = "before"
				}
				delta, err := json.Marshal(text)
				if err != nil {
					t.Error(err)
				}
				events = append(events, fmt.Sprintf(`{"type":"response.output_text.delta","output_index":%d,"delta":%s}`, index, delta))
			}
			if index == 0 {
				continue
			}
			if index == 2 {
				events = append(events, `{"type":"response.output_text.delta","output_index":0,"delta":" complete"}`,
					fmt.Sprintf(`{"type":"response.output_item.done","output_index":0,"item":%s}`, output[0]))
			}
			events = append(events, fmt.Sprintf(`{"type":"response.output_item.done","output_index":%d,"item":%s}`, index, raw))
		}
		return append(events, fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_streamed","status":"completed","output":[%s],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`, bytes.Join(nativeBytes(output), []byte(","))))
	}))
	var events []Event
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{
		Model: "gpt-6-sol", WebSearchMode: "native", EnabledTools: []toolspec.ID{toolspec.ToolWebSearch},
		OnEvent: func(event Event) { events = append(events, event) },
	})
	if _, err := engine.SubmitUserMessage(t.Context(), "search"); err != nil {
		t.Fatal(err)
	}
	var streams []string
	var timeline []EventKind
	var acceptedRows []TranscriptCommittedRowFact
	for _, event := range events {
		if event.Kind == EventAssistantMessage && event.Message.NativeResponse != nil {
			acceptedRows = append(acceptedRows, TranscriptCommittedRowFactsFromEvent(event)...)
		}
		if event.Kind == EventAssistantDelta {
			if event.AssistantTranscriptStreamID == nil {
				t.Fatal("assistant delta has no stream identity")
			}
			streams = append(streams, event.AssistantTranscriptStreamID.String())
			timeline = append(timeline, event.Kind)
		} else if event.Kind == EventToolCallStarted && event.ToolCall != nil && event.ToolCall.ID == "search_native" {
			timeline = append(timeline, event.Kind)
		}
	}
	if len(streams) != 4 || streams[0] != streams[2] || streams[0] == streams[1] || streams[1] == streams[3] || streams[0] == streams[3] {
		t.Fatalf("separate provider messages share a streaming identity: %v", streams)
	}
	if len(timeline) < 4 || timeline[0] != EventAssistantDelta || timeline[1] != EventToolCallStarted || timeline[2] != EventAssistantDelta {
		t.Fatalf("hosted call moved out of provider streaming order: %v", timeline)
	}
	if len(acceptedRows) != len(output) {
		t.Fatalf("accepted native response delivered %d of %d visible items before result delivery", len(acceptedRows), len(output))
	}
	if acceptedRows[1].ToolCall == nil || acceptedRows[1].ToolCall.ToolCallID != "search_native" {
		t.Fatal("accepted native call lost its original source slot")
	}
	var resultLocator *TranscriptCommittedRowFact
	for _, event := range events {
		if event.Kind != EventToolCallCompleted {
			continue
		}
		for _, row := range TranscriptCommittedRowFactsFromEvent(event) {
			if row.Tool != nil && row.Tool.ToolCallID == "search_native" {
				resultLocator = &row
			}
		}
	}
	if resultLocator == nil || resultLocator.Locator != acceptedRows[1].Locator {
		t.Fatal("native result did not update its accepted call's locator")
	}
	if err := engine.WithTranscriptHydrationSnapshot(func(snapshot TranscriptHydrationSnapshot) error {
		var hydratedStreams []string
		for _, row := range snapshot.CommittedRows {
			if row.Assistant != nil {
				if row.Assistant.StreamID == nil {
					return fmt.Errorf("native assistant lost its stream correlation")
				}
				hydratedStreams = append(hydratedStreams, row.Assistant.StreamID.String())
			}
		}
		if !reflect.DeepEqual(hydratedStreams, []string{streams[0], streams[1], streams[3]}) {
			return fmt.Errorf("native assistant correlations changed through hydration: %v", hydratedStreams)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
