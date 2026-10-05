package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"

	"core/server/httpcompression"
	"core/server/session"
	"core/shared/config"
)

func TestConnectionCapabilitiesPreserveLockedRequestContract(t *testing.T) {
	id := config.ConnectionID("local")
	settings := config.Settings{
		Model: "claude-model-alias", Connection: &id,
		Connections: map[config.ConnectionID]config.ProviderConnection{
			id: {Protocol: config.ConnectionResponses, Endpoint: openaiTestOptionalString("http://localhost:1234")},
		},
	}
	locked := &session.LockedContract{ProviderContract: session.LockedProviderCapabilities{
		ProviderID: "chatgpt-codex", SupportsResponsesAPI: true, SupportsReasoningEncrypted: true,
	}}
	resolved, err := ResolveEffectiveProviderCapabilities(locked, settings)
	if err != nil {
		t.Fatal(err)
	}
	transport, err := ResolveRuntimeProviderCapabilities(settings)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ProviderID != "chatgpt-codex" || transport.ProviderID != "openai-compatible" ||
		!resolved.SupportsReasoningEncrypted || transport.SupportsReasoningEncrypted {
		t.Fatalf("historical contract and actual transport were conflated: %+v", resolved)
	}
	settings.Connections[id] = config.ProviderConnection{Protocol: config.ConnectionChatGPT}
	actual, err := ResolveRuntimeProviderCapabilities(settings)
	if err != nil || actual.ProviderID != "chatgpt-codex" {
		t.Fatalf("declared subscription must resolve without credentials or model-family inference: %+v, %v", actual, err)
	}
}

