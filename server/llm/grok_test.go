package llm

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"core/internal/testharness/httpclient"
	"core/shared/config"
	"core/shared/textutil"
)

func TestGrokRejectsUnsupportedEffortBeforeInference(t *testing.T) {
	selected, err := ResolveConnectionVariant(config.ProviderConnection{Protocol: config.ConnectionGrokCLIProxy})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: httpclient.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatal("unsupported effort reached inference")
		return nil, errors.New("unexpected request")
	})}
	provider, err := NewProviderClient(ProviderClientOptions{
		Provider: selected.Provider, Variant: &selected.Variant, Auth: oauthStaticAuth{}, HTTPClient: client,
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
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
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
			provider, err := NewProviderClient(ProviderClientOptions{
				Provider: selected.Provider, Variant: &selected.Variant, Model: "grok-4.7",
				Auth: auth, HTTPClient: client, ModelVerbosity: "high", Store: true,
				ProviderIdentifier: textutil.Value("kent-fixture"), ContextWindowTokens: 500_000,
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = provider.Generate(t.Context(), Request{
				Model: "grok-4.7", SessionID: textutil.Value("fixture"), ToolChoiceMode: ToolChoiceModeAutomatic,
				ReasoningEffort: "high", SupportsReasoningEffort: true, FastMode: true, EnableNativeWebSearch: true,
				Temperature: 0.4, MaxTokens: 128,
			}, StreamCallbacks{})
			if err != nil {
				t.Fatal(err)
			}
			if requests != 1 {
				t.Fatalf("dispatched %d requests", requests)
			}
		})
	}
}
