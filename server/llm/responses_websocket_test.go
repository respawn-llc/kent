package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"core/shared/config"
	"core/shared/textutil"
	"github.com/coder/websocket"
	"github.com/google/uuid"
	sdkwebsocket "github.com/openai/openai-go/v3/packages/websocket"
)

func TestAssistantMessageSourceCompletesBeforeResponseAcceptance(t *testing.T) {
	source := `{"type":"message","id":"source","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"message","annotations":[]},{"type":"output_text","text":" continuation","annotations":[]}]}`
	completed := make(chan AssistantSource)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer socket.CloseNow()
		if _, _, err := socket.Read(t.Context()); err != nil {
			return
		}
		events := []string{
			fmt.Sprintf(`{"type":"response.output_item.added","output_index":2,"item":%s}`, source),
			`{"type":"response.output_text.delta","output_index":2,"delta":"message"}`,
			`{"type":"response.output_text.done","output_index":2,"text":"message"}`,
			`{"type":"response.output_text.delta","output_index":2,"content_index":1,"delta":" continuation"}`,
			`{"type":"response.output_text.done","output_index":2,"content_index":1,"text":" continuation"}`,
			fmt.Sprintf(`{"type":"response.output_item.done","output_index":2,"item":%s}`, source),
		}
		for _, event := range events {
			if err := socket.Write(t.Context(), websocket.MessageText, []byte(event)); err != nil {
				return
			}
		}
		select {
		case result := <-completed:
			if result.Text != "message continuation" || result.OutputIndex != 2 || result.Phase != MessagePhaseCommentary {
				t.Errorf("source completion lost message identity or content: %+v", result)
			}
		case <-time.After(5 * time.Second):
			return
		}
		_ = socket.Write(t.Context(), websocket.MessageText, []byte(fmt.Sprintf(
			`{"type":"response.completed","response":{"id":"completed","output":[%s]}}`, source)))
	}))
	defer server.Close()
	transport := newCanonicalOAuthTestTransport(t, server)
	defer transport.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, err := transport.Generate(ctx, ResponsesRequest{
		Model: "gpt-6-sol", ReasoningEffort: "high", ToolChoiceMode: ToolChoiceModeAutomatic,
		SessionID: textutil.Value("session-1"), CodexDispatch: newTestCodexDispatch(t),
	}, StreamCallbacks{OnAssistantSourceComplete: func(source AssistantSource) { completed <- source }})
	if err != nil {
		t.Fatal(err)
	}
}

func TestResponsesSocketTurnCompletion(t *testing.T) {
	for _, test := range []struct {
		name      string
		endTurn   json.RawMessage
		want      bool
		wantError bool
	}{
		{name: "omitted", want: true},
		{name: "true", endTurn: json.RawMessage(`true`), want: true},
		{name: "false", endTurn: json.RawMessage(`false`), want: false},
		{name: "null", endTurn: json.RawMessage(`null`), want: true},
		{name: "invalid", endTurn: json.RawMessage(`"false"`), wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := map[string]any{"id": "empty", "status": "completed", "output": []any{}}
			if test.endTurn != nil {
				response["end_turn"] = test.endTurn
			}
			event, err := json.Marshal(map[string]any{"type": "response.completed", "response": response})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				serveResponsesTestWebsocket(t, w, r, func(json.RawMessage) []string {
					return []string{string(event)}
				})
			}))
			defer server.Close()
			registration := testConnectionRegistration(t, config.ProviderConnection{
				Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL),
			})
			registration.Variant.BaseURL = textutil.Value(server.URL + "/v1")
			transport := newTestHTTPTransport(t, staticAuthHeader{}, registration)
			defer transport.Close()
			result, err := transport.Generate(t.Context(), ResponsesRequest{
				Model: "gpt-6-sol", SessionID: textutil.Value("session"),
				ReasoningEffort: "high", ToolChoiceMode: ToolChoiceModeAutomatic,
			}, StreamCallbacks{})
			if test.wantError {
				if err == nil {
					t.Fatal("invalid completion metadata was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.EndTurn == nil || *result.EndTurn != test.want {
				t.Fatalf("turn completion = %v, want %v", result.EndTurn, test.want)
			}
		})
	}
}