func TestFastModeUsesCurrentConnectionCapabilityWithLockedProviderFacts(t *testing.T) {
	locked := &session.LockedContract{ProviderContract: session.LockedProviderCapabilities{
		ProviderID: "anthropic",
	}}
	tests := []struct {
		name       string
		connection config.ProviderConnection
	}{
		{
			name: "first-party default",
			connection: config.ProviderConnection{
				Protocol: config.ConnectionResponses,
				Endpoint: openaiTestOptionalString("https://api.openai.com/v1"),
			},
		},
		{
			name: "explicit compatible capability",
			connection: config.ProviderConnection{
				Protocol: config.ConnectionResponses,
				Endpoint: openaiTestOptionalString("http://localhost:1234/v1"),
				Capabilities: config.ProviderCapabilitiesOverride{
					ProviderID: "local-provider", SupportsResponsesAPI: true, SupportsFastMode: true,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id := config.ConnectionID("current")
			settings := config.Settings{
				Model: "custom-model", Connection: &id,
				Connections: map[config.ConnectionID]config.ProviderConnection{id: test.connection},
			}
			resolved, err := ResolveEffectiveProviderCapabilities(locked, settings)
			if err != nil {
				t.Fatalf("ResolveEffectiveProviderCapabilities: %v", err)
			}
			if resolved.ProviderID != "anthropic" {
				t.Fatalf("locked request provider = %q, want anthropic", resolved.ProviderID)
			}
			if !SupportsFastModeProvider(resolved) {
				t.Fatalf("current connection fast-mode capability was not used: %+v", resolved)
			}
		})
	}
}

func TestConnectionCapabilityOverrides(t *testing.T) {
	id := config.ConnectionID("local")
	settings := config.Settings{Connection: &id, Connections: map[config.ConnectionID]config.ProviderConnection{
		id: {
			Protocol: config.ConnectionResponses, Endpoint: openaiTestOptionalString("http://localhost:1234"),
			Capabilities: config.ProviderCapabilitiesOverride{
				ProviderID: "custom", SupportsResponsesAPI: true,
				SupportsFastMode: true, SupportsProviderVerbosity: true,
			},
		},
	}}
	resolved, err := ResolveEffectiveProviderCapabilities(nil, settings)
	if err != nil || resolved.ProviderID != "custom" || !resolved.SupportsProviderVerbosity {
		t.Fatalf("connection capability override = %+v, %v", resolved, err)
	}
	if !SupportsFastModeProvider(resolved) {
		t.Fatalf("selected connection did not project its fast-mode capability: %+v", resolved)
	}
}

func TestInferProviderCapabilities_UsesRegistryContracts(t *testing.T) {
	openai, err := InferProviderCapabilities("openai")
	if err != nil {
		t.Fatalf("infer openai capabilities: %v", err)
	}
	if !openai.SupportsResponsesCompact || !openai.IsOpenAIFirstParty || !openai.SupportsNativeWebSearch {
		t.Fatalf("expected first-party openai compact support, got %+v", openai)
	}
	if !openai.SupportsPromptCacheKey {
		t.Fatalf("expected openai prompt cache key support, got %+v", openai)
	}
	if !openai.SupportsFastMode {
		t.Fatalf("expected first-party OpenAI fast-mode support, got %+v", openai)
	}
	if !openai.SupportsProviderVerbosity {
		t.Fatalf("expected openai provider verbosity support, got %+v", openai)
	}

	oauth, err := InferProviderCapabilities("chatgpt-codex")
	if err != nil {
		t.Fatalf("infer codex capabilities: %v", err)
	}
	if oauth.ProviderID != "chatgpt-codex" || !oauth.SupportsResponsesCompact || !oauth.IsOpenAIFirstParty || !oauth.SupportsNativeWebSearch {
		t.Fatalf("unexpected oauth capabilities: %+v", oauth)
	}
	if !oauth.SupportsPromptCacheKey {
		t.Fatalf("expected chatgpt-codex prompt cache key support, got %+v", oauth)
	}
	if !oauth.SupportsFastMode {
		t.Fatalf("expected ChatGPT subscription fast-mode support, got %+v", oauth)
	}
	if !oauth.SupportsProviderVerbosity {
		t.Fatalf("expected chatgpt-codex provider verbosity support, got %+v", oauth)
	}
}

func TestRemoteCompactionProtocolsAreProviderOwned(t *testing.T) {
	openAI, ok := lookupProviderVariantContract("openai")
	if !ok || openAI.Variant.RemoteCompactionProtocol != remoteCompactionResponsesTriggerV2 {
		t.Fatalf("openai compaction protocol = %v, want Responses-trigger V2", openAI.Variant.RemoteCompactionProtocol)
	}
	codex, ok := lookupProviderVariantContract("chatgpt-codex")
	if !ok || codex.Variant.RemoteCompactionProtocol != remoteCompactionResponsesTriggerV2 {
		t.Fatalf("chatgpt-codex compaction protocol = %v, want Responses-trigger V2", codex.Variant.RemoteCompactionProtocol)
	}
	compatible, ok := lookupProviderVariantContract("openai-compatible")
	if !ok || compatible.Variant.RemoteCompactionProtocol != remoteCompactionUnsupported {
		t.Fatalf("openai-compatible compaction protocol = %v, want unsupported", compatible.Variant.RemoteCompactionProtocol)
	}
}

func TestProviderVariantRequestCompressionDefaults(t *testing.T) {
	tests := []struct {
		providerID string
		want       httpcompression.RequestContentCoding
	}{
		{providerID: "chatgpt-codex", want: httpcompression.ContentCodingZstd},
		{providerID: "openai", want: httpcompression.ContentCodingIdentity},
		{providerID: "openai-compatible", want: httpcompression.ContentCodingIdentity},
		{providerID: "anthropic", want: httpcompression.ContentCodingIdentity},
	}

	for _, test := range tests {
		t.Run(test.providerID, func(t *testing.T) {
			registration, ok := lookupProviderVariantContract(test.providerID)
			if !ok {
				t.Fatalf("missing provider variant %q", test.providerID)
			}
			if registration.Variant.RequestCompression != test.want {
				t.Fatalf("request compression = %q, want %q", registration.Variant.RequestCompression, test.want)
			}
		})
	}
}

func TestInferProviderCapabilities_UnknownProviderFailsExplicitly(t *testing.T) {
	_, err := InferProviderCapabilities("custom-provider")
	if !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("expected unsupported provider error, got %v", err)
	}
}

