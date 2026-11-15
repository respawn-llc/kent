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

func TestReferenceEditStepsAreSelectedByID(t *testing.T) {
	deepseek, err := newConnectionForm(&authpb.ConnectionCatalog{})
	if err != nil {
		t.Fatal(err)
	}
	deepseek.selectTemplate(connectionTemplateDeepSeekAPI)
	deepseekSteps, err := referenceEditSteps(deepseek)
	if err != nil {
		t.Fatal(err)
	}
	if ids := stepIDs(deepseekSteps); len(ids) != 2 || ids[0] != connectionStepEndpoint || ids[1] != connectionStepEnvironment {
		t.Fatalf("DeepSeek edit steps = %v", ids)
	}
	endpoint := "https://api.openai.com/v1"
	env := "OPENAI_API_KEY"
	openai := &connectionForm{
		template:   connectionTemplateOpenAIAPI,
		definition: config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: &endpoint, EnvironmentVariable: &env},
	}
	openaiSteps, err := referenceEditSteps(openai)
	if err != nil {
		t.Fatal(err)
	}
	if ids := stepIDs(openaiSteps); len(ids) != 1 || ids[0] != connectionStepEnvironment {
		t.Fatalf("OpenAI edit steps = %v", ids)
	}
}

func stepIDs(steps []onboardingStepDefinition) []onboardingStepID {
	ids := make([]onboardingStepID, len(steps))
	for i, step := range steps {
		ids[i] = step.id
	}
	return ids
}
