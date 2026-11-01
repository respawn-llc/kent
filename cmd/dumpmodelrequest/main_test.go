package main

import (
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/sessionstore"
	"core/shared/config"
)

func TestInspectionResolvesNativeSupportOutsideLockedContract(t *testing.T) {
	for _, endpoint := range []string{"", "https://proxy.example/v1"} {
		for _, override := range []string{"", "openai"} {
			settings := testsetup.ProviderSettings(config.Settings{Model: "gpt-6-astra"})
			if endpoint != "" {
				id, err := settings.Connection.ConcreteID()
				if err != nil {
					t.Fatal(err)
				}
				definition := settings.Connections[*id]
				definition.Endpoint = &endpoint
				settings.Connections[*id] = definition
			}
			caps, _, err := resolveInspectionProviderCapabilities(settings, &session.LockedContract{
				Model:            "gpt-6-astra",
				ProviderContract: session.LockedProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true},
			}, override)
			if err != nil {
				t.Fatal(err)
			}
			if caps.SupportsNativeThinkingUpdates != (endpoint == "") {
				t.Fatalf("endpoint=%q native=%v", endpoint, caps.SupportsNativeThinkingUpdates)
			}
		}
	}
}
