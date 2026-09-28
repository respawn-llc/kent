package config

import "testing"

func TestGPT6OnboardingDefaults(t *testing.T) {
	settings := DefaultOnboardingSettings()
	if settings.Model != "gpt-6-sol" {
		t.Fatalf("onboarding model = %q", settings.Model)
	}
	if settings.ModelContextWindow != 272_000 || settings.ContextCompactionThresholdTokens != 258_400 {
		t.Fatalf("onboarding context budget = %d/%d", settings.ModelContextWindow, settings.ContextCompactionThresholdTokens)
	}
}