func TestHTTPTransportPreservesConfiguredCapabilitiesOverrideAcrossEndpointVariant(t *testing.T) {
	transport := NewHTTPTransport(oauthStaticAuth{})
	transport.BaseURL = "https://proxy.example/v1"
	transport.BaseURLExplicit = true
	transport.ProviderCapabilitiesOverride = &ProviderCapabilities{
		ProviderID:                    "chatgpt-codex",
		SupportsResponsesAPI:          true,
		SupportsFastMode:              true,
		SupportsResponsesCompact:      true,
		SupportsNativeWebSearch:       true,
		SupportsReasoningEncrypted:    true,
		SupportsServerSideContextEdit: true,
		SupportsProviderVerbosity:     true,
		IsOpenAIFirstParty:            true,
	}

	caps, err := transport.ProviderCapabilities(context.Background())
	if err != nil {
		t.Fatalf("ProviderCapabilities: %v", err)
	}
	if caps.ProviderID != "chatgpt-codex" || !caps.SupportsResponsesCompact ||
		!caps.SupportsFastMode || !caps.IsOpenAIFirstParty {
		t.Fatalf("capabilities = %+v, want configured capabilities override", caps)
	}
}

func TestResolveOpenAITransportProviderVariant_DefaultLoopbackAndRemoteCompatibleBaseURL(t *testing.T) {
	endpoint := func(raw string, explicit bool) ProviderTransportEndpoint {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse endpoint %q: %v", raw, err)
		}
		return ProviderTransportEndpoint{URL: parsed, Explicit: explicit}
	}
	if got, err := resolveOpenAITransportProviderVariant(ProviderTransportEndpoint{}, OpenAIAuthMode{}); err != nil || got != "openai" {
		t.Fatalf("expected default base url to resolve openai variant, got variant=%q err=%v", got, err)
	}
	if got, err := resolveOpenAITransportProviderVariant(endpoint("https://api.openai.com/v1/", true), OpenAIAuthMode{}); err != nil || got != "openai" {
		t.Fatalf("expected normalized default base url to resolve openai variant, got variant=%q err=%v", got, err)
	}
	if got, err := resolveOpenAITransportProviderVariant(endpoint("https://api.openai.com", true), OpenAIAuthMode{}); err != nil || got != "openai" {
		t.Fatalf("expected bare api.openai.com base url to resolve openai variant, got variant=%q err=%v", got, err)
	}
	if got, err := resolveOpenAITransportProviderVariant(endpoint("http://127.0.0.1:8080/v1", true), OpenAIAuthMode{}); err != nil || got != "openai-compatible" {
		t.Fatalf("expected loopback base url to resolve openai-compatible variant, got variant=%q err=%v", got, err)
	}
	if got, err := resolveOpenAITransportProviderVariant(endpoint("https://example.openai.azure.com/openai/v1", true), OpenAIAuthMode{}); err != nil || got != "openai-compatible" {
		t.Fatalf("expected remote compatible base url to resolve openai-compatible variant, got variant=%q err=%v", got, err)
	}
	if got, err := resolveOpenAITransportProviderVariant(ProviderTransportEndpoint{}, OpenAIAuthMode{IsOAuth: true}); err != nil || got != "chatgpt-codex" {
		t.Fatalf("expected implicit oauth mode to resolve chatgpt-codex variant, got variant=%q err=%v", got, err)
	}
	if got, err := resolveOpenAITransportProviderVariant(endpoint("https://proxy.example/backend-api/codex", true), OpenAIAuthMode{IsOAuth: true}); err != nil || got != "openai-compatible" {
		t.Fatalf("expected custom explicit oauth endpoint to resolve compatible variant, got variant=%q err=%v", got, err)
	}
	if got, err := resolveOpenAITransportProviderVariant(endpoint("http://127.0.0.1/backend-api/codex", true), OpenAIAuthMode{IsOAuth: true}); err != nil || got != "openai-compatible" {
		t.Fatalf("expected loopback explicit oauth endpoint to resolve compatible variant, got variant=%q err=%v", got, err)
	}
	if got, err := resolveOpenAITransportProviderVariant(endpoint("https://chatgpt.com/backend-api/codex", true), OpenAIAuthMode{IsOAuth: true}); err != nil || got != "chatgpt-codex" {
		t.Fatalf("expected canonical explicit oauth endpoint to resolve chatgpt-codex variant, got variant=%q err=%v", got, err)
	}
	for _, endpoint := range []string{
		"https://chatgpt.com/backend-api/codex?proxy=true",
		"https://chatgpt.com/backend-api/codex?",
		"https://user:secret@chatgpt.com/backend-api/codex",
	} {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			t.Fatalf("parse endpoint %q: %v", endpoint, err)
		}
		if got, err := resolveOpenAITransportProviderVariant(ProviderTransportEndpoint{URL: parsed, Explicit: true}, OpenAIAuthMode{IsOAuth: true}); err != nil || got != "openai-compatible" {
			t.Fatalf("expected non-canonical explicit oauth endpoint %q to resolve compatible variant, got variant=%q err=%v", endpoint, got, err)
		}
	}
}