func TestResponsesSocketContinuesExactNativePrefixWithOnlyNewInput(t *testing.T) {
	for _, protocol := range []config.ConnectionProtocol{config.ConnectionResponses, config.ConnectionGrokAPIKey, config.ConnectionGrokOAuthAPI} {
		t.Run(string(protocol), func(t *testing.T) {
			native := json.RawMessage(`{"type":"message","id":"native","role":"assistant","content":[{"type":"output_text","text":"answer","annotations":[]}],"provider_extension":{"opaque":"retained"}}`)
			next := json.RawMessage(`{"type":"message","role":"user","content":[{"type":"input_text","text":"next"}]}`)
			var turns int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				serveResponsesTestWebsocket(t, w, r, func(raw json.RawMessage) []string {
					turns++
					var create struct {
						Input        []json.RawMessage `json:"input"`
						Previous     *string           `json:"previous_response_id"`
						Instructions *string           `json:"instructions"`
					}
					if err := json.Unmarshal(raw, &create); err != nil {
						t.Error(err)
					}
					wantInstructions := turns != 2 || protocol == config.ConnectionResponses
					if (create.Instructions != nil) != wantInstructions {
						t.Errorf("create instructions presence=%t want=%t: %s", create.Instructions != nil, wantInstructions, raw)
					}
					if turns == 2 {
						if create.Previous == nil || *create.Previous != "response-native" || len(create.Input) != 1 {
							t.Errorf("next request retransmitted prefix: %s", raw)
						}
						if len(create.Input) == 1 && !bytes.Equal(create.Input[0], next) {
							t.Errorf("new input missing: %s", raw)
						}
					}
					if turns == 3 && (create.Previous != nil || len(create.Input) != 4) {
						t.Errorf("changed controls did not reset to full context: %s", raw)
					}
					if turns == 4 && (create.Previous != nil || len(create.Input) != 1) {
						t.Errorf("replacement context did not reset: %s", raw)
					}
					return []string{fmt.Sprintf(`{"type":"response.output_item.done","output_index":0,"item":%s}`, native), fmt.Sprintf(`{"type":"response.completed","response":{"id":"response-native","output":[%s]}}`, native)}
				})
			}))
			defer server.Close()
			connection := config.ProviderConnection{Protocol: protocol}
			if protocol == config.ConnectionResponses {
				connection.Endpoint = textutil.Value(defaultOpenAIBaseURL)
			} else if protocol == config.ConnectionGrokAPIKey {
				connection.EnvironmentVariable = textutil.Value("KENT_TEST_GROK_KEY")
			}
			registration := testConnectionRegistration(t, connection)
			registration.Variant.BaseURL = textutil.Value(server.URL + "/v1")
			transport := newTestHTTPTransport(t, staticAuthHeader{}, registration)
			defer transport.Close()
			request := ResponsesRequest{Model: "gpt-6-sol", ReasoningEffort: "high", SessionID: textutil.Value("session"), ToolChoiceMode: ToolChoiceModeAutomatic,
				SystemPrompt: "initial instructions",
				Items:        []ResponseItem{{Raw: json.RawMessage(`{"type":"message","role":"user","content":[{"type":"input_text","text":"first"}]}`)}},
			}
			response, err := transport.Generate(t.Context(), request, StreamCallbacks{})
			if err != nil {
				t.Fatal(err)
			}
			request.Items = append(request.Items, response.OutputItems...)
			request.Items = append(request.Items, ResponseItem{Raw: next})
			response, err = transport.Generate(t.Context(), request, StreamCallbacks{})
			if err != nil {
				t.Fatal(err)
			}
			request.Items = append(request.Items, response.OutputItems...)
			request.SystemPrompt = "changed instructions"
			if _, err := transport.Generate(t.Context(), request, StreamCallbacks{}); err != nil {
				t.Fatal(err)
			}
			request.Items = []ResponseItem{{Raw: next}}
			if _, err := transport.Generate(t.Context(), request, StreamCallbacks{}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestResponsesCompactionReplacesContextWithoutReplacingSocket(t *testing.T) {
	var openings, turns, compactions atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			compactions.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"compaction\",\"id\":\"checkpoint\",\"encrypted_content\":\"encrypted\"}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"compacted\",\"output\":[{\"type\":\"compaction\",\"id\":\"checkpoint\",\"encrypted_content\":\"encrypted\"}]}}"+"\n\n")
			return
		}
		openings.Add(1)
		serveResponsesTestWebsocket(t, w, r, func(raw json.RawMessage) []string {
			var create struct {
				Previous *string           `json:"previous_response_id"`
				Input    []json.RawMessage `json:"input"`
			}
			if err := json.Unmarshal(raw, &create); err != nil {
				t.Error(err)
			}
			switch turns.Add(1) {
			case 2:
				if create.Previous != nil || len(create.Input) != 1 {
					t.Errorf("compacted context did not replace prefix: %s", raw)
				}
			case 3:
				if create.Previous == nil || len(create.Input) != 0 {
					t.Errorf("post-compaction continuation not incremental: %s", raw)
				}
			}
			return []string{completedResponseSSEJSON}
		})
	}))
	defer server.Close()
	registration := testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL)})
	registration.Variant.BaseURL = textutil.Value(server.URL + "/v1")
	transport := newTestHTTPTransport(t, staticAuthHeader{}, registration)
	request := ResponsesRequest{Model: "gpt-6-sol", ReasoningEffort: "high", SessionID: textutil.Value("session"), ToolChoiceMode: ToolChoiceModeAutomatic,
		Items: []ResponseItem{{Raw: json.RawMessage(`{"type":"message","role":"user","content":[{"type":"input_text","text":"first"}]}`)}}}
	if _, err := transport.Generate(t.Context(), request, StreamCallbacks{}); err != nil {
		t.Fatal(err)
	}
	compacted, err := transport.Compact(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.Items = PrepareResponsesInputItems(compacted.OutputItems)
	for range 2 {
		if _, err := transport.Generate(t.Context(), request, StreamCallbacks{}); err != nil {
			t.Fatal(err)
		}
	}
	if openings.Load() != 1 || compactions.Load() != 1 {
		t.Fatalf("socket openings=%d HTTP compactions=%d", openings.Load(), compactions.Load())
	}
}

