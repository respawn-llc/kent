package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"

	"core/internal/testharness/httpclient"
	"core/server/llm"
	"core/server/session"
	"core/server/session/sessiontest"
	"core/server/tools"
	"core/shared/config"
	"core/shared/modelcontract"
	"core/shared/textutil"
	"core/shared/toolspec"
)

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
	client := nativeResponseTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, err := fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_native\",\"status\":\"completed\",\"output\":[%s],\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}}}\n\n", native)
		if err != nil {
			t.Error(err)
		}
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
	client := nativeResponseTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Input []json.RawMessage `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		output := bytes.Join(nativeBytes(native), []byte(","))
		if calls.Add(1) > 1 {
			next := 0
			for _, raw := range request.Input {
				var item struct {
					ID     string `json:"id"`
					Type   string `json:"type"`
					CallID string `json:"call_id"`
				}
				if err := json.Unmarshal(raw, &item); err != nil {
					t.Error(err)
				}
				if next < len(native) && bytes.Equal(raw, native[next]) {
					next++
				}
				if item.Type == "function_call_output" && item.CallID == "call_native" {
					toolResults.Add(1)
				}
			}
			if next != len(native) {
				t.Errorf("next provider request retained %d/%d native items", next, len(native))
			}
			output = []byte(`{"type":"message","id":"msg_final","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"done","annotations":[]}]}`)
		} else {
			if _, err := fmt.Fprintf(w, "data: {\"type\":\"response.output_item.added\",\"output_index\":2,\"item\":%s}\n\ndata: {\"type\":\"response.output_item.added\",\"output_index\":3,\"item\":%s}\n\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":3,\"delta\":\"after the call\"}\n\n", native[2], native[3]); err != nil {
				t.Error(err)
			}
		}
		_, err := fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_native\",\"status\":\"completed\",\"output\":[%s],\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}}}\n\n", output)
		if err != nil {
			t.Error(err)
		}
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

func TestNativeOutputPresentationKeepsInterleavedHostedCalls(t *testing.T) {
	t.Parallel()
	output := []json.RawMessage{
		json.RawMessage(`{"type":"message","id":"before","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"before","annotations":[]}]}`),
		json.RawMessage(`{"type":"reasoning","id":"rs_middle","summary":[{"type":"summary_text","text":"consider search"}]}`),
		json.RawMessage(`{"type":"web_search_call","id":"search_native","status":"completed","action":{"type":"search","query":"sample"}}`),
		json.RawMessage(`{"type":"message","id":"after","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"after","annotations":[]}]}`),
		json.RawMessage(`{"type":"message","id":"final","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"final","annotations":[]}]}`),
	}
	client := nativeResponseTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, err := fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ordered\",\"status\":\"completed\",\"output\":[%s],\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}}}\n\n", bytes.Join(nativeBytes(output), []byte(",")))
		if err != nil {
			t.Error(err)
		}
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
	client := nativeResponseTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		output := opaque
		if calls.Add(1) > 1 {
			var request struct {
				Input []json.RawMessage `json:"input"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if !slices.ContainsFunc(request.Input, func(item json.RawMessage) bool { return bytes.Equal(item, opaque) }) {
				t.Error("invisible native output missing from next request")
			}
			output = final
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, err := fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_native\",\"status\":\"completed\",\"output\":[%s],\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}}}\n\n", output)
		if err != nil {
			t.Error(err)
		}
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
	client := nativeResponseTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for index, raw := range output {
			if _, err := fmt.Fprintf(w, "data: {\"type\":\"response.output_item.added\",\"output_index\":%d,\"item\":%s}\n\n", index, raw); err != nil {
				t.Error(err)
			}
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
				if _, err := fmt.Fprintf(w, "data: {\"type\":\"response.output_text.delta\",\"output_index\":%d,\"delta\":%s}\n\n", index, delta); err != nil {
					t.Error(err)
				}
			}
			if index == 0 {
				continue
			}
			if index == 2 {
				if _, err := fmt.Fprintf(w, "data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"delta\":\" complete\"}\n\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":%s}\n\n", output[0]); err != nil {
					t.Error(err)
				}
			}
			if _, err := fmt.Fprintf(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":%d,\"item\":%s}\n\n", index, raw); err != nil {
				t.Error(err)
			}
		}
		if _, err := fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_streamed\",\"status\":\"completed\",\"output\":[%s],\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}}}\n\n", bytes.Join(nativeBytes(output), []byte(","))); err != nil {
			t.Error(err)
		}
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