func TestIsOpenAIFirstPartyBaseURL(t *testing.T) {
	if !IsOpenAIFirstPartyBaseURL("https://api.openai.com") {
		t.Fatal("expected bare api.openai.com to be treated as first-party OpenAI")
	}
	if !IsOpenAIFirstPartyBaseURL("https://api.openai.com/v1") {
		t.Fatal("expected default OpenAI /v1 endpoint to be treated as first-party OpenAI")
	}
	if IsOpenAIFirstPartyBaseURL("https://example.test/v1") {
		t.Fatal("did not expect non-OpenAI endpoint to be treated as first-party OpenAI")
	}
	if IsOpenAIFirstPartyBaseURL("http://127.0.0.1:11434/v1") {
		t.Fatal("did not expect loopback endpoint to be treated as first-party OpenAI")
	}
}

func TestKnownNonFirstPartyProviderContractsRemainLocalCompactionOnly(t *testing.T) {
	for _, providerID := range []string{"anthropic", "openai-compatible"} {
		caps, err := InferProviderCapabilities(providerID)
		if err != nil {
			t.Fatalf("infer %s capabilities: %v", providerID, err)
		}
		if caps.SupportsResponsesCompact {
			t.Fatalf("expected compact unsupported for %s, got %+v", providerID, caps)
		}
		if caps.IsOpenAIFirstParty {
			t.Fatalf("expected third-party classification for %s, got %+v", providerID, caps)
		}
		if caps.SupportsPromptCacheKey {
			t.Fatalf("expected prompt cache key unsupported for %s, got %+v", providerID, caps)
		}
		if caps.SupportsNativeWebSearch {
			t.Fatalf("expected native web search unsupported for %s, got %+v", providerID, caps)
		}
		if caps.SupportsProviderVerbosity {
			t.Fatalf("expected provider verbosity unsupported for %s, got %+v", providerID, caps)
		}
	}
}

func TestProviderCapabilitiesFromOverrideCopiesProviderVerbosityValue(t *testing.T) {
	for _, supportsProviderVerbosity := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "disabled"}[supportsProviderVerbosity], func(t *testing.T) {
			caps, ok := ProviderCapabilitiesFromOverride(config.ProviderCapabilitiesOverride{
				ProviderID:                "custom-provider",
				SupportsProviderVerbosity: supportsProviderVerbosity,
			})
			if !ok {
				t.Fatal("expected provider override capabilities")
			}
			if caps.SupportsProviderVerbosity != supportsProviderVerbosity {
				t.Fatalf("provider verbosity = %v, want %v, caps=%+v", caps.SupportsProviderVerbosity, supportsProviderVerbosity, caps)
			}
		})
	}
}

