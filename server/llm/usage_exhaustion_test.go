package llm

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"core/shared/config"
	"core/shared/llmerrors"
	"core/shared/textutil"
)

func TestResponsesHTTPUsageExhaustion(t *testing.T) {
	body, err := os.ReadFile("testdata/usage-exhaustion/openai-quota.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range [][]byte{body, []byte(`{"error":{"type":"insufficient_quota"}}`)} {
		t.Run(string(body), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write(body)
			}))
			defer server.Close()
			registration, err := ResolveConnectionVariant(config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(server.URL)})
			if err != nil {
				t.Fatal(err)
			}
			client, err := NewProviderClient(ProviderClientOptions{Registration: registration, Auth: &countingAuth{}, ConnectionID: textutil.Value(config.ConnectionID("fixture")), ContextWindowTokens: 256_000})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Generate(t.Context(), Request{Model: "gpt-6-sol", SessionID: textutil.Value("fixture"), ToolChoiceMode: ToolChoiceModeAutomatic, ReasoningEffort: "high"}, StreamCallbacks{})
			var failure *ProviderAPIError
			if !errors.As(err, &failure) || !IsUsageExhaustionError(err) || !IsNonRetriableModelError(err) || failure.ConnectionID == nil || *failure.ConnectionID != "fixture" {
				t.Fatalf("quota failure = %v", err)
			}
		})
	}
}

func TestGrokNumericWKEUsageExhaustion(t *testing.T) {
	body, err := os.ReadFile("testdata/usage-exhaustion/grok-wke.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	registration, err := ResolveConnectionVariant(config.ProviderConnection{Protocol: config.ConnectionGrokCLIProxy})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewProviderClient(ProviderClientOptions{Registration: registration, Auth: oauthStaticAuth{}, HTTPClient: newRewritingHTTPClient(t, server), ContextWindowTokens: 256_000})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Generate(t.Context(), Request{Model: "grok-4.7", SessionID: textutil.Value("fixture"), ToolChoiceMode: ToolChoiceModeAutomatic, ReasoningEffort: "high"}, StreamCallbacks{})
	var failure *ProviderAPIError
	if !errors.As(err, &failure) || !IsUsageExhaustionError(err) || failure.ProviderCode != "personal-team-blocked:spending-limit" || failure.Raw == "" {
		t.Fatalf("WKE failure = %+v", err)
	}
}

func TestGrokUsageExhaustionSignals(t *testing.T) {
	for _, test := range []struct {
		protocol  config.ConnectionProtocol
		status    int
		body      string
		exhausted bool
	}{
		{config.ConnectionGrokCLIProxy, 402, `{"error":"fixture credit requirement"}`, true},
		{config.ConnectionGrokOAuthAPI, 402, `{"error":"fixture credit requirement"}`, false},
		{config.ConnectionGrokAPIKey, 402, `{"error":"fixture credit requirement"}`, false},
		{config.ConnectionGrokOAuthAPI, 403, `{"error":{"code":"personal-team-blocked:spending-limit","message":"fixture"}}`, true},
		{config.ConnectionGrokCLIProxy, 429, `{"code":"429","error":"fixture [WKE=personal-team-blocked:spending-limit]"}`, true},
		{config.ConnectionGrokOAuthAPI, 429, `{"code":429,"error":"fixture rate limit"}`, false},
		{config.ConnectionGrokOAuthAPI, 403, `{"error":"fixture permission"}`, false},
		{config.ConnectionGrokCLIProxy, 429, `{"code":429,"error":"fixture [WKE=personal-team-blocked:spending-limit"}`, false},
		{config.ConnectionGrokCLIProxy, 429, `{"code":429,"error":"fixture [WKE=unknown]"}`, false},
		{config.ConnectionGrokCLIProxy, 429, `{"code":429,"error":"fixture [WKE= personal-team-blocked:spending-limit]"}`, false},
		{config.ConnectionGrokCLIProxy, 401, `{"code":429,"error":"fixture [WKE=personal-team-blocked:spending-limit]"}`, false},
		{config.ConnectionGrokCLIProxy, 426, `{"code":"personal-team-blocked:spending-limit","error":"fixture"}`, false},
		{config.ConnectionGrokAPIKey, 429, `{"error":{"code":"insufficient_quota","message":"fixture"}}`, false},
	} {
		t.Run(string(test.protocol)+"/"+http.StatusText(test.status)+"/"+test.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			connection := config.ProviderConnection{Protocol: test.protocol}
			if test.protocol == config.ConnectionGrokAPIKey {
				connection.EnvironmentVariable = textutil.Value("FIXTURE_GROK_KEY")
			}
			registration, err := ResolveConnectionVariant(connection)
			if err != nil {
				t.Fatal(err)
			}
			client, err := NewProviderClient(ProviderClientOptions{Registration: registration, Auth: oauthStaticAuth{}, HTTPClient: newRewritingHTTPClient(t, server), ContextWindowTokens: 256_000})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Generate(t.Context(), Request{Model: "grok-4.7", SessionID: textutil.Value("fixture"), ToolChoiceMode: ToolChoiceModeAutomatic, ReasoningEffort: "high"}, StreamCallbacks{})
			var failure *ProviderAPIError
			if !errors.As(err, &failure) || IsUsageExhaustionError(err) != test.exhausted || failure.Raw == "" {
				t.Fatalf("classified Grok failure = %v", err)
			}
			if test.status == http.StatusPaymentRequired && !test.exhausted {
				if failure.Code != llmerrors.UnifiedErrorCodeEntitlement || !IsNonRetriableModelError(err) {
					t.Fatalf("public-route payment failure must remain terminal entitlement: %+v", failure)
				}
			}
		})
	}
}

