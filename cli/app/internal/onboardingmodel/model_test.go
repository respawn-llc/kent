package onboardingmodel

import (
	"testing"

	"builder/server/auth"
	"builder/shared/config"
)

func TestProviderCapabilitiesForSettingsPreservesOpenAIFirstPartyPolicy(t *testing.T) {
	caps, err := ProviderCapabilitiesForSettings(auth.State{Method: auth.Method{Type: auth.MethodOAuth}}, config.Settings{})
	if err != nil {
		t.Fatalf("ProviderCapabilitiesForSettings: %v", err)
	}
	if !caps.IsOpenAIFirstParty {
		t.Fatal("expected OAuth default settings to be OpenAI first-party")
	}
}

func TestModelCapabilityHelpersDelegateCatalogPolicy(t *testing.T) {
	if !SupportsReasoningEffortModel("gpt-5") {
		t.Fatal("expected gpt-5 to support reasoning effort")
	}
	if len(SupportedThinkingLevelsModel("gpt-5")) == 0 {
		t.Fatal("expected thinking levels")
	}
	if meta, ok := LookupModelMetadata("gpt-5.4"); !ok || meta.ContextWindowTokens == 0 {
		t.Fatalf("expected gpt-5.4 metadata, got %+v ok=%t", meta, ok)
	}
}
