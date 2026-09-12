package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"core/shared/textutil"
)

func TestNativeThinkingSupport(t *testing.T) {
	for _, oauth := range []bool{false, true} {
		for _, custom := range []bool{false, true} {
			transport := NewHTTPTransport(staticAuth{})
			if oauth {
				transport = NewHTTPTransport(oauthStaticAuth{})
			}
			if custom {
				transport.BaseURL = "https://proxy.example/v1"
				transport.BaseURLExplicit = true
			}
			transport.ProviderCapabilitiesOverride = &ProviderCapabilities{
				ProviderID: "openai", SupportsResponsesAPI: true, IsOpenAIFirstParty: true,
			}
			caps, err := transport.ProviderCapabilities(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, model := range []string{"gpt-6-astra", "gpt-5", "unknown"} {
				want := !custom && model == "gpt-6-astra"
				if got := SupportsNativeThinkingUpdates(model, caps); got != want {
					t.Fatalf("oauth=%v custom=%v model=%s: support=%v, want %v", oauth, custom, model, got, want)
				}
			}
			if caps.ProviderID != "openai" || !caps.IsOpenAIFirstParty {
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
