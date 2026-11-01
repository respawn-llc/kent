package llm

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"core/shared/config"
	"core/shared/textutil"
	"github.com/coder/websocket"
	"github.com/openai/openai-go/v3/responses"
)

func TestCyberAccessIsSentOnEverySocketTurn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer socket.CloseNow()
		for range 2 {
			_, data, err := socket.Read(t.Context())
			if err != nil {
				t.Error(err)
				return
			}
			var request responses.ResponsesClientEventResponseCreateParam
			if err := json.Unmarshal(data, &request); err != nil {
				t.Error(err)
				return
			}
			if request.AccessPrograms.Cyber != "daybreak_blue" {
				t.Error("socket turn omitted requested cybersecurity access")
			}
			if err := socket.Write(t.Context(), websocket.MessageText, []byte(`{"type":"response.completed","response":{"id":"completed","output":[]}}`)); err != nil {
				t.Error(err)
				return
			}
		}
	}))
	defer server.Close()
	transport := newCanonicalOAuthTestTransport(t, server)
	defer transport.Close()
	transport.CyberAccess = true
	for range 2 {
		_, err := transport.Generate(t.Context(), ResponsesRequest{
			Model: "gpt-6-sol", ReasoningEffort: "low", ToolChoiceMode: ToolChoiceModeAutomatic,
			SessionID: textutil.Value("session-1"), CodexDispatch: newTestCodexDispatch(t),
		}, StreamCallbacks{})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestCyberAccessRequestSelection(t *testing.T) {
	for _, connection := range []config.ProviderConnection{
		{Protocol: config.ConnectionChatGPT},
		{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL)},
	} {
		for _, enabled := range []bool{false, true} {
			client := newResponsesClientFromOptions(t, ProviderClientOptions{
				Registration: testConnectionRegistration(t, connection),
				Auth:         providerTestAuth{}, Model: "gpt-6-sol", CyberAccess: enabled,
			})
			transport := httpTransportFromResponsesClient(t, client)
			payload, err := transport.buildDispatchPayload(
				ResponsesRequest{Model: "gpt-6-sol", ReasoningEffort: "low", ToolChoiceMode: ToolChoiceModeAutomatic},
				OpenAIAuthMode{}, transport.registration.Variant.Capabilities, nil,
			)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				AccessPrograms *struct{ Cyber string } `json:"access_programs"`
			}
			if err := json.Unmarshal(encoded, &wire); err != nil {
				t.Fatal(err)
			}
			if enabled {
				if wire.AccessPrograms == nil || wire.AccessPrograms.Cyber != "daybreak_blue" {
					t.Fatalf("missing requested cyber access: %s", encoded)
				}
			} else if wire.AccessPrograms != nil {
				t.Fatal("cyber access enabled without opt-in")
			}
			var create responses.ResponsesClientEventResponseCreateParam
			if err := json.Unmarshal(encoded, &create); err != nil {
				t.Fatal(err)
			}
			if create.AccessPrograms.Cyber != payload.AccessPrograms.Cyber {
				t.Fatal("websocket request lost cyber access selection")
			}
		}
	}
}

func TestCyberAccessRejectsUnsupportedConnection(t *testing.T) {
	_, err := NewProviderClient(ProviderClientOptions{
		Registration: testConnectionRegistration(t, config.ProviderConnection{
			Protocol: config.ConnectionResponses, Endpoint: textutil.Value("http://localhost:8000/v1"),
		}),
		Auth: providerTestAuth{}, Model: "gpt-6-sol", CyberAccess: true,
	})
	if !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("unsupported connection did not reject cyber access: %v", err)
	}
}