func TestGrokSocketBareExhaustionKeepsStructuredClassification(t *testing.T) {
	for _, test := range []struct {
		code      string
		message   string
		exhausted bool
	}{
		{`"personal-team-blocked:spending-limit"`, "unavailable", true},
		{`"429"`, "[WKE=personal-team-blocked:spending-limit] unavailable", true},
		{`429`, "[WKE=personal-team-blocked:spending-limit] unavailable", true},
		{`"429"`, "rate limited", false},
		{`429`, "rate limited", false},
	} {
		t.Run(test.code+test.message, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				serveResponsesTestWebsocket(t, w, r, func(json.RawMessage) []string {
					return []string{fmt.Sprintf(`{"error":{"code":%s,"type":"allowance","message":%q}}`, test.code, test.message)}
				})
			}))
			defer server.Close()
			registration := testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionGrokOAuthAPI})
			registration.Variant.BaseURL = textutil.Value(server.URL + "/v1")
			transport := newTestHTTPTransport(t, oauthStaticAuth{}, registration)
			_, err := transport.Generate(t.Context(), ResponsesRequest{Model: "grok-4.7", SessionID: textutil.Value("session"), ReasoningEffort: "high", ToolChoiceMode: ToolChoiceModeAutomatic}, StreamCallbacks{})
			var providerErr *ProviderAPIError
			if !errors.As(err, &providerErr) || IsUsageExhaustionError(err) != test.exhausted {
				t.Fatalf("structured exhaustion lost: %T %v", err, err)
			}
			if test.exhausted && providerErr.ProviderCode != "personal-team-blocked:spending-limit" {
				t.Fatalf("normalized exhaustion code = %q", providerErr.ProviderCode)
			}
			if providerErr.StatusCode != nil {
				t.Fatalf("absent provider status became %v", *providerErr.StatusCode)
			}
		})
	}
}