func TestResponsesStructuredUsageExhaustion(t *testing.T) {
	for _, test := range []struct {
		protocol  config.ConnectionProtocol
		status    int
		code      string
		typ       string
		exhausted bool
	}{
		{config.ConnectionResponses, 429, "insufficient_quota", "", true},
		{config.ConnectionResponses, 429, "credit_balance_exhausted", "", true},
		{config.ConnectionResponses, 429, "organization_spend_limit_exceeded", "", true},
		{config.ConnectionResponses, 429, "project_spend_limit_exceeded", "", true},
		{config.ConnectionResponses, 429, "organization_usage_limit_exceeded", "", true},
		{config.ConnectionChatGPT, 429, "", "usage_limit_reached", true},
		{config.ConnectionResponses, 429, "", "", false},
		{config.ConnectionResponses, 429, "rate_limit_exceeded", "", false},
		{config.ConnectionResponses, 503, "server_is_overloaded", "", false},
		{config.ConnectionResponses, 401, "insufficient_quota", "", false},
		{config.ConnectionResponses, 426, "insufficient_quota", "", false},
		{config.ConnectionResponses, 429, "usage_limit_reached", "", false},
	} {
		t.Run(string(test.protocol)+"/"+test.code+"/"+test.typ+http.StatusText(test.status), func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"error": map[string]string{"code": test.code, "type": test.typ, "message": "fixture"}})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				_, _ = w.Write(body)
			}))
			defer server.Close()
			connection := config.ProviderConnection{Protocol: test.protocol}
			if test.protocol == config.ConnectionResponses {
				connection.Endpoint = textutil.Value(server.URL)
			}
			registration, err := ResolveConnectionVariant(connection)
			if err != nil {
				t.Fatal(err)
			}
			client, err := NewProviderClient(ProviderClientOptions{Registration: registration, Auth: oauthStaticAuth{}, HTTPClient: newRewritingHTTPClient(t, server), ContextWindowTokens: 256_000})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Generate(t.Context(), Request{Model: "gpt-6-sol", SessionID: textutil.Value("session-1"), CodexDispatch: newTestCodexDispatch(t), ToolChoiceMode: ToolChoiceModeAutomatic, ReasoningEffort: "high"}, StreamCallbacks{})
			var failure *ProviderAPIError
			if !errors.As(err, &failure) || IsUsageExhaustionError(err) != test.exhausted || failure.ProviderCode != test.code || failure.ProviderType != test.typ {
				t.Fatalf("classified failure = %+v", err)
			}
		})
	}
}

func TestResponsesStreamUsageExhaustion(t *testing.T) {
	for _, event := range []string{
		`{"type":"error","error":{"type":"insufficient_quota","message":"fixture"}}`,
		`{"type":"error","error":{"type":"insufficient_quota"}}`,
		`{"type":"response.failed","response":{"error":{"code":"credit_balance_exhausted","message":"fixture"}}}`,
	} {
		transport := newOpenAIStreamTestTransport(t, event)
		_, err := transport.Generate(t.Context(), ResponsesRequest{
			Model: "gpt-6-sol", SessionID: textutil.Value("fixture"), ToolChoiceMode: ToolChoiceModeAutomatic, ReasoningEffort: "high",
		}, StreamCallbacks{})
		if !IsUsageExhaustionError(err) || !IsNonRetriableModelError(err) {
			t.Fatalf("stream failure = %v", err)
		}
	}
}
