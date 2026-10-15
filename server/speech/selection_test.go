package speech

import (
	"testing"

	"core/server/launch"
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
	rotation := new(launch.ConnectionRotation)
	for _, want := range []config.ConnectionID{"second", "first", "second"} {
		invocation, err := selection.Allocate(rotation)
		if err != nil || invocation.ID != want {
			t.Fatalf("allocation = %v, %v; want %v", invocation, err, want)
		}
	}
}

func TestAvailabilityDoesNotAllocateAndSharesLaunchRotation(t *testing.T) {
	defaults := config.ConnectionSelection{"a", "b"}
	settings := config.Settings{
		Connection: &defaults, ConnectionOrder: []config.ConnectionID{"b", "a"},
		Connections: map[config.ConnectionID]config.ProviderConnection{
			"a": {Protocol: config.ConnectionChatGPT},
			"b": {Protocol: config.ConnectionChatGPT},
		},
	}
	rotation := new(launch.ConnectionRotation)
	for range 3 {
		selection, err := EligibleDefaults(settings)
		if err != nil || selection.Limits == nil || selection.Limits.MaxBytes != 25_000_000 {
			t.Fatalf("availability = %v, %v", selection.Limits, err)
		}
	}
	first, err := rotation.Prepare(settings, true)
	if err != nil || first != "b" {
		t.Fatalf("launch = %v, %v", first, err)
	}
	selection, err := EligibleDefaults(settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []config.ConnectionID{"a", "b"} {
		invocation, err := selection.Allocate(rotation)
		if err != nil || invocation.ID != want {
			t.Fatalf("invocation/retry = %v, %v; want %v", invocation, err, want)
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