func TestResponsesSocketErrorsRetainProviderFactsWithoutHandshakeStatus(t *testing.T) {
	for _, test := range []struct {
		name         string
		event        string
		code         UnifiedErrorCode
		status       *int
		providerCode string
		nonRetriable bool
	}{
		{name: "rate limit", event: `{"type":"error","status":429,"error":{"code":"rate_limit_exceeded","type":"rate_limit","message":"retry later"}}`, code: UnifiedErrorCodeUnknown, status: textutil.Value(429), providerCode: "rate_limit_exceeded"},
		{name: "quota", event: `{"type":"error","error":{"code":"insufficient_quota","type":"allowance","message":"unavailable"}}`, code: UnifiedErrorCodeUsageExhausted, providerCode: "insufficient_quota", nonRetriable: true},
		{name: "failed", event: `{"type":"response.failed","response":{"id":"failed","error":{"code":"server_is_overloaded","message":"busy"}}}`, code: UnifiedErrorCodeProviderOverload, providerCode: "server_is_overloaded"},
		{name: "incomplete", event: `{"type":"response.incomplete","response":{"id":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`, code: UnifiedErrorCodeUnknown, providerCode: "max_output_tokens", nonRetriable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				serveResponsesTestWebsocket(t, w, r, func(json.RawMessage) []string { return []string{test.event} })
			}))
			defer server.Close()
			registration := testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL)})
			registration.Variant.BaseURL = textutil.Value(server.URL + "/v1")
			transport := newTestHTTPTransport(t, staticAuthHeader{}, registration)
			_, err := transport.Generate(t.Context(), ResponsesRequest{Model: "gpt-6-sol", ReasoningEffort: "high", SessionID: textutil.Value("session"), ToolChoiceMode: ToolChoiceModeAutomatic}, StreamCallbacks{})
			var failure *ProviderAPIError
			if !errors.As(err, &failure) || failure.Code != test.code || failure.ProviderCode != test.providerCode || IsNonRetriableModelError(err) != test.nonRetriable {
				t.Fatalf("provider classification=%+v", err)
			}
			if (test.status == nil) != (failure.StatusCode == nil) || (test.status != nil && *test.status != *failure.StatusCode) {
				t.Fatalf("provider status=%v expected=%v", failure.StatusCode, test.status)
			}
		})
	}
}

func TestResponsesSocketRejectsInvalidFramesBeforeTerminal(t *testing.T) {
	for name, event := range map[string]string{
		"malformed":        `{"type":`,
		"numeric type":     `{"type":1,"error":{"code":"insufficient_quota"}}`,
		"null type":        `{"type":null,"error":{"code":"insufficient_quota"}}`,
		"empty type":       `{"type":"","error":{"code":"insufficient_quota"}}`,
		"missing envelope": `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				serveResponsesTestWebsocket(t, w, r, func(json.RawMessage) []string { return []string{event} })
			}))
			defer server.Close()
			registration := testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL)})
			registration.Variant.BaseURL = textutil.Value(server.URL + "/v1")
			transport := newTestHTTPTransport(t, staticAuthHeader{}, registration)
			defer transport.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			_, err := transport.Generate(ctx, ResponsesRequest{Model: "gpt-6-sol", ReasoningEffort: "high", SessionID: textutil.Value("session"), ToolChoiceMode: ToolChoiceModeAutomatic}, StreamCallbacks{})
			if err == nil || errors.Is(err, context.DeadlineExceeded) || IsUsageExhaustionError(err) {
				t.Fatalf("invalid frame did not fail promptly as a protocol failure: %v", err)
			}
		})
	}
}

func TestResponsesOpeningRetainsRejectedUpgradeFacts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", "rejected-request")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"code":"insufficient_quota","message":"unavailable"}}`)
	}))
	defer server.Close()
	transport := newCanonicalOAuthTestTransport(t, server)
	_, err := transport.Generate(t.Context(), testCodexResponsesRequest(newTestCodexDispatch(t)), StreamCallbacks{})
	var failure *ProviderAPIError
	if !errors.As(err, &failure) || !IsUsageExhaustionError(err) || failure.StatusCode == nil ||
		*failure.StatusCode != http.StatusTooManyRequests || failure.ProviderRequestID == nil ||
		*failure.ProviderRequestID != "rejected-request" || failure.ProviderCode != "insufficient_quota" {
		t.Fatalf("rejected upgrade lost provider facts: %v", err)
	}
}

