package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"core/internal/testharness/httpclient"
	"core/shared/config"
	"core/shared/llmerrors"
	"core/shared/textutil"
)

func TestGrokFailedStreamRetainsEmittedTraceAndTypedFailure(t *testing.T) {
	const trace = "provider trace before failure"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		encoded, _ := json.Marshal(trace)
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.reasoning_summary_text.delta\",\"output_index\":0,\"summary_index\":0,\"item_id\":\"r1\",\"delta\":%s}\n\n", encoded)
		_, _ = io.WriteString(w, "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"personal-team-blocked:spending-limit\",\"message\":\"fixture quota failure\"}}}\n\n")
	}))
	defer server.Close()
	selected, err := ResolveConnectionVariant(config.ProviderConnection{Protocol: config.ConnectionGrokCLIProxy})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewProviderClient(ProviderClientOptions{Registration: selected, Auth: oauthStaticAuth{},
		HTTPClient: newRewritingHTTPClient(t, server), ContextWindowTokens: 256_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	var observed []ReasoningSummaryDelta
	_, err = provider.Generate(t.Context(), Request{
		Model: "grok-4.7", SessionID: textutil.Value("fixture"), ToolChoiceMode: ToolChoiceModeAutomatic,
	}, StreamCallbacks{OnReasoningSummaryDelta: func(delta ReasoningSummaryDelta) { observed = append(observed, delta) }})
	var failure *ProviderAPIError
	if !errors.As(err, &failure) || failure.ProviderCode != "personal-team-blocked:spending-limit" ||
		!IsNonRetriableModelError(err) || IsAuthenticationError(err) {
		t.Fatalf("stream failure = %v", err)
	}
	if len(observed) != 1 || observed[0].Text != trace || requests != 1 {
		t.Fatalf("emitted trace/replay = %+v, %d requests", observed, requests)
	}
}

func TestGrokToolContinuationKeepsOpaqueItemsAndReasoning(t *testing.T) {
	const trace = "**provider heading**\n\n  exact provider trace \r\n"
	encodedTrace, _ := json.Marshal(trace)
	reasoning := json.RawMessage(fmt.Sprintf(`{"type":"reasoning","id":"r1","encrypted_content":"opaque","summary":[{"type":"summary_text","text":%s}],"xai_extension":{"keep":true}}`, encodedTrace))
	call := json.RawMessage(`{"type":"function_call","id":"fc1","call_id":"c1","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}`)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		if requests == 1 {
			_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.reasoning_summary_text.delta\",\"output_index\":0,\"summary_index\":0,\"item_id\":\"r1\",\"delta\":%s}\n\n", encodedTrace)
			_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":%s}\n\n", call)
			_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"first\",\"output\":[%s,%s]}}\n\n", reasoning, call)
			return
		}
		var request struct {
			Input []json.RawMessage `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if len(request.Input) != 3 {
			t.Fatalf("continuation input count = %d", len(request.Input))
		}
		var opaque struct {
			Extension *struct{ Keep bool } `json:"xai_extension"`
		}
		if err := json.Unmarshal(request.Input[0], &opaque); err != nil || opaque.Extension == nil || !opaque.Extension.Keep {
			t.Errorf("opaque reasoning lost: %s, %v", request.Input[0], err)
		}
		var result struct {
			CallID string `json:"call_id"`
			Output []struct {
				Type string `json:"type"`
			} `json:"output"`
		}
		if err := json.Unmarshal(request.Input[2], &result); err != nil || result.CallID != "c1" || len(result.Output) != 2 ||
			result.Output[0].Type != "input_text" || result.Output[1].Type != "input_image" {
			t.Errorf("call-bound result changed: %s, %v", request.Input[2], err)
		}
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"second\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}]}}\n\n")
	}))
	defer server.Close()
	selected, err := ResolveConnectionVariant(config.ProviderConnection{Protocol: config.ConnectionGrokCLIProxy})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewProviderClient(ProviderClientOptions{Registration: selected, Auth: oauthStaticAuth{},
		HTTPClient: newRewritingHTTPClient(t, server), ContextWindowTokens: 256_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Model: "grok-4.7", SessionID: textutil.Value("fixture"), ToolChoiceMode: ToolChoiceModeAutomatic}
	var deltas []ReasoningSummaryDelta
	first, err := provider.Generate(t.Context(), request, StreamCallbacks{OnReasoningSummaryDelta: func(delta ReasoningSummaryDelta) {
		deltas = append(deltas, delta)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(deltas) != 1 || deltas[0].Text != trace || deltas[0].CurrentStatus != nil ||
		len(first.Reasoning) != 1 || first.Reasoning[0].Text != trace {
		t.Fatalf("Grok reasoning was reshaped: deltas=%+v final=%+v", deltas, first.Reasoning)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].ID != "c1" {
		t.Fatalf("tool calls = %+v", first.ToolCalls)
	}
	request.Items = append(first.OutputItems, PrepareResponsesInputItems([]ResponseItem{{
		Type: ResponseItemTypeFunctionCallOutput, CallID: textutil.Value("c1"),
		Output: json.RawMessage(`[{"type":"input_text","text":"result"},{"type":"input_image","image_url":"data:image/png;base64,AA=="}]`),
	}})...)
	second, err := provider.Generate(t.Context(), request, StreamCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	if !second.ProviderPhase.IsAbsent() || requests != 2 {
		t.Fatalf("phase/continuation = %+v, %d", second.ProviderPhase, requests)
	}
}

func TestGrokFailuresKeepDiagnosticsWithoutRetryOrAuthFallback(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		code   string
		auth   bool
	}{
		{"expired sign-in", 401, `{"error":{"code":"invalid_token","message":"fixture expired token"}}`, "invalid_token", true},
		{"subscription spending limit", 402, `{"error":"fixture credit requirement","code":"personal-team-blocked:spending-limit"}`, "personal-team-blocked:spending-limit", false},
		{"subscription entitlement", 403, `{"error":"fixture entitlement requirement","code":"personal-team-blocked:spending-limit"}`, "personal-team-blocked:spending-limit", false},
		{"protocol version", 426, `{"error":"fixture client update required","code":"client_version_unsupported"}`, "client_version_unsupported", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			selected, err := ResolveConnectionVariant(config.ProviderConnection{Protocol: config.ConnectionGrokCLIProxy})
			if err != nil {
				t.Fatal(err)
			}
			provider, err := NewProviderClient(ProviderClientOptions{Registration: selected, Auth: oauthStaticAuth{},
				ConnectionID: textutil.Value(config.ConnectionID("selected-grok")),
				HTTPClient:   newRewritingHTTPClient(t, server), ContextWindowTokens: 256_000,
				ProviderCapabilitiesOverride: &ProviderCapabilities{ProviderID: "chatgpt-codex", SupportsResponsesAPI: true},
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = provider.Generate(t.Context(), Request{
				Model: "grok-4.7", SessionID: textutil.Value("fixture"), ToolChoiceMode: ToolChoiceModeAutomatic,
			}, StreamCallbacks{})
			var failure *ProviderAPIError
			if !errors.As(err, &failure) || failure.ProviderID != "grok-cli-proxy" || failure.StatusCode != test.status || failure.ProviderCode != test.code || failure.Message == "" ||
				failure.ConnectionID == nil || *failure.ConnectionID != "selected-grok" {
				t.Fatalf("provider failure = %#v, %v", failure, err)
			}
			if llmerrors.IsAuthenticationError(err) != test.auth || !IsNonRetriableModelError(err) {
				t.Fatalf("incorrect retry/auth classification: %v", err)
			}
			if requests != 1 {
				t.Fatalf("failed request replayed %d times", requests)
			}
		})
	}
}

func TestGrokRejectsUnsupportedEffortBeforeInference(t *testing.T) {
	selected, err := ResolveConnectionVariant(config.ProviderConnection{Protocol: config.ConnectionGrokCLIProxy})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: httpclient.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatal("unsupported effort reached inference")
		return nil, errors.New("unexpected request")
	})}
	provider, err := NewProviderClient(ProviderClientOptions{Registration: selected, Auth: oauthStaticAuth{}, HTTPClient: client,
		ContextWindowTokens: 256_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Generate(t.Context(), Request{
		Model: "grok-4.7", SessionID: textutil.Value("fixture"), ToolChoiceMode: ToolChoiceModeAutomatic,
		ReasoningEffort: "none", SupportsReasoningEffort: true,
	}, StreamCallbacks{})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid effort error = %v", err)
	}
}

func TestGrokUnknownModelForwardsCustomEffort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Reasoning struct{ Effort string } `json:"reasoning"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Reasoning.Effort != "future-effort" {
			t.Errorf("effort = %q", body.Reasoning.Effort)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"fixture\",\"status\":\"completed\",\"output\":[]}}\n\n")
	}))
	defer server.Close()
	selected, err := ResolveConnectionVariant(config.ProviderConnection{Protocol: config.ConnectionGrokCLIProxy})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewProviderClient(ProviderClientOptions{Registration: selected, Auth: oauthStaticAuth{},
		HTTPClient: newRewritingHTTPClient(t, server), ContextWindowTokens: 256_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Generate(t.Context(), Request{
		Model: "grok-future", SessionID: textutil.Value("fixture"), ToolChoiceMode: ToolChoiceModeAutomatic,
		ReasoningEffort: "future-effort", SupportsReasoningEffort: true,
	}, StreamCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGrokManualModelNativeSearchCompatibility(t *testing.T) {
	for _, model := range []string{"grok-4.5", "grok-future"} {
		t.Run(model, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Tools []struct{ Type string } `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				wantCount := 1
				if model == "grok-4.5" {
					wantCount = 0
				}
				if len(body.Tools) != wantCount {
					t.Errorf("native tools = %+v", body.Tools)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"output\":[]}}\n\n")
			}))
			defer server.Close()
			selected, err := ResolveConnectionVariant(config.ProviderConnection{Protocol: config.ConnectionGrokCLIProxy})
			if err != nil {
				t.Fatal(err)
			}
			provider, err := NewProviderClient(ProviderClientOptions{Registration: selected, Auth: oauthStaticAuth{},
				HTTPClient: newRewritingHTTPClient(t, server), ContextWindowTokens: 256_000,
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = provider.Generate(t.Context(), Request{
				Model: model, SessionID: textutil.Value("fixture"), ToolChoiceMode: ToolChoiceModeAutomatic,
				ReasoningEffort: "high", EnableNativeWebSearch: true,
			}, StreamCallbacks{})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGrokUsagePreservesAbsentAndZeroCounts(t *testing.T) {
	for _, rawUsage := range []string{`{}`, `{"input_tokens":0,"output_tokens":0,"input_tokens_details":{"cached_tokens":0}}`} {
		t.Run(rawUsage, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"output\":[],\"usage\":%s}}\n\n", rawUsage)
			}))
			defer server.Close()
			selected, err := ResolveConnectionVariant(config.ProviderConnection{Protocol: config.ConnectionGrokCLIProxy})
			if err != nil {
				t.Fatal(err)
			}
			provider, err := NewProviderClient(ProviderClientOptions{Registration: selected, Auth: oauthStaticAuth{},
				HTTPClient: newRewritingHTTPClient(t, server), ContextWindowTokens: 256_000,
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := provider.Generate(t.Context(), Request{
				Model: "grok-4.7", SessionID: textutil.Value("fixture"), ToolChoiceMode: ToolChoiceModeAutomatic,
			}, StreamCallbacks{})
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(result.Usage)
			if err != nil {
				t.Fatal(err)
			}
			var counts struct {
				Input  *int `json:"input_tokens"`
				Output *int `json:"output_tokens"`
				Cached *int `json:"cached_input_tokens"`
			}
			if err := json.Unmarshal(encoded, &counts); err != nil {
				t.Fatal(err)
			}
			if rawUsage == "{}" {
				if counts.Input != nil || counts.Output != nil || counts.Cached != nil {
					t.Fatalf("absent counts fabricated: %s", encoded)
				}
			} else if counts.Input == nil || counts.Output == nil || counts.Cached == nil || *counts.Input != 0 || *counts.Output != 0 || *counts.Cached != 0 {
				t.Fatalf("reported zeros lost: %s", encoded)
			}
		})
	}
}

func TestGrokContextUsageRemainsSeparateFromBilling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":12,\"output_tokens\":8,\"total_tokens\":20,\"context_details\":{\"input_tokens\":30,\"output_tokens\":7}}}}\n\n")
	}))
	defer server.Close()
	selected, err := ResolveConnectionVariant(config.ProviderConnection{Protocol: config.ConnectionGrokCLIProxy})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewProviderClient(ProviderClientOptions{Registration: selected, Auth: oauthStaticAuth{},
		HTTPClient: newRewritingHTTPClient(t, server), ContextWindowTokens: 256_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.Generate(t.Context(), Request{
		Model: "grok-4.7", SessionID: textutil.Value("fixture"), ToolChoiceMode: ToolChoiceModeAutomatic,
	}, StreamCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result.Usage)
	if err != nil {
		t.Fatal(err)
	}
	var projected struct {
		Context *struct {
			Tokens int    `json:"tokens"`
			Point  string `json:"measurement_point"`
		} `json:"context_usage"`
	}
	if err := json.Unmarshal(encoded, &projected); err != nil {
		t.Fatal(err)
	}
	if projected.Context == nil || projected.Context.Tokens != 37 || projected.Context.Point != "completed_response" {
		t.Fatalf("context measurement = %s", encoded)
	}
	if result.Usage.InputTokens == nil || *result.Usage.InputTokens != 12 || result.Usage.OutputTokens == nil || *result.Usage.OutputTokens != 8 {
		t.Fatalf("billing counts changed: %s", encoded)
	}
	var evidence struct {
		Total   int `json:"total_tokens"`
		Context struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"context_details"`
	}
	if result.ProviderEvidence.Usage == nil {
		t.Fatal("raw usage evidence missing")
	}
	if err := json.Unmarshal(*result.ProviderEvidence.Usage, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Total != 20 || evidence.Context.Input != 30 || evidence.Context.Output != 7 {
		t.Fatalf("raw usage changed: %+v", evidence)
	}
}

func TestGrokDispatchUsesSelectedRouteAndSupportedPayload(t *testing.T) {
	for _, protocol := range []config.ConnectionProtocol{config.ConnectionGrokCLIProxy, config.ConnectionGrokOAuthAPI, config.ConnectionGrokAPIKey} {
		t.Run(string(protocol), func(t *testing.T) {
			definition := config.ProviderConnection{Protocol: protocol}
			if protocol == config.ConnectionGrokAPIKey {
				definition.EnvironmentVariable = textutil.Value("GROK_KEY")
			}
			selected, err := ResolveConnectionVariant(definition)
			if err != nil {
				t.Fatal(err)
			}
			requests := 0
			var dispatched map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				var body struct {
					Model       string                           `json:"model"`
					ServiceTier string                           `json:"service_tier"`
					Reasoning   struct{ Effort, Summary string } `json:"reasoning"`
					Include     []string                         `json:"include"`
					Tools       []struct{ Type string }          `json:"tools"`
					Text        *struct{ Verbosity *string }     `json:"text"`
					Store       *bool                            `json:"store"`
					MaxTokens   *int                             `json:"max_output_tokens"`
					Temperature *float64                         `json:"temperature"`
					Metadata    json.RawMessage                  `json:"client_metadata"`
				}
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(raw, &dispatched); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(raw, &body); err != nil {
					t.Error(err)
				}
				if body.Model != "grok-4.7" || body.ServiceTier != "priority" || body.Reasoning.Effort != "high" || body.Reasoning.Summary != "concise" {
					t.Errorf("model controls = %+v", body)
				}
				if len(body.Include) != 1 || body.Include[0] != "reasoning.encrypted_content" || len(body.Tools) != 1 || body.Tools[0].Type != "web_search" {
					t.Errorf("Grok tool/reasoning controls = %+v", body)
				}
				if body.Store != nil || body.Text != nil && body.Text.Verbosity != nil || len(body.Metadata) != 0 {
					t.Errorf("OpenAI-only payload fields: %+v", body)
				}
				if body.MaxTokens == nil || *body.MaxTokens != 128 || body.Temperature == nil || *body.Temperature != 0.4 {
					t.Errorf("OAuth suppressed supported generation controls: %+v", body)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"output\":[]}}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			rewrite := newRewritingHTTPClient(t, server)
			client := &http.Client{Transport: httpclient.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != *selected.Variant.BaseURL+"/responses" {
					t.Errorf("wrong route: %s", r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer token" || r.Header.Get("User-Agent") != "kent-fixture/"+config.Version {
					t.Errorf("wrong auth/identity headers: %v", r.Header)
				}
				for _, name := range []string{"ChatGPT-Account-Id", "originator", "session-id", "x-codex-routing-hint", "x-codex-turn-state", "Content-Encoding"} {
					if r.Header.Get(name) != "" {
						t.Errorf("Grok received %s header", name)
					}
				}
				proxy := protocol == config.ConnectionGrokCLIProxy
				for name, expected := range map[string]string{
					"x-xai-token-auth": "xai-grok-cli", "x-grok-client-version": "1.0.46",
					"x-grok-model-override": "grok-4.7", "x-grok-agent-id": "kent-fixture",
				} {
					if !proxy {
						expected = ""
					}
					if r.Header.Get(name) != expected {
						t.Errorf("%s = %q, expected %q", name, r.Header.Get(name), expected)
					}
				}
				return rewrite.Transport.RoundTrip(r)
			})}
			var auth DispatchAuthProvider = oauthStaticAuth{}
			if protocol == config.ConnectionGrokAPIKey {
				auth = staticAuth{}
			}
			provider, err := NewProviderClient(ProviderClientOptions{Registration: selected, Model: "grok-4.7",
				Auth: auth, HTTPClient: client, ModelVerbosity: "high", Store: true,
				ProviderIdentifier: textutil.Value("kent-fixture"), ContextWindowTokens: 500_000,
				ProviderCapabilitiesOverride: &ProviderCapabilities{ProviderID: "chatgpt-codex", SupportsResponsesAPI: true, SupportsNativeWebSearch: true, SupportsFastMode: true},
			})
			if err != nil {
				t.Fatal(err)
			}
			request := Request{
				Model: "grok-4.7", SessionID: textutil.Value("fixture"), ToolChoiceMode: ToolChoiceModeAutomatic,
				ReasoningEffort: "high", SupportsReasoningEffort: true, FastMode: true, EnableNativeWebSearch: true,
				Temperature: 0.4, MaxTokens: 128,
			}
			_, err = provider.Generate(t.Context(), request, StreamCallbacks{})
			if err != nil {
				t.Fatal(err)
			}
			if requests != 1 {
				t.Fatalf("dispatched %d requests", requests)
			}
			mode := OpenAIAuthMode{}
			mode.IsOAuth = protocol != config.ConnectionGrokAPIKey
			raw, err := MarshalResponsesWirePayload(selected, RequestAsResponses(request), true, "high", mode,
				ProviderCapabilities{ProviderID: "chatgpt-codex", SupportsResponsesAPI: true, SupportsNativeWebSearch: true, SupportsFastMode: true})
			if err != nil {
				t.Fatal(err)
			}
			var inspected map[string]any
			if err := json.Unmarshal(raw, &inspected); err != nil {
				t.Fatal(err)
			}
			// Streaming is selected by the SDK dispatch method, not payload preparation.
			delete(dispatched, "stream")
			if !reflect.DeepEqual(inspected, dispatched) {
				t.Fatalf("offline/live payload mismatch: inspected=%v dispatched=%v", inspected, dispatched)
			}
		})
	}
}
