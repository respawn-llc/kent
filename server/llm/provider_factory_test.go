package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"core/server/auth"
	"core/shared/config"
	"core/shared/textutil"
)

func openAIClientFromProvider(t *testing.T, client Client) *ResponsesClient {
	t.Helper()
	if watchdog, ok := client.(*idleWatchdogClient); ok {
		client = watchdog.streamingModelClient
	}
	openAIClient, ok := client.(*ResponsesClient)
	if !ok {
		t.Fatalf("expected *ResponsesClient, got %T", client)
	}
	return openAIClient
}

func newResponsesClientFromOptions(t *testing.T, options ProviderClientOptions) *ResponsesClient {
	t.Helper()
	client, err := NewProviderClient(options)
	if err != nil {
		t.Fatalf("new provider client: %v", err)
	}
	return openAIClientFromProvider(t, client)
}

func httpTransportFromResponsesClient(t *testing.T, client *ResponsesClient) *HTTPTransport {
	t.Helper()
	transport, ok := client.transport.(*HTTPTransport)
	if !ok {
		t.Fatalf("expected *HTTPTransport, got %T", client.transport)
	}
	return transport
}

type providerTestAuth struct{}

func (providerTestAuth) ResolveDispatchAuth(context.Context) (*DispatchAuth, error) {
	return &DispatchAuth{Header: "Bearer test"}, nil
}

type providerTestMissingAuth struct{}

func (providerTestMissingAuth) ResolveDispatchAuth(context.Context) (*DispatchAuth, error) {
	return nil, auth.ErrAuthNotConfigured
}

func TestInferProviderFromModel(t *testing.T) {
	got, err := InferProviderFromModel("gpt-6-sol")
	if err != nil {
		t.Fatalf("infer openai provider: %v", err)
	}
	if got != ProviderOpenAI {
		t.Fatalf("expected openai provider, got %q", got)
	}
	got, err = InferProviderFromModel("claude-3-7-sonnet")
	if err != nil {
		t.Fatalf("infer anthropic provider: %v", err)
	}
	if got != ProviderAnthropic {
		t.Fatalf("expected anthropic provider, got %q", got)
	}
	if _, err := InferProviderFromModel("custom-model"); !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("expected unsupported provider inference for unknown model family, got %v", err)
	}
	for _, model := range []string{"grok-4.6", "grok-4.7"} {
		if got, err := InferProviderFromModel(model); err != nil || got != ProviderGrok {
			t.Fatalf("registered Grok model %s ownership = %q, %v", model, got, err)
		}
	}
	if _, err := InferProviderFromModel("grok-uncatalogued"); !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("uncatalogued Grok model acquired catalog ownership: %v", err)
	}
}

func TestNewProviderClient_OpenAI(t *testing.T) {
	httpClient := &http.Client{Timeout: 7 * time.Second}
	providerIdentifier := "factory-agent"
	openAIClient := newResponsesClientFromOptions(t, ProviderClientOptions{
		Model:              "gpt-6-sol",
		Registration:       testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL)}),
		Auth:               providerTestAuth{},
		HTTPClient:         httpClient,
		ModelVerbosity:     "HIGH",
		ProviderIdentifier: &providerIdentifier,
	})
	transport := httpTransportFromResponsesClient(t, openAIClient)
	if transport.Client != httpClient {
		t.Fatal("expected provider HTTP client override to be used")
	}
	if transport.ContextWindowTokens != 272_000 {
		t.Fatalf("expected context window from model metadata, got %d", transport.ContextWindowTokens)
	}
	if transport.ModelVerbosity != "high" {
		t.Fatalf("expected normalized model verbosity, got %q", transport.ModelVerbosity)
	}
	if transport.ProviderIdentifier != "factory-agent" {
		t.Fatalf("provider identifier = %q, want factory-agent", transport.ProviderIdentifier)
	}
}

