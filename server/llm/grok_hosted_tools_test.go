package llm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"core/shared/config"
	"core/shared/textutil"
)

func TestOpenAICustomCallsDoNotUseGrokOwnershipNamespaces(t *testing.T) {
	for _, callID := range []string{"as_pdf", "xs_search"} {
		t.Run(callID, func(t *testing.T) {
			transport := newOpenAIStreamTestTransport(t,
				fmt.Sprintf(`{"type":"response.completed","response":{"output":[{"type":"custom_tool_call","id":"custom_item","call_id":%q,"name":"patch","input":"patch input"}]}}`, callID),
			)
			response, err := transport.Generate(t.Context(), ResponsesRequest{
				SessionID: textutil.Value("fixture"), ToolChoiceMode: ToolChoiceModeAutomatic,
			}, StreamCallbacks{})
			if err != nil {
				t.Fatal(err)
			}
			if len(response.ToolCalls) != 1 || response.ToolCalls[0].ID != callID {
				t.Fatalf("local custom calls = %+v", response.ToolCalls)
			}
		})
	}
}

func TestPreparedCustomCallKeepsCallIdentityWithoutInventingItemIdentity(t *testing.T) {
	items := PrepareResponsesInputItems(ItemsFromMessages([]Message{{
		Role:      RoleAssistant,
		ToolCalls: []ToolCall{{ID: "local-call", Name: "patch", Custom: true, CustomInput: textutil.Value("patch input")}},
	}}))
	input, err := buildResponsesInput(items)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var calls []struct {
		CallID string `json:"call_id"`
	}
	if err := json.Unmarshal(raw, &calls); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].CallID != "local-call" {
		t.Fatalf("custom call identity = %+v", calls)
	}
	var payload []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if _, present := payload[0]["id"]; present {
		t.Fatalf("reconstructed call invented provider item ID: %s", raw)
	}
}

func TestGrokHostedCustomCallsAreNotLocalAndKeepWireHistory(t *testing.T) {
	for _, completed := range []bool{false, true} {
		t.Run(fmt.Sprintf("completed=%t", completed), func(t *testing.T) {
			items := []json.RawMessage{
				json.RawMessage(`{"type":"custom_tool_call","id":"remote_pdf","call_id":"as_pdf","name":"pdf_browse","input":"{\"document_id\":\"doc\",\"pages\":\"1\"}"}`),
				json.RawMessage(`{"type":"custom_tool_call","id":"remote_x","call_id":"xs_search","name":"x_keyword_search","input":"{\"query\":\"kent\"}"}`),
				json.RawMessage(`{"type":"custom_tool_call","id":"local_patch","call_id":"patch_call","name":"patch","input":"patch input"}`),
			}
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				serveResponsesTestWebsocket(t, w, r, func(raw json.RawMessage) []string {
					requests++
					if requests == 2 {
						var body struct {
							Input []json.RawMessage `json:"input"`
						}
						if err := json.Unmarshal(raw, &body); err != nil {
							t.Error(err)
						}
						for _, want := range items[:2] {
							var expected any
							_ = json.Unmarshal(want, &expected)
							found := false
							for _, raw := range body.Input {
								var got any
								_ = json.Unmarshal(raw, &got)
								if reflect.DeepEqual(got, expected) {
									found = true
								}
							}
							if !found {
								t.Errorf("hosted history lost: %s", want)
							}
						}
						return []string{`{"type":"response.completed","response":{"id":"second","output":[]}}`}
					}
					var events []string
					for index, item := range items {
						events = append(events, fmt.Sprintf(`{"type":"response.output_item.added","output_index":%d,"item":%s}`, index, item))
						events = append(events, fmt.Sprintf(`{"type":"response.output_item.done","output_index":%d,"item":%s}`, index, item))
					}
					if completed {
						output, _ := json.Marshal(items)
						events = append(events, fmt.Sprintf(`{"type":"response.completed","response":{"id":"first","output":%s}}`, output))
					} else {
						events = append(events, `{"type":"response.completed","response":{"id":"first","output":[]}}`)
					}
					return events
				})
			}))
			defer server.Close()
			selected, err := ResolveConnectionVariant(config.ProviderConnection{Protocol: config.ConnectionGrokOAuthAPI})
			if err != nil {
				t.Fatal(err)
			}
			provider, err := NewProviderClient(ProviderClientOptions{Registration: selected, Auth: oauthStaticAuth{},
				HTTPClient: newRewritingHTTPClient(t, server), ContextWindowTokens: 500_000})
			if err != nil {
				t.Fatal(err)
			}
			request := Request{Model: "grok-4.7", SessionID: textutil.Value("fixture"), ToolChoiceMode: ToolChoiceModeAutomatic, ReasoningEffort: "high"}
			first, err := provider.Generate(t.Context(), request, StreamCallbacks{})
			if err != nil {
				t.Fatal(err)
			}
			if len(first.ToolCalls) != 1 || first.ToolCalls[0].ID != "patch_call" {
				t.Fatalf("local executions = %+v", first.ToolCalls)
			}
			request.Items = PrepareResponsesInputItems(first.OutputItems)
			if _, err := provider.Generate(t.Context(), request, StreamCallbacks{}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
