package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"core/shared/config"
	"core/shared/textutil"
)

func TestNativeThinkingSupport(t *testing.T) {
	for _, connection := range []struct {
		name       string
		definition config.ProviderConnection
		native     bool
	}{
		{name: "first-party", definition: config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL)}, native: true},
		{name: "subscription", definition: config.ProviderConnection{Protocol: config.ConnectionChatGPT}, native: true},
		{name: "custom", definition: config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value("https://proxy.example/v1")}},
	} {
		for _, override := range []bool{false, true} {
			definition := connection.definition
			if override {
				definition.Capabilities = config.ProviderCapabilitiesOverride{
					ProviderID: "openai", SupportsResponsesAPI: true, IsOpenAIFirstParty: true,
				}
			}
			prepared, err := ResolveConnectionCapabilities(definition)
			if err != nil {
				t.Fatal(err)
			}
			transport := NewHTTPTransport(missingAuth{})
			transport.ProviderCapabilitiesOverride = &prepared
			caps, err := transport.ProviderCapabilities(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, model := range []string{"gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol", "gpt-6-luna", "gpt-5.6-sol", "unknown"} {
				want := connection.native && (model == "gpt-6-astra" || model == "gpt-6.1-sol" || model == "gpt-6-sol" || model == "gpt-6-luna")
				if got := SupportsNativeThinkingUpdates(model, caps); got != want {
					t.Fatalf("connection=%s override=%v model=%s: support=%v, want %v", connection.name, override, model, got, want)
				}
			}
			if caps != prepared {
				t.Fatalf("prepared connection contract changed: %+v, want %+v", caps, prepared)
			}
			if override && (caps.ProviderID != "openai" || !caps.IsOpenAIFirstParty) {
				t.Fatalf("general override changed: %+v", caps)
			}
		}
	}
}

func TestConfigurationUpdatePreparedHTTPInput(t *testing.T) {
	prior := ItemsFromMessages([]Message{{Role: RoleUser, Content: textutil.Value("first")}})
	items := PrepareOpenAIInputItems(append(CloneResponseItems(prior), ResponseItem{
		Type: ResponseItemTypeConfigurationUpdate, ConfigurationEffort: textutil.Value("low"),
	}))
	input, err := buildResponsesInput(items)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var wire []json.RawMessage
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire) != 2 || !bytes.Equal(items[0].Raw, prior[0].Raw) {
		t.Fatal("prepared preceding input changed")
	}
	var update struct {
		Type      string `json:"type"`
		Reasoning struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
	}
	if err := json.Unmarshal(wire[1], &update); err != nil {
		t.Fatal(err)
	}
	if update.Type != "configuration_update" || update.Reasoning.Effort != "low" {
		t.Fatalf("update = %+v", update)
	}
}
