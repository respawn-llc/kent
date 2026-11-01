package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"core/shared/textutil"
)

func TestCodexSocketTurnStateComesFromMetadataAndContinuesInBand(t *testing.T) {
	var creates int
	var openings int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		openings++
		if len(r.Header.Values(codexTurnStateHeader)) != 0 {
			t.Error("socket opening replayed turn state")
		}
		w.Header().Set(codexTurnStateHeader, "header-state")
		serveResponsesTestWebsocket(t, w, r, func(raw json.RawMessage) []string {
			creates++
			var create struct {
				Metadata map[string]string `json:"client_metadata"`
				Previous *string           `json:"previous_response_id"`
			}
			if err := json.Unmarshal(raw, &create); err != nil {
				t.Error(err)
			}
			state, present := create.Metadata[codexTurnStateHeader]
			if creates == 2 {
				if !present || state != "metadata-state" || create.Previous == nil {
					t.Errorf("next create lost state or incremental continuation: %s", raw)
				}
			} else if present {
				t.Error("turn state leaked into a new Agent Turn")
			}
			return []string{
				`{"type":"response.metadata","headers":{}}`,
				`{"type":"response.metadata","headers":{"x-codex-turn-state":" bad"}}`,
				`{"type":"response.metadata","headers":{"x-codex-turn-state":"metadata-state"}}`,
				`{"type":"response.metadata","headers":{"x-codex-turn-state":"later-conflict"}}`,
				completedResponseSSEJSON,
			}
		})
	}))
	t.Cleanup(server.Close)

	dispatch := newTestCodexDispatch(t)
	transport := newCanonicalOAuthTestTransport(t, server)
	if _, err := transport.Generate(context.Background(), testCodexResponsesRequest(dispatch), StreamCallbacks{}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got, ok := dispatch.currentTurnState(); !ok || got != "metadata-state" {
		t.Fatalf("turn state = (%q, %v), want first valid metadata", got, ok)
	}
	if _, err := transport.Generate(t.Context(), testCodexResponsesRequest(dispatch), StreamCallbacks{}); err != nil {
		t.Fatal(err)
	}
	if _, err := transport.Generate(t.Context(), testCodexResponsesRequest(newTestCodexDispatch(t)), StreamCallbacks{}); err != nil {
		t.Fatal(err)
	}
	if openings != 1 || creates != 3 {
		t.Fatalf("routing changed socket lifetime: %d openings, %d creates", openings, creates)
	}
	if diagnostics := dispatch.TurnStateDiagnostics(); len(diagnostics) != 1 || diagnostics[0] != CodexTurnStateDiagnosticInvalid {
		t.Fatalf("invalid metadata diagnostics = %v", diagnostics)
	}
}

func TestCodexTurnStateInvalidWarningIsRedacted(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	dispatch := newTestCodexDispatch(t)
	dispatch.bindTurnState(nil, "https://chatgpt.invalid/backend-api/codex/responses")
	const secret = "secret\nstate"
	dispatch.observeTurnStateCandidate(secret, codexTurnStateSourceHTTPHeader)

	if strings.Contains(logs.String(), secret) {
		t.Fatal("warning leaked provider turn state")
	}
	if got := dispatch.TurnStateDiagnostics(); len(got) != 1 || got[0] != CodexTurnStateDiagnosticInvalid {
		t.Fatalf("diagnostics = %v, want one invalid diagnostic", got)
	}
}

func TestCodexSocketOptionalMetadataDoesNotChangeGeneration(t *testing.T) {
	for _, test := range []struct {
		name    string
		headers string
		want    *string
		invalid bool
	}{
		{"number", `{"x-codex-turn-state":42}`, nil, true},
		{"object", `{"x-codex-turn-state":{"value":"opaque"}}`, nil, true},
		{"invalid headers", `42`, nil, true},
		{"array", `{"x-codex-turn-state":["opaque,state=value","ignored"]}`, textutil.Value("opaque,state=value"), false},
		{"nested array", `{"X-Codex-Turn-State":[["opaque-state"]]}`, textutil.Value("opaque-state"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var creates int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				serveResponsesTestWebsocket(t, w, r, func(raw json.RawMessage) []string {
					creates++
					if creates == 2 {
						var create struct {
							Metadata map[string]string `json:"client_metadata"`
						}
						if err := json.Unmarshal(raw, &create); err != nil {
							t.Error(err)
						}
						state, present := create.Metadata[codexTurnStateHeader]
						if present != (test.want != nil) || (test.want != nil && state != *test.want) {
							t.Errorf("next create metadata = %v", create.Metadata)
						}
					}
					return []string{fmt.Sprintf(`{"type":"response.metadata","headers":%s}`, test.headers), completedResponseSSEJSON}
				})
			}))
			t.Cleanup(server.Close)
			transport := newCanonicalOAuthTestTransport(t, server)
			t.Cleanup(func() { _ = transport.Close() })
			dispatch := newTestCodexDispatch(t)
			for range 2 {
				if _, err := transport.Generate(t.Context(), testCodexResponsesRequest(dispatch), StreamCallbacks{}); err != nil {
					t.Fatalf("optional metadata changed generation outcome: %v", err)
				}
			}
			diagnostics := dispatch.TurnStateDiagnostics()
			if test.invalid {
				if len(diagnostics) != 1 || diagnostics[0] != CodexTurnStateDiagnosticInvalid {
					t.Fatalf("invalid state diagnostics = %v", diagnostics)
				}
			} else if len(diagnostics) != 0 {
				t.Fatalf("valid state diagnostics = %v", diagnostics)
			}
		})
	}
}

const completedResponseSSEJSON = `{"type":"response.completed","response":{"id":"response-test","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2},"output":[]}}`
const completedCompactionItemSSEJSON = `{"type":"response.output_item.done","output_index":0,"item":{"type":"compaction","id":"checkpoint","encrypted_content":"opaque-checkpoint"}}`
const completedCompactionSSEJSON = `{"type":"response.completed","response":{"id":"compact-test","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2},"output":[{"type":"compaction","id":"checkpoint","encrypted_content":"opaque-checkpoint"}]}}`

func writeCompletedResponseSSE(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", completedResponseSSEJSON)
}

func newTestCodexDispatch(t *testing.T) *CodexDispatchContext {
	t.Helper()
	dispatch, err := NewCodexDispatchContext(CodexDispatchFacts{
		SessionID:            "session-1",
		StepID:               "step-1",
		CompactionGeneration: 1,
		RequestKind:          CodexRequestKindTurn.Optional(),
	})
	if err != nil {
		t.Fatalf("NewCodexDispatchContext: %v", err)
	}
	return dispatch
}

func testCodexResponsesRequest(dispatch *CodexDispatchContext) ResponsesRequest {
	return ResponsesRequest{
		Model:          "gpt-6-sol",
		ToolChoiceMode: ToolChoiceModeAutomatic,
		SessionID:      textutil.Value("session-1"),
		CodexDispatch:  dispatch, ReasoningEffort: "high",
	}
}
