package app

import (
	"testing"

	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
)

func TestDeepSeekSetupPrefillsOfficialEndpoint(t *testing.T) {
	form, err := newConnectionForm(&authpb.ConnectionCatalog{})
	if err != nil {
		t.Fatal(err)
	}
	form.selectTemplate(connectionTemplateDeepSeekAPI)
	if form.definition.Protocol != config.ConnectionDeepSeekAPIKey || form.id != "deepseek-1" || form.definition.Endpoint != nil {
		t.Fatalf("template = %s %+v", form.id, form.definition)
	}
	var endpointStep onboardingStepDefinition
	for _, step := range form.steps() {
		if step.id == connectionStepEndpoint {
			endpointStep = step
		}
	}
	if endpointStep.build == nil || !endpointStep.visible(nil) {
		t.Fatal("DeepSeek setup hides the endpoint")
	}
	screen := endpointStep.build(nil)
	if screen.InputValue != config.DefaultDeepSeekResponsesEndpoint {
		t.Fatalf("endpoint field = %q", screen.InputValue)
	}
	if err := endpointStep.apply(&onboardingFlowState{}, config.DefaultDeepSeekResponsesEndpoint); err != nil {
		t.Fatal(err)
	}
	if form.definition.Endpoint != nil {
		t.Fatalf("official endpoint was stored: %v", *form.definition.Endpoint)
	}
	if err := endpointStep.apply(&onboardingFlowState{}, "https://models.example.test/v1/"); err != nil {
		t.Fatal(err)
	}
	if form.definition.Endpoint == nil || *form.definition.Endpoint != "https://models.example.test/v1" {
		t.Fatalf("custom endpoint = %v", form.definition.Endpoint)
	}
}
