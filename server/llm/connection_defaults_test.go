package llm

import (
	"testing"

	"core/shared/config"
)

func TestConnectionDefaultsDeriveOnlyUnresolvedContext(t *testing.T) {
	for _, test := range []struct {
		name      string
		origin    config.SourceKind
		window    int
		threshold int
	}{
		{"default subscription budget", config.SourceDefault, 500_000, 475_000},
		{"authored subscription budget", config.SourceFileKind, 400_000, 372_000},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := config.DefaultOnboardingSettings()
			settings.Connection = config.SingleConnection("subscription")
			settings.Connections = map[config.ConnectionID]config.ProviderConnection{
				"subscription": {Protocol: config.ConnectionChatGPT},
			}
			if test.origin == config.SourceFileKind {
				settings.ModelContextWindow = test.window
				settings.ContextCompactionThresholdTokens = test.threshold
			}
			sources := map[string]config.Origin{
				"model":                               {Kind: config.SourceDefault},
				"thinking_level":                      {Kind: config.SourceDefault},
				"model_context_window":                {Kind: test.origin},
				"context_compaction_threshold_tokens": {Kind: test.origin},
			}
			if err := ApplyConnectionModelDefaults(&settings, sources); err != nil {
				t.Fatal(err)
			}
			if settings.ModelContextWindow != test.window || settings.ContextCompactionThresholdTokens != test.threshold {
				t.Fatalf("budget = %d/%d, want %d/%d", settings.ModelContextWindow, settings.ContextCompactionThresholdTokens, test.window, test.threshold)
			}
			if settings.Model != "gpt-6.2-sol" || settings.ThinkingLevel != config.DefaultOnboardingSettings().ThinkingLevel {
				t.Fatalf("subscription changed model or thinking: %s/%s", settings.Model, settings.ThinkingLevel)
			}
		})
	}
}
