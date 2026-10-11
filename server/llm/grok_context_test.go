package llm

import (
	"testing"

	"core/shared/config"
)

func TestGrokDerivedContextUsesSelectedVariant(t *testing.T) {
	for _, tc := range []struct {
		protocol config.ConnectionProtocol
		model    string
		window   int
	}{
		{config.ConnectionGrokCLIProxy, "grok-4.7", 256_000},
		{config.ConnectionGrokOAuthAPI, "grok-4.7", 500_000},
		{config.ConnectionGrokCLIProxy, "grok-4.6", 100_000},
	} {
		t.Run(string(tc.protocol)+"/"+tc.model, func(t *testing.T) {
			id := config.ConnectionID("grok")
			settings := config.Settings{
				Connection:  &id,
				Connections: map[config.ConnectionID]config.ProviderConnection{id: {Protocol: tc.protocol}},
			}
			if err := ApplyDerivedModelContextBudget(&settings, tc.model, 100_000, 95_000); err != nil {
				t.Fatal(err)
			}
			if settings.ModelContextWindow != tc.window || settings.ContextCompactionThresholdTokens != tc.window*95/100 {
				t.Fatalf("budget = %d/%d", settings.ModelContextWindow, settings.ContextCompactionThresholdTokens)
			}
		})
	}
}
