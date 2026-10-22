package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"core/shared/config"
	"core/shared/textutil"
	"github.com/coder/websocket"
	"github.com/google/uuid"
)

func TestFirstPartyResponsesStreamsThroughManagedWebsocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer socket.CloseNow()
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Error("selected connection credential was not sent")
		}
		_, raw, err := socket.Read(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		var create struct {
			Type  string `json:"type"`
			Model string `json:"model"`
			Store *bool  `json:"store"`
		}
		if err := json.Unmarshal(raw, &create); err != nil {
			t.Error(err)
		}
		if create.Type != "response.create" || create.Model != "gpt-6-sol" || create.Store == nil || *create.Store {
			t.Errorf("prepared generation settings lost: %+v", create)
		}
		for _, event := range []string{
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"reasoning-native","summary":[]}}`,
			`{"type":"response.reasoning_summary_text.delta","output_index":0,"summary_index":0,"item_id":"reasoning-native","delta":"plan"}`,
			`{"type":"response.output_item.added","output_index":1,"item":{"type":"message","id":"native","role":"assistant","content":[]}}`,
			`{"type":"response.output_text.delta","output_index":1,"delta":"answer"}`,
			`{"type":"response.completed","response":{"id":"resp-native","status":"completed","model":"gpt-6-sol","output":[{"type":"reasoning","id":"reasoning-native","summary":[{"type":"summary_text","text":"plan"}],"encrypted_content":"opaque"},{"type":"message","id":"native","role":"assistant","content":[{"type":"output_text","text":"answer","annotations":[]}]},{"type":"function_call","id":"call-item","call_id":"call-native","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}"}],"usage":{"input_tokens":10,"output_tokens":1,"total_tokens":11}}}`,
		} {
			if err := socket.Write(ctx, websocket.MessageText, []byte(event)); err != nil {
				t.Error(err)
				return
			}
		}
	}))
	defer server.Close()
	registration := testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL)})
	registration.Variant.BaseURL = textutil.Value(server.URL + "/v1")
	client, err := NewProviderClient(ProviderClientOptions{Registration: registration, Auth: staticAuthHeader{}, HTTPClient: server.Client(), Model: "gpt-6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	var text, reasoning string
	response, err := client.Generate(ctx, Request{Model: "gpt-6-sol", ReasoningEffort: "high", SessionID: textutil.Value(uuid.NewString()), ToolChoiceMode: ToolChoiceModeAutomatic}, StreamCallbacks{
		OnAssistantDelta:        func(delta AssistantDelta) { text += delta.Text },
		OnReasoningSummaryDelta: func(delta ReasoningSummaryDelta) { reasoning += delta.Text },
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "answer" || response.Assistant.Content == nil || *response.Assistant.Content != text || response.Assistant.NativeResponse == nil {
		t.Fatal("streamed native response was not projected through the provider client")
	}
	if reasoning != "plan" || len(response.ToolCalls) != 1 || response.ToolCalls[0].ID != "call-native" || response.ToolCalls[0].OutputIndex == nil || *response.ToolCalls[0].OutputIndex != 2 {
		t.Fatal("reasoning or indexed native tool output was lost across the SDK event bridge")
	}
}