func TestResponsesOpeningRejectsChangedOriginBeforeSendingCredentials(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprintf("secure=%t", secure), func(t *testing.T) {
			var destinationRequests atomic.Int32
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				destinationRequests.Add(1)
				w.WriteHeader(http.StatusForbidden)
			}))
			defer destination.Close()
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, destination.URL+"/v1/responses", http.StatusTemporaryRedirect)
			})
			origin := httptest.NewUnstartedServer(handler)
			if secure {
				origin.StartTLS()
			} else {
				origin.Start()
			}
			defer origin.Close()
			registration := testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionChatGPT})
			registration.Variant.BaseURL = textutil.Value(origin.URL + "/v1")
			transport := newTestHTTPTransport(t, oauthStaticAuth{}, registration)
			transport.Client = origin.Client()
			_, err := transport.Generate(t.Context(), testCodexResponsesRequest(newTestCodexDispatch(t)), StreamCallbacks{})
			if err == nil || destinationRequests.Load() != 0 {
				t.Fatalf("redirect sent selected credentials outside configured origin: requests=%d error=%v", destinationRequests.Load(), err)
			}
		})
	}
}

func TestResponsesOpeningKeepsSameOriginRedirectAndCallerPolicy(t *testing.T) {
	var redirects, openings atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/responses" {
			http.Redirect(w, r, "/socket", http.StatusTemporaryRedirect)
			return
		}
		openings.Add(1)
		if r.Header.Get("Authorization") != "Bearer token" || r.Header.Get("ChatGPT-Account-Id") != "acc-1" ||
			r.Header.Get("session-id") != "session-1" {
			t.Error("redirect lost selected credential/account/Session headers")
		}
		serveResponsesTestWebsocket(t, w, r, func(json.RawMessage) []string { return []string{completedResponseSSEJSON} })
	}))
	defer server.Close()
	registration := testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionChatGPT})
	registration.Variant.BaseURL = textutil.Value(server.URL + "/v1")
	transport := newTestHTTPTransport(t, oauthStaticAuth{}, registration)
	transport.Client = server.Client()
	transport.Client.CheckRedirect = func(*http.Request, []*http.Request) error { redirects.Add(1); return nil }
	if _, err := transport.Generate(t.Context(), testCodexResponsesRequest(newTestCodexDispatch(t)), StreamCallbacks{}); err != nil {
		t.Fatal(err)
	}
	if redirects.Load() != 1 || openings.Load() != 1 {
		t.Fatal("same-origin opening lost the existing redirect policy")
	}
}

func TestResponsesOpeningRejectsDowngradeBeforeCallerRedirectPolicy(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://"+r.Host+"/socket", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	registration := testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionChatGPT})
	registration.Variant.BaseURL = textutil.Value(server.URL + "/v1")
	transport := newTestHTTPTransport(t, oauthStaticAuth{}, registration)
	transport.Client = server.Client()
	var callerPolicy atomic.Int32
	transport.Client.CheckRedirect = func(*http.Request, []*http.Request) error {
		callerPolicy.Add(1)
		return errors.New("caller received an insecure redirect")
	}
	_, err := transport.Generate(t.Context(), testCodexResponsesRequest(newTestCodexDispatch(t)), StreamCallbacks{})
	if err == nil || callerPolicy.Load() != 0 {
		t.Fatal("same-host/port downgrade passed the configured-origin boundary")
	}
}