func TestLockedProviderVerbosityPreservesExplicitFalseAndFallsBackOnlyWhenAbsent(t *testing.T) {
	explicitFalse := false
	locked := LockedProviderCapabilitiesFromContract(ProviderCapabilities{
		ProviderID:                "custom-provider",
		SupportsProviderVerbosity: explicitFalse,
	})
	if locked.SupportsProviderVerbosity == nil || *locked.SupportsProviderVerbosity {
		t.Fatalf("expected newly locked explicit false provider verbosity, got %+v", locked)
	}

	encoded, err := json.Marshal(locked)
	if err != nil {
		t.Fatalf("marshal locked provider capabilities: %v", err)
	}
	var restored session.LockedProviderCapabilities
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatalf("unmarshal locked provider capabilities: %v", err)
	}
	if restored.SupportsProviderVerbosity == nil || *restored.SupportsProviderVerbosity {
		t.Fatalf("expected round-tripped explicit false provider verbosity, got %+v", restored)
	}

	explicitCaps, ok := ProviderCapabilitiesFromLocked(&session.LockedContract{
		ProviderContract: session.LockedProviderCapabilities{
			ProviderID:                "openai",
			IsOpenAIFirstParty:        true,
			SupportsProviderVerbosity: &explicitFalse,
		},
	})
	if !ok {
		t.Fatal("expected explicit locked provider capabilities")
	}
	if explicitCaps.SupportsProviderVerbosity {
		t.Fatalf("expected explicit false provider verbosity to win, got %+v", explicitCaps)
	}

	legacyCaps, ok := ProviderCapabilitiesFromLocked(&session.LockedContract{
		ProviderContract: session.LockedProviderCapabilities{
			ProviderID:         "openai",
			IsOpenAIFirstParty: true,
		},
	})
	if !ok {
		t.Fatal("expected legacy locked provider capabilities")
	}
	if !legacyCaps.SupportsProviderVerbosity {
		t.Fatalf("expected absent legacy provider verbosity to fall back to first-party behavior, got %+v", legacyCaps)
	}
}

func TestProviderCapabilitiesFromLockedOrOverridePrefersSessionContract(t *testing.T) {
	lockedVerbosity := true
	caps, ok := ProviderCapabilitiesFromLockedOrOverride(
		&session.LockedContract{ProviderContract: session.LockedProviderCapabilities{
			ProviderID:                "locked-provider",
			SupportsResponsesAPI:      false,
			SupportsProviderVerbosity: &lockedVerbosity,
		}},
		config.ProviderCapabilitiesOverride{
			ProviderID:                "configured-provider",
			SupportsResponsesAPI:      true,
			SupportsProviderVerbosity: false,
		},
	)
	if !ok {
		t.Fatal("expected resolved provider capabilities")
	}
	if caps.ProviderID != "locked-provider" || caps.SupportsResponsesAPI || !caps.SupportsProviderVerbosity {
		t.Fatalf("resolved capabilities = %+v, want locked contract", caps)
	}
}

func TestRequiredToolChoiceSupportUsesResponsesAdapterContract(t *testing.T) {
	for _, providerID := range []string{"openai", "chatgpt-codex", "openai-compatible"} {
		t.Run(providerID, func(t *testing.T) {
			caps, ok := LookupProviderCapabilityContract(providerID)
			if !ok {
				t.Fatalf("missing provider contract %q", providerID)
			}
			if err := ValidateToolChoiceSupport(caps, ToolChoiceModeRequired); err != nil {
				t.Fatalf("ValidateToolChoiceSupport() error = %v", err)
			}
		})
	}
}

