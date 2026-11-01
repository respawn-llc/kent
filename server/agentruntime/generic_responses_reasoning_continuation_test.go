package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"core/server/llm"
	session "core/server/sessionstore"
	"core/server/tools"
	"core/shared/config"
	"core/shared/textutil"
	"core/shared/toolspec"
)

type genericResponsesContinuationRequest struct {
	Input              []json.RawMessage `json:"input"`
	Tools              []json.RawMessage `json:"tools"`
	PreviousResponseID json.RawMessage   `json:"previous_response_id"`
}

type genericResponsesContinuationItem struct {
	Type             string          `json:"type"`
	ID               string          `json:"id"`
	CallID           string          `json:"call_id"`
	EncryptedContent json.RawMessage `json:"encrypted_content"`
	Content          []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

type genericResponsesContinuationExpectation struct {
	typeName                  string
	id                        *string
	callID                    *string
	reasoningText             *string
	requireNoEncryptedContent bool
}

func TestGenericResponsesHTTPPreservesPlaintextReasoningAcrossSessionContinuation(t *testing.T) {
	runGenericResponsesReasoningContinuation(t, nil)
}

func TestGenericResponsesHTTPPreservesPlaintextFromMixedReasoningAcrossSessionContinuation(t *testing.T) {
	encryptedContent := "opaque encrypted reasoning"
	runGenericResponsesReasoningContinuation(t, &encryptedContent)
}

func runGenericResponsesReasoningContinuation(t *testing.T, encryptedContent *string) {
	t.Helper()
	reasoningOutput := func(id, text string) json.RawMessage {
		item := struct {
			Type    string `json:"type"`
			ID      string `json:"id"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			EncryptedContent *string `json:"encrypted_content,omitempty"`
		}{
			Type: "reasoning", ID: id,
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{{Type: "reasoning_text", Text: text}},
			EncryptedContent: encryptedContent,
		}
		raw, err := json.Marshal(item)
		if err != nil {
			t.Fatalf("encode reasoning output fixture: %v", err)
		}
		return raw
	}
	outputs := [][]json.RawMessage{
		{
			reasoningOutput("rs_first", "verbatim reasoning before first tool"),
			json.RawMessage(`{"type":"function_call","id":"fc_first","call_id":"call_first","name":"exec_command","arguments":"{\"cmd\":\"true\"}","status":"completed"}`),
		},
		{
			reasoningOutput("rs_after_tool", "verbatim reasoning after first tool"),
			json.RawMessage(`{"type":"message","id":"msg_after_tool","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"first turn complete","annotations":[]}]}`),
		},
		{
			reasoningOutput("rs_next_turn", "verbatim reasoning in next turn"),
			json.RawMessage(`{"type":"message","id":"msg_next_turn","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"second turn complete","annotations":[]}]}`),
		},
		{
			reasoningOutput("rs_reopened", "verbatim reasoning after reopen"),
			json.RawMessage(`{"type":"function_call","id":"fc_reopened","call_id":"call_reopened","name":"exec_command","arguments":"{\"cmd\":\"true\"}","status":"completed"}`),
		},
		{
			reasoningOutput("rs_reopened_tool", "verbatim reasoning after reopened tool"),
			json.RawMessage(`{"type":"message","id":"msg_reopened","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"reopened turn complete","annotations":[]}]}`),
		},
	}
	var mu sync.Mutex
	var requests []genericResponsesContinuationRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("Responses HTTP path = %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Upgrade") != "" {
			t.Error("generic Responses continuation used a WebSocket upgrade")
			http.Error(w, "WebSocket transport is not supported by this fixture", http.StatusBadRequest)
			return
		}
		var request genericResponsesContinuationRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode Responses request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, request)
		requestNumber := len(requests)
		mu.Unlock()
		if requestNumber > len(outputs) {
			t.Errorf("unexpected Responses HTTP request %d", requestNumber)
			http.Error(w, "unexpected request", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for index, item := range outputs[requestNumber-1] {
			event, err := json.Marshal(struct {
				Type        string          `json:"type"`
				OutputIndex int             `json:"output_index"`
				Item        json.RawMessage `json:"item"`
			}{Type: "response.output_item.done", OutputIndex: index, Item: item})
			if err != nil {
				t.Errorf("encode Responses output event: %v", err)
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", event); err != nil {
				t.Errorf("write Responses output event: %v", err)
				return
			}
		}
		output, err := json.Marshal(outputs[requestNumber-1])
		if err != nil {
			t.Errorf("encode completed Responses output: %v", err)
			return
		}
		completed, err := json.Marshal(struct {
			Type     string `json:"type"`
			Response struct {
				ID      string          `json:"id"`
				Status  string          `json:"status"`
				EndTurn bool            `json:"end_turn"`
				Output  json.RawMessage `json:"output"`
			} `json:"response"`
		}{
			Type: "response.completed",
			Response: struct {
				ID      string          `json:"id"`
				Status  string          `json:"status"`
				EndTurn bool            `json:"end_turn"`
				Output  json.RawMessage `json:"output"`
			}{
				ID: fmt.Sprintf("response_%d", requestNumber), Status: "completed",
				EndTurn: requestNumber != 1 && requestNumber != 4, Output: output,
			},
		})
		if err != nil {
			t.Errorf("encode Responses completed event: %v", err)
			return
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", completed); err != nil {
			t.Errorf("write Responses completed event: %v", err)
		}
	}))
	defer server.Close()

	registration, err := llm.ResolveConnectionVariant(config.ProviderConnection{
		Protocol: config.ConnectionResponses,
		Endpoint: textutil.Value(server.URL + "/v1"),
	})
	if err != nil {
		t.Fatalf("resolve generic Responses connection: %v", err)
	}
	client, err := llm.NewProviderClient(llm.ProviderClientOptions{
		Registration:        registration,
		Auth:                transportStaticAuth{},
		HTTPClient:          server.Client(),
		Model:               "gpt-6-sol",
		ContextWindowTokens: 272_000,
	})
	if err != nil {
		t.Fatalf("create generic Responses HTTP client: %v", err)
	}
	capabilities, err := client.(llm.ProviderCapabilitiesClient).ProviderCapabilities(t.Context())
	if err != nil {
		t.Fatalf("resolve generic Responses capabilities: %v", err)
	}
	if capabilities.ProviderID != "openai-compatible" || !capabilities.SupportsResponsesAPI || capabilities.SupportsReasoningEncrypted {
		t.Fatalf("Responses fixture is not a generic HTTP connection without encrypted-reasoning support: %+v", capabilities)
	}

	registry := newTestToolRegistry(t, tools.HandlerRegistration{
		ID: toolspec.ToolExecCommand, Handler: fakeTool{name: toolspec.ToolExecCommand},
	})
	cfg := Config{
		Model:         "gpt-6-sol",
		ThinkingLevel: "high",
		EnabledTools:  []toolspec.ID{toolspec.ToolExecCommand},
	}
	store := mustCreateTestSession(t)
	engine := mustNewTestEngine(t, store, client, registry, cfg)
	for _, text := range []string{"initial tool turn", "continue the same Session"} {
		answer, err := engine.SubmitUserMessage(t.Context(), text)
		if err != nil {
			t.Fatalf("submit user turn %q: %v", text, err)
		}
		if answer.Content == nil || *answer.Content == "" {
			t.Fatalf("user turn %q did not complete with an assistant answer: %+v", text, answer)
		}
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("close Session runtime before reopen: %v", err)
	}

	reopened := mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), client, registry, cfg)
	answer, err := reopened.SubmitUserMessage(t.Context(), "continue after reopening the Session")
	if err != nil {
		t.Fatalf("continue reopened Session: %v", err)
	}
	if answer.Content == nil || *answer.Content == "" {
		t.Fatalf("reopened Session did not complete with an assistant answer: %+v", answer)
	}

	mu.Lock()
	captured := append([]genericResponsesContinuationRequest(nil), requests...)
	mu.Unlock()
	if len(captured) != len(outputs) {
		t.Fatalf("captured Responses request count = %d, want %d", len(captured), len(outputs))
	}
	for index, request := range captured {
		if len(request.Input) == 0 {
			t.Fatalf("Responses request %d has no stateless full input", index+1)
		}
		if request.PreviousResponseID != nil {
			t.Fatalf("Responses request %d used previous_response_id: %s", index+1, request.PreviousResponseID)
		}
	}
	requireGenericResponsesToolAvailable(t, captured[3])
	requireGenericResponsesToolAvailable(t, captured[4])

	reasoningExpectation := func(id, text string) genericResponsesContinuationExpectation {
		return genericResponsesContinuationExpectation{
			typeName: "reasoning", id: textutil.Value(id), reasoningText: textutil.Value(text),
			requireNoEncryptedContent: encryptedContent != nil,
		}
	}
	requireGenericResponsesInputOrder(t, captured[1], []genericResponsesContinuationExpectation{
		reasoningExpectation("rs_first", "verbatim reasoning before first tool"),
		{typeName: "function_call", id: textutil.Value("fc_first"), callID: textutil.Value("call_first")},
		{typeName: "function_call_output", callID: textutil.Value("call_first")},
	})
	requireGenericResponsesInputOrder(t, captured[2], []genericResponsesContinuationExpectation{
		reasoningExpectation("rs_first", "verbatim reasoning before first tool"),
		{typeName: "function_call", id: textutil.Value("fc_first"), callID: textutil.Value("call_first")},
		{typeName: "function_call_output", callID: textutil.Value("call_first")},
		reasoningExpectation("rs_after_tool", "verbatim reasoning after first tool"),
		{typeName: "message", id: textutil.Value("msg_after_tool")},
	})
	requireGenericResponsesInputOrder(t, captured[3], []genericResponsesContinuationExpectation{
		reasoningExpectation("rs_first", "verbatim reasoning before first tool"),
		{typeName: "function_call", id: textutil.Value("fc_first"), callID: textutil.Value("call_first")},
		{typeName: "function_call_output", callID: textutil.Value("call_first")},
		reasoningExpectation("rs_after_tool", "verbatim reasoning after first tool"),
		{typeName: "message", id: textutil.Value("msg_after_tool")},
		reasoningExpectation("rs_next_turn", "verbatim reasoning in next turn"),
		{typeName: "message", id: textutil.Value("msg_next_turn")},
	})
	requireGenericResponsesInputOrder(t, captured[4], []genericResponsesContinuationExpectation{
		reasoningExpectation("rs_first", "verbatim reasoning before first tool"),
		{typeName: "function_call", id: textutil.Value("fc_first"), callID: textutil.Value("call_first")},
		{typeName: "function_call_output", callID: textutil.Value("call_first")},
		reasoningExpectation("rs_after_tool", "verbatim reasoning after first tool"),
		{typeName: "message", id: textutil.Value("msg_after_tool")},
		reasoningExpectation("rs_next_turn", "verbatim reasoning in next turn"),
		{typeName: "message", id: textutil.Value("msg_next_turn")},
		reasoningExpectation("rs_reopened", "verbatim reasoning after reopen"),
		{typeName: "function_call", id: textutil.Value("fc_reopened"), callID: textutil.Value("call_reopened")},
		{typeName: "function_call_output", callID: textutil.Value("call_reopened")},
	})
	requirePersistedNativeResponseItemsUnchanged(t, store, outputs)
}