func TestNewProviderClient_ResponsesClientPathCompressesCodexRequest(t *testing.T) {
	var requestEncoding string
	var acceptEncoding string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestEncoding = r.Header.Get("Content-Encoding")
		acceptEncoding = r.Header.Get("Accept-Encoding")
		_, _ = io.Copy(io.Discard, r.Body)
		writeCompletedResponseSSE(w)
	}))
	defer server.Close()

	client, err := NewProviderClient(ProviderClientOptions{
		Model:        "gpt-5.6-sol",
		Registration: testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionChatGPT}),
		Auth:         oauthStaticAuth{},
		HTTPClient:   newRewritingHTTPClient(t, server),
	})
	if err != nil {
		t.Fatalf("NewProviderClient: %v", err)
	}
	sessionID, dispatch := compressionDispatch(t, CodexRequestKindTurn)
	if _, err := client.Generate(context.Background(), Request{
		Model:          "gpt-5.6-sol",
		SessionID:      sessionID,
		CodexDispatch:  dispatch,
		ToolChoiceMode: ToolChoiceModeAutomatic,
		SystemPrompt:   strings.Repeat("large request content ", 100),
	}, StreamCallbacks{}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if requestEncoding != "zstd" {
		t.Fatalf("Content-Encoding = %q, want zstd", requestEncoding)
	}
	if acceptEncoding == "zstd,gzip" {
		t.Fatalf("injected client unexpectedly received Kent response negotiation: %q", acceptEncoding)
	}
}

func TestNewProviderClient_OAuthPathCompressesCodexRequest(t *testing.T) {
	var requestEncoding string
	var acceptEncoding string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestEncoding = r.Header.Get("Content-Encoding")
		acceptEncoding = r.Header.Get("Accept-Encoding")
		_, _ = io.Copy(io.Discard, r.Body)
		writeCompletedResponseSSE(w)
	}))
	defer server.Close()

	client, err := NewProviderClient(ProviderClientOptions{
		Model:        "gpt-5.6-sol",
		Registration: testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionChatGPT}),
		Auth:         oauthStaticAuth{},
		HTTPClient:   newRewritingHTTPClient(t, server),
	})
	if err != nil {
		t.Fatalf("NewProviderClient: %v", err)
	}
	sessionID, dispatch := compressionDispatch(t, CodexRequestKindTurn)
	if _, err := client.Generate(context.Background(), Request{
		Model:          "gpt-5.6-sol",
		SessionID:      sessionID,
		CodexDispatch:  dispatch,
		ToolChoiceMode: ToolChoiceModeAutomatic,
		SystemPrompt:   strings.Repeat("large request content ", 100),
	}, StreamCallbacks{}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if requestEncoding != "zstd" {
		t.Fatalf("Content-Encoding = %q, want zstd", requestEncoding)
	}
	if acceptEncoding == "zstd,gzip" {
		t.Fatalf("injected client unexpectedly received Kent response negotiation: %q", acceptEncoding)
	}
}

func TestNewProviderClient_LunaUsesCatalogMetadata(t *testing.T) {
	openAIClient := newResponsesClientFromOptions(t, ProviderClientOptions{
		Model:        "gpt-6-luna",
		Registration: testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL)}),
		Auth:         providerTestAuth{},
	})
	transport := httpTransportFromResponsesClient(t, openAIClient)
	if transport.ContextWindowTokens != 272_000 {
		t.Fatalf("expected luna context window from model metadata, got %d", transport.ContextWindowTokens)
	}
}

func TestNewProviderClient_AnthropicNotImplemented(t *testing.T) {
	_, err := NewProviderClient(ProviderClientOptions{
		Model:        "claude-3-7-sonnet",
		Registration: globalProviderRegistry.providerVariantsByID["anthropic"],
		Auth:         providerTestAuth{},
	})
	if !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("expected unsupported provider error, got %v", err)
	}
}

func TestNewProviderClient_ExplicitProviderOverrideAllowsCustomModelAlias(t *testing.T) {
	openAIClient := newResponsesClientFromOptions(t, ProviderClientOptions{
		Registration: testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL)}),
		Model:        "my-team-alias",
		Auth:         providerTestAuth{},
	})

	providerCaps, err := openAIClient.ProviderCapabilities(context.Background())
	if err != nil {
		t.Fatalf("provider capabilities: %v", err)
	}
	if providerCaps.ProviderID != "openai" || !providerCaps.IsOpenAIFirstParty {
		t.Fatalf("expected explicit provider override to bypass model inference, got %+v", providerCaps)
	}
}