func TestResponsesOpeningRevalidatesAuthorityAfterCallerRedirectPolicy(t *testing.T) {
	var redirectedRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/responses" {
			http.Redirect(w, r, "/socket", http.StatusTemporaryRedirect)
			return
		}
		redirectedRequests.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	registration := testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionChatGPT})
	registration.Variant.BaseURL = textutil.Value(server.URL + "/v1")
	transport := newTestHTTPTransport(t, oauthStaticAuth{}, registration)
	transport.Client = server.Client()
	transport.Client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		next.Host = "foreign.invalid"
		return nil
	}
	_, err := transport.Generate(t.Context(), testCodexResponsesRequest(newTestCodexDispatch(t)), StreamCallbacks{})
	if err == nil || redirectedRequests.Load() != 0 {
		t.Fatal("caller redirect policy changed HTTP authority before credential-bearing dispatch")
	}
}

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
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"reasoning-native","summary":[{"type":"summary_text","text":"plan"}],"encrypted_content":"opaque"}}`,
			`{"type":"response.output_item.done","output_index":1,"item":{"type":"message","id":"native","role":"assistant","content":[{"type":"output_text","text":"answer","annotations":[]}]}}`,
			`{"type":"response.output_item.done","output_index":2,"item":{"type":"function_call","id":"call-item","call_id":"call-native","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}"}}`,
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

func TestResponsesSocketFailsWhenItsReceiveQueueExceeds1600Events(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	blocked, release, sent := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer socket.CloseNow()
		defer close(sent)
		if _, _, err := socket.Read(ctx); err != nil {
			return
		}
		frame := []byte(`{"type":"response.created","response":{"id":"pending","output":[]}}`)
		if err := socket.Write(ctx, websocket.MessageText, frame); err != nil {
			return
		}
		select {
		case <-blocked:
		case <-ctx.Done():
			return
		}
		for range 1601 {
			if err := socket.Write(ctx, websocket.MessageText, frame); err != nil {
				return
			}
		}
		_, _, _ = socket.Read(ctx)
	}))
	defer server.Close()
	registration := testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL)})
	registration.Variant.BaseURL = textutil.Value(server.URL + "/v1")
	transport := newTestHTTPTransport(t, staticAuthHeader{}, registration)
	done := make(chan error, 1)
	var first sync.Once
	go func() {
		_, err := transport.Generate(ctx, ResponsesRequest{Model: "gpt-6-sol", ReasoningEffort: "high", SessionID: textutil.Value("session"), ToolChoiceMode: ToolChoiceModeAutomatic}, StreamCallbacks{
			OnStreamActivity: func() { first.Do(func() { close(blocked); <-release }) },
		})
		done <- err
	}()
	select {
	case <-sent:
	case <-ctx.Done():
		close(release)
		t.Fatal(ctx.Err())
	}
	close(release)
	if err := <-done; !errors.Is(err, sdkwebsocket.ErrBufferLimit) {
		t.Fatalf("receive bound failure = %v", err)
	}
}

func TestResponsesSocketRetainsOpeningCredentialsAcrossCompletedCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveResponsesTestWebsocket(t, w, r, func(json.RawMessage) []string {
			return []string{completedResponseSSEJSON}
		})
	}))
	t.Cleanup(server.Close)
	auth := &countingAuth{}
	registration := testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL)})
	registration.Variant.BaseURL = textutil.Value(server.URL + "/v1")
	transport := newTestHTTPTransport(t, auth, registration)
	request := ResponsesRequest{Model: "gpt-6-sol", ReasoningEffort: "high", SessionID: textutil.Value("session"), ToolChoiceMode: ToolChoiceModeAutomatic}
	for range 2 {
		ctx, cancel := context.WithCancel(t.Context())
		if _, err := transport.Generate(ctx, request, StreamCallbacks{}); err != nil {
			cancel()
			t.Fatal(err)
		}
		cancel()
	}
	if auth.calls.Load() != 1 {
		t.Fatalf("credential resolutions on one reusable socket = %d", auth.calls.Load())
	}
}

func TestResponsesStopAbandonsOnlyActiveSocketAndNextCallStartsFresh(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	started, abandoned := make(chan struct{}), make(chan struct{})
	var openings atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer socket.CloseNow()
		if _, _, err := socket.Read(ctx); err != nil {
			t.Error(err)
			return
		}
		if openings.Add(1) == 1 {
			close(started)
			_, _, _ = socket.Read(ctx)
			close(abandoned)
			return
		}
		if err := socket.Write(ctx, websocket.MessageText, []byte(completedResponseSSEJSON)); err != nil {
			t.Error(err)
		}
		_, _, _ = socket.Read(ctx)
	}))
	defer server.Close()
	registration := testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL)})
	registration.Variant.BaseURL = textutil.Value(server.URL + "/v1")
	auth := &countingAuth{}
	client, err := NewProviderClient(ProviderClientOptions{Registration: registration, Auth: auth, HTTPClient: server.Client(), Model: "gpt-6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := client.(io.Closer).Close(); err != nil {
			t.Error(err)
		}
	}()
	request := Request{Model: "gpt-6-sol", ReasoningEffort: "high", SessionID: textutil.Value("session"), ToolChoiceMode: ToolChoiceModeAutomatic}
	active, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		_, err := client.Generate(active, request, StreamCallbacks{})
		done <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	stop()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop returned %v", err)
	}
	select {
	case <-abandoned:
	case <-ctx.Done():
		t.Fatal("Stop did not release the provider socket")
	}
	if _, err := client.Generate(ctx, request, StreamCallbacks{}); err != nil {
		t.Fatal(err)
	}
	if openings.Load() != 2 || auth.calls.Load() != 2 {
		t.Fatalf("fresh invocation: sockets=%d credentials=%d", openings.Load(), auth.calls.Load())
	}
}

func TestResponsesSocketContinuesWithCommittedToolOutput(t *testing.T) {
	call := json.RawMessage(`{"type":"function_call","id":"fc_first","call_id":"call_first","name":"exec_command","arguments":"{}","status":"completed"}`)
	output := json.RawMessage(`{"type":"function_call_output","call_id":"call_first","output":"result"}`)
	var turns int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveResponsesTestWebsocket(t, w, r, func(raw json.RawMessage) []string {
			turns++
			var create struct {
				Input    []json.RawMessage `json:"input"`
				Previous *string           `json:"previous_response_id"`
			}
			if err := json.Unmarshal(raw, &create); err != nil {
				t.Fatal(err)
			}
			if turns == 2 {
				if create.Previous == nil || *create.Previous != "response-first" || len(create.Input) != 1 || !bytes.Equal(create.Input[0], output) {
					t.Errorf("tool continuation lost committed output: %s", raw)
				}
				return []string{`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_final","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"done","annotations":[]}]}}`, `{"type":"response.completed","response":{"id":"response-final","output":[]}}`}
			}
			return []string{fmt.Sprintf(`{"type":"response.output_item.done","output_index":0,"item":%s}`, call), `{"type":"response.completed","response":{"id":"response-first","output":[]}}`}
		})
	}))
	defer server.Close()
	transport := newCanonicalOAuthTestTransport(t, server)
	request := testCodexResponsesRequest(newTestCodexDispatch(t))
	response, err := transport.Generate(t.Context(), request, StreamCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.ToolCalls) != 1 || response.NativeResponse == nil || len(response.OutputItems) != 1 {
		t.Fatalf("finalized tool call lost: %+v", response)
	}
	request.Items = append(request.Items, response.OutputItems...)
	request.Items = append(request.Items, ResponseItem{Raw: output})
	restored, err := MessageFromNativeResponse(*response.NativeResponse.Record())
	if err != nil || len(restored.ToolCalls) != 1 || restored.ToolCalls[0].ID != response.ToolCalls[0].ID {
		t.Fatalf("restored tool call lost: %+v, %v", restored, err)
	}
	final, err := transport.Generate(t.Context(), request, StreamCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	if final.NativeResponse == nil || final.AssistantText == nil || final.ProviderPhase == nil {
		t.Fatalf("finalized answer lost: %+v", final)
	}
	restored, err = MessageFromNativeResponse(*final.NativeResponse.Record())
	if err != nil || restored.Content == nil || restored.Phase == nil || *restored.Phase != MessagePhaseFinal {
		t.Fatalf("restored answer lost: %+v, %v", restored, err)
	}
}