func requireGenericResponsesToolAvailable(t *testing.T, request genericResponsesContinuationRequest) {
	t.Helper()
	for _, raw := range request.Tools {
		var tool struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &tool); err != nil {
			t.Fatalf("decode Responses tool declaration: %v", err)
		}
		if tool.Name == string(toolspec.ToolExecCommand) {
			return
		}
	}
	t.Fatal("Responses request omitted the enabled exec_command tool")
}

func requireGenericResponsesInputOrder(
	t *testing.T,
	request genericResponsesContinuationRequest,
	expected []genericResponsesContinuationExpectation,
) {
	t.Helper()
	next := 0
	for _, raw := range request.Input {
		var item genericResponsesContinuationItem
		if err := json.Unmarshal(raw, &item); err != nil {
			t.Fatalf("decode Responses input item: %v", err)
		}
		want := expected[next]
		if item.Type != want.typeName ||
			(want.id != nil && item.ID != *want.id) ||
			(want.callID != nil && item.CallID != *want.callID) {
			continue
		}
		if want.reasoningText != nil {
			matchedText := false
			for _, content := range item.Content {
				if content.Type == "reasoning_text" && content.Text == *want.reasoningText {
					matchedText = true
					break
				}
			}
			if !matchedText {
				t.Fatalf("reasoning item %q lost its verbatim text: %s", *want.id, raw)
			}
		}
		if want.requireNoEncryptedContent && item.EncryptedContent != nil {
			t.Fatalf("reasoning item %q sent unsupported encrypted content: %s", *want.id, raw)
		}
		next++
		if next == len(expected) {
			return
		}
	}
	t.Fatalf("Responses input retained %d/%d ordered native items and tool results", next, len(expected))
}

func requirePersistedNativeResponseItemsUnchanged(t *testing.T, store *session.Store, outputs [][]json.RawMessage) {
	t.Helper()
	window, err := mustMaterializeTestEventLog(t, store).ReadRecentRecords(128)
	if err != nil {
		t.Fatalf("read persisted Session response items: %v", err)
	}
	var actual [][]byte
	for _, event := range window.Records {
		message, ok := mustSessionEventPayload(event).(session.MessageRecord)
		if !ok || message.NativeResponse == nil {
			continue
		}
		actual = append(actual, message.NativeResponse.Items...)
	}
	var expected [][]byte
	for _, response := range outputs {
		for _, item := range response {
			expected = append(expected, item)
		}
	}
	if len(actual) != len(expected) {
		t.Fatalf("persisted native response items = %d, want %d", len(actual), len(expected))
	}
	for index := range expected {
		if !bytes.Equal(actual[index], expected[index]) {
			t.Fatalf("persisted native response item %d changed:\n got %s\nwant %s", index, actual[index], expected[index])
		}
	}
}