func TestNewProviderClientRequiresResolvedRegistration(t *testing.T) {
	_, err := NewProviderClient(ProviderClientOptions{
		Model: "gpt-6-sol",
		Auth:  providerTestAuth{},
	})
	if err == nil {
		t.Fatal("client inferred transport policy without a resolved registration")
	}
}

func TestNewProviderClient_RemoteOpenAICompatibleBaseURLAllowsCustomModelFamily(t *testing.T) {
	openAIClient := newResponsesClientFromOptions(t, ProviderClientOptions{
		Model:          "vendor-custom-model",
		Auth:           providerTestAuth{},
		Registration:   testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value("https://example.openrouter.ai/api/v1")}),
		ModelVerbosity: "MEDIUM",
	})

	transport := httpTransportFromResponsesClient(t, openAIClient)
	if transport.serviceBaseURL() != "https://example.openrouter.ai/api/v1" {
		t.Fatalf("expected custom base url to be preserved, got %q", transport.serviceBaseURL())
	}

	providerCaps, err := openAIClient.ProviderCapabilities(context.Background())
	if err != nil {
		t.Fatalf("provider capabilities: %v", err)
	}
	if providerCaps.ProviderID != "openai-compatible" {
		t.Fatalf("expected remote base url to resolve openai-compatible provider id, got %+v", providerCaps)
	}
	if !providerCaps.SupportsResponsesAPI {
		t.Fatalf("expected responses api support, got %+v", providerCaps)
	}
	if providerCaps.SupportsResponsesCompact || providerCaps.SupportsNativeWebSearch || providerCaps.IsOpenAIFirstParty {
		t.Fatalf("expected conservative remote provider capabilities, got %+v", providerCaps)
	}
}

func TestNewProviderClient_RemoteOpenAICompatibleBaseURLAllowsAnonymousCapabilitiesResolution(t *testing.T) {
	openAIClient := newResponsesClientFromOptions(t, ProviderClientOptions{
		Model:        "vendor-custom-model",
		Auth:         anonymousAuth{},
		Registration: testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value("https://example.openrouter.ai/api/v1")}),
	})

	providerCaps, err := openAIClient.ProviderCapabilities(context.Background())
	if err != nil {
		t.Fatalf("provider capabilities: %v", err)
	}
	if providerCaps.ProviderID != "openai-compatible" {
		t.Fatalf("expected remote base url to resolve openai-compatible provider id, got %+v", providerCaps)
	}
}

func TestNewProviderClient_KeepsExplicitOpenAIBaseURLExplicit(t *testing.T) {
	openAIClient := newResponsesClientFromOptions(t, ProviderClientOptions{
		Model:        "gpt-6-sol",
		Auth:         providerTestMissingAuth{},
		Registration: testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value("https://api.openai.com")}),
	})
	transport := httpTransportFromResponsesClient(t, openAIClient)
	if transport.serviceBaseURL() != defaultOpenAIBaseURL {
		t.Fatal("expected configured OpenAI URL")
	}
}

func TestNewProviderClient_LocalBaseURLUsesUncompressedTransportByDefault(t *testing.T) {
	openAIClient := newResponsesClientFromOptions(t, ProviderClientOptions{
		Model:        "vendor-custom-model",
		Auth:         providerTestMissingAuth{},
		Registration: testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value("http://127.0.0.1:11434/v1")}),
	})
	transport := httpTransportFromResponsesClient(t, openAIClient)
	if transport.Client.Transport != sharedHTTPTransport {
		t.Fatalf("local provider transport = %T, want shared uncompressed transport", transport.Client.Transport)
	}
}

func TestProviderErrorReducerForUnknownIDFailsFast(t *testing.T) {
	_, err := providerErrorReducerForID("custom-provider-id")
	if err == nil {
		t.Fatal("expected missing provider reducer error")
	}
}
