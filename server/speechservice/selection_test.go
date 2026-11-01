package speech

import (
	"testing"

	"core/shared/config"
)

func TestEligibleDefaults(t *testing.T) {
	defaults := config.ConnectionSelection{"first", "unsupported", "second"}
	settings := config.Settings{
		Connection:      &defaults,
		ConnectionOrder: []config.ConnectionID{"second", "unsupported", "first", "nondefault"},
		Connections: map[config.ConnectionID]config.ProviderConnection{
			"first":       {Protocol: config.ConnectionChatGPT},
			"second":      {Protocol: config.ConnectionGrokOAuthAPI},
			"unsupported": {Protocol: config.ConnectionGrokCLIProxy},
			"nondefault":  {Protocol: config.ConnectionChatGPT},
		},
	}
	selection, err := EligibleDefaults(settings)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Limits == nil || selection.Limits.MaxBytes != 25_000_000 {
		t.Fatalf("eligible default limits = %v", selection.Limits)
	}
}

func TestRepeatedAvailability(t *testing.T) {
	defaults := config.ConnectionSelection{"a", "b"}
	settings := config.Settings{
		Connection: &defaults, ConnectionOrder: []config.ConnectionID{"b", "a"},
		Connections: map[config.ConnectionID]config.ProviderConnection{
			"a": {Protocol: config.ConnectionChatGPT},
			"b": {Protocol: config.ConnectionChatGPT},
		},
	}
	for range 3 {
		selection, err := EligibleDefaults(settings)
		if err != nil || selection.Limits == nil || selection.Limits.MaxBytes != 25_000_000 {
			t.Fatalf("availability = %v, %v", selection.Limits, err)
		}
	}
}

func TestUnavailableDefaults(t *testing.T) {
	for _, settings := range []config.Settings{
		{},
		{
			Connection: config.SingleConnection("unsupported"),
			Connections: map[config.ConnectionID]config.ProviderConnection{
				"unsupported": {Protocol: config.ConnectionGrokCLIProxy},
				"nondefault":  {Protocol: config.ConnectionChatGPT},
			},
		},
	} {
		selection, err := EligibleDefaults(settings)
		if err != nil || selection.Limits != nil {
			t.Fatalf("unavailable = %v, %v", selection.Limits, err)
		}
	}
}
