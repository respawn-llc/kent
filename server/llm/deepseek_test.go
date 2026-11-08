package llm

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"core/shared/config"
	"core/shared/textutil"
)

func TestDeepSeekConnectionUsesOfficialBaseUnlessEndpointIsSet(t *testing.T) {
	official, err := ResolveConnectionVariant(config.ProviderConnection{
		Protocol: config.ConnectionDeepSeekAPIKey, EnvironmentVariable: textutil.Value("DEEPSEEK_API_KEY"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if official.Provider != ProviderDeepSeek || official.Variant.BaseURL == nil || *official.Variant.BaseURL != config.DefaultDeepSeekResponsesEndpoint {
		t.Fatalf("official transport = %+v", official.Variant.BaseURL)
	}
	if official.Variant.generationProtocol != responsesSSE || official.Variant.Capabilities.SupportsResponsesCompact || official.Variant.Capabilities.SupportsNativeWebSearch {
		t.Fatalf("transport policy = %+v", official.Variant)
	}
	custom, err := ResolveConnectionVariant(config.ProviderConnection{
		Protocol: config.ConnectionDeepSeekAPIKey, EnvironmentVariable: textutil.Value("DEEPSEEK_API_KEY"),
		Endpoint: textutil.Value("https://models.example.test/deepseek/"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if custom.Variant.BaseURL == nil || *custom.Variant.BaseURL != "https://models.example.test/deepseek" {
		t.Fatalf("custom base = %v", custom.Variant.BaseURL)
	}
}

func TestDeepSeekPayloadStaysStateless(t *testing.T) {
	var request *http.Request
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request = r.Clone(r.Context())
		var err error
		body, err = io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"role\":\"assistant\",\"status\":\"completed\",\"phase\":\"final_answer\",\"content\":[{\"type\":\"output_text\",\"text\":\"pong\"}]}}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"model\":\"deepseek-flash\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"role\":\"assistant\",\"status\":\"completed\",\"phase\":\"final_answer\",\"content\":[{\"type\":\"output_text\",\"text\":\"pong\"}]}]}}\n\n"+
			"data: [DONE]\n\n")
	}))
	defer server.Close()
	registration, err := ResolveConnectionVariant(config.ProviderConnection{
		Protocol: config.ConnectionDeepSeekAPIKey, EnvironmentVariable: textutil.Value("DEEPSEEK_API_KEY"),
		Endpoint: textutil.Value(server.URL),
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewProviderClient(ProviderClientOptions{
		Registration: registration, Auth: &countingAuth{}, Store: true, ModelVerbosity: "high",
		ContextWindowTokens: 1_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Generate(t.Context(), Request{
		Model: "deepseek-flash", SessionID: textutil.Value("session"), PromptCacheKey: "cache",
		ReasoningEffort: "high", SupportsReasoningEffort: true, FastMode: true,
		MaxTokens: 32, Temperature: 0.2, ToolChoiceMode: ToolChoiceModeAutomatic,
		SystemPrompt: "Stay brief.",
	}, StreamCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Assistant.Content == nil || *response.Assistant.Content != "pong" {
		t.Fatalf("assistant = %+v", response.Assistant.Content)
	}
	if request == nil || request.URL.Path != "/responses" || request.Header.Get("Authorization") != "Bearer token" {
		t.Fatalf("request = %+v", request)
	}
	if request.Header.Get("originator") != "" || request.Header.Get("session-id") != "" {
		t.Fatalf("OpenAI headers = %v", request.Header)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"store", "include", "previous_response_id", "prompt_cache_key", "service_tier"} {
		if _, present := payload[forbidden]; present {
			t.Fatalf("payload contains %s: %s", forbidden, body)
		}
	}
	reasoning, _ := payload["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" || reasoning["summary"] != nil {
		t.Fatalf("reasoning = %#v", reasoning)
	}
	if payload["instructions"] != "Stay brief." {
		t.Fatalf("instructions = %#v", payload["instructions"])
	}
}

func TestDeepSeekRejectsUnsupportedEffortBeforeDispatch(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	registration, err := ResolveConnectionVariant(config.ProviderConnection{
		Protocol: config.ConnectionDeepSeekAPIKey, EnvironmentVariable: textutil.Value("DEEPSEEK_API_KEY"),
		Endpoint: textutil.Value(server.URL),
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewProviderClient(ProviderClientOptions{Registration: registration, Auth: &countingAuth{}, ContextWindowTokens: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Generate(t.Context(), Request{
		Model: "deepseek-flash", SessionID: textutil.Value("session"), ReasoningEffort: "medium", ToolChoiceMode: ToolChoiceModeAutomatic,
	}, StreamCallbacks{})
	if !errors.Is(err, ErrInvalidRequest) || requests != 0 {
		t.Fatalf("err = %v, requests = %d", err, requests)
	}
}

func TestDeepSeekPaymentRequiredIsUsageExhaustion(t *testing.T) {
	for _, test := range []struct {
		status    int
		exhausted bool
	}{
		{http.StatusPaymentRequired, true},
		{http.StatusTooManyRequests, false},
		{http.StatusUnauthorized, false},
		{http.StatusBadRequest, false},
		{http.StatusUnprocessableEntity, false},
	} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"error":{"message":"fixture"}}`))
			}))
			defer server.Close()
			registration, err := ResolveConnectionVariant(config.ProviderConnection{
				Protocol: config.ConnectionDeepSeekAPIKey, EnvironmentVariable: textutil.Value("DEEPSEEK_API_KEY"),
				Endpoint: textutil.Value(server.URL),
			})
			if err != nil {
				t.Fatal(err)
			}
			client, err := NewProviderClient(ProviderClientOptions{Registration: registration, Auth: &countingAuth{}, ContextWindowTokens: 1_000_000})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Generate(t.Context(), Request{
				Model: "deepseek-flash", SessionID: textutil.Value("session"), ReasoningEffort: "high", ToolChoiceMode: ToolChoiceModeAutomatic,
			}, StreamCallbacks{})
			if IsUsageExhaustionError(err) != test.exhausted {
				t.Fatalf("exhausted = %t, err = %v", test.exhausted, err)
			}
		})
	}
}

func TestDeepSeekCatalogPricesAndVision(t *testing.T) {
	flash, ok := LookupModelCapabilityContract("deepseek-flash")
	pro, proOK := LookupModelCapabilityContract("deepseek-v4-pro")
	if !ok || !proOK || flash.TokenPricing == nil || pro.TokenPricing == nil {
		t.Fatal("missing DeepSeek catalog entries")
	}
	if flash.ContextWindowTokens != 1_000_000 || pro.ContextWindowTokens != 1_000_000 {
		t.Fatalf("windows = %d %d", flash.ContextWindowTokens, pro.ContextWindowTokens)
	}
	if *flash.TokenPricing != (TokenPricing{300, 6, 1_200}) || *pro.TokenPricing != (TokenPricing{1_320, 44, 3_960}) {
		t.Fatalf("prices = %+v %+v", flash.TokenPricing, pro.TokenPricing)
	}
	if !flash.SupportsVisionInputs || pro.SupportsVisionInputs {
		t.Fatalf("vision flash=%t pro=%t", flash.SupportsVisionInputs, pro.SupportsVisionInputs)
	}
	if flash.DefaultReasoningEffort == nil || *flash.DefaultReasoningEffort != "high" {
		t.Fatalf("default effort = %v", flash.DefaultReasoningEffort)
	}
	for _, effort := range []string{"medium", "xhigh"} {
		if err := ValidateModelReasoningEffort("deepseek-v4-pro", effort); err == nil {
			t.Fatalf("accepted %s", effort)
		}
	}
	if err := ValidateModelReasoningEffort("deepseek-flash", "none"); err != nil {
		t.Fatal(err)
	}
}