func TestRequiredToolChoiceSupportReturnsTypedErrorForNonResponsesAdapter(t *testing.T) {
	caps, ok := LookupProviderCapabilityContract("anthropic")
	if !ok {
		t.Fatal("missing anthropic provider contract")
	}
	err := ValidateToolChoiceSupport(caps, ToolChoiceModeRequired)
	if !errors.Is(err, ErrUnsupportedToolChoicePolicy) {
		t.Fatalf("ValidateToolChoiceSupport() error = %v, want ErrUnsupportedToolChoicePolicy", err)
	}
	var typedErr *UnsupportedToolChoicePolicyError
	if !errors.As(err, &typedErr) {
		t.Fatalf("ValidateToolChoiceSupport() error type = %T, want *UnsupportedToolChoicePolicyError", err)
	}
	if typedErr.ProviderID != "anthropic" || typedErr.Mode != ToolChoiceModeRequired {
		t.Fatalf("typed error = %+v", typedErr)
	}
}

func TestSupportsFastModeProvider(t *testing.T) {
	if !SupportsFastModeProvider(ProviderCapabilities{
		ProviderID: "openai-compatible", SupportsResponsesAPI: true, SupportsFastMode: true,
	}) {
		t.Fatal("expected an explicitly capable compatible provider to support fast mode")
	}
	if SupportsFastModeProvider(ProviderCapabilities{
		ProviderID: "openai", SupportsResponsesAPI: true, IsOpenAIFirstParty: true,
	}) {
		t.Fatal("first-party identity must not imply fast-mode support without the capability")
	}
	for _, providerID := range []string{"openai", "chatgpt-codex"} {
		caps, err := InferProviderCapabilities(providerID)
		if err != nil || !SupportsFastModeProvider(caps) {
			t.Fatalf("%s built-in contract should support fast mode: caps=%+v err=%v", providerID, caps, err)
		}
	}
	compatible, err := InferProviderCapabilities("openai-compatible")
	if err != nil || SupportsFastModeProvider(compatible) {
		t.Fatalf("compatible provider should default to no fast-mode support: caps=%+v err=%v", compatible, err)
	}
}

func TestSupportsPromptCacheKeyProvider(t *testing.T) {
	if !SupportsPromptCacheKeyProvider(ProviderCapabilities{ProviderID: "openai-compatible", SupportsResponsesAPI: true, SupportsPromptCacheKey: true}) {
		t.Fatal("expected explicit prompt cache capability to enable support")
	}
	if SupportsPromptCacheKeyProvider(ProviderCapabilities{ProviderID: "openai-compatible", SupportsResponsesAPI: true, SupportsPromptCacheKey: false}) {
		t.Fatal("did not expect prompt cache support without explicit capability")
	}
}

func TestProviderCapabilitiesFromLockedHandlesExplicitAndLegacyCapabilities(t *testing.T) {
	tests := []struct {
		name               string
		locked             session.LockedProviderCapabilities
		wantPromptCacheKey bool
	}{
		{
			name: "explicit prompt cache false is preserved",
			locked: session.LockedProviderCapabilities{
				ProviderID:                "openai",
				SupportsResponsesAPI:      true,
				SupportsPromptCacheKey:    false,
				HasSupportsPromptCacheKey: true,
			},
		},
		{
			name: "legacy prompt cache inherits openai support",
			locked: session.LockedProviderCapabilities{
				ProviderID:           "openai",
				SupportsResponsesAPI: true,
			},
			wantPromptCacheKey: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps, ok := ProviderCapabilitiesFromLocked(&session.LockedContract{ProviderContract: tt.locked})
			if !ok {
				t.Fatal("expected locked provider capabilities")
			}
			if caps.SupportsPromptCacheKey != tt.wantPromptCacheKey {
				t.Fatalf("prompt cache key = %v, want %v, caps=%+v", caps.SupportsPromptCacheKey, tt.wantPromptCacheKey, caps)
			}
		})
	}
}
