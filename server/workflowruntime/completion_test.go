package workflowruntime

import (
	"encoding/json"
	"strings"
	"testing"

	"builder/server/llm"
	"builder/server/workflow"
	"builder/shared/config"
)

type completionSchemaProperty struct {
	Type        any      `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
}

type completionSchemaBranch struct {
	AdditionalProperties bool                                `json:"additionalProperties"`
	Required             []string                            `json:"required"`
	Properties           map[string]completionSchemaProperty `json:"properties"`
}

func TestSelectCompletionMode(t *testing.T) {
	supported := llm.ProviderCapabilities{SupportsResponsesAPI: true}
	unsupported := llm.ProviderCapabilities{}
	tests := []struct {
		name    string
		mode    config.WorkflowCompletionMode
		caps    llm.ProviderCapabilities
		want    CompletionMode
		wantErr string
	}{
		{name: "auto structured", mode: config.WorkflowCompletionModeAuto, caps: supported, want: CompletionModeStructuredOutput},
		{name: "auto tool", mode: config.WorkflowCompletionModeAuto, caps: unsupported, want: CompletionModeTool},
		{name: "forced tool", mode: config.WorkflowCompletionModeTool, caps: supported, want: CompletionModeTool},
		{name: "forced structured unsupported", mode: config.WorkflowCompletionModeStructuredOutput, caps: unsupported, wantErr: "responses API"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SelectCompletionMode(tt.mode, tt.caps)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("SelectCompletionMode error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("SelectCompletionMode: %v", err)
			}
			if got != tt.want {
				t.Fatalf("mode = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompletionJSONSchemaIncludesTransitionSpecificParameterBranches(t *testing.T) {
	raw, err := CompletionJSONSchema(CompletionContract{
		Transitions: []CompletionTransition{
			{ID: "done", Parameters: []workflow.Parameter{{Key: "summary", Description: "Summary of work."}}},
			{ID: "blocked", Parameters: []workflow.Parameter{{Key: "risk", Description: "Risk note."}}},
		},
	})
	if err != nil {
		t.Fatalf("CompletionJSONSchema: %v", err)
	}
	var schema struct {
		AdditionalProperties bool                                `json:"additionalProperties"`
		Required             []string                            `json:"required"`
		Properties           map[string]completionSchemaProperty `json:"properties"`
		OneOf                []completionSchemaBranch            `json:"oneOf"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	if schema.AdditionalProperties {
		t.Fatal("schema allows additional properties")
	}
	if _, ok := schema.Properties["summary"]; !ok {
		t.Fatalf("schema properties missing summary: %+v", schema.Properties)
	}
	if got := schema.Properties["summary"].Description; got != "Summary of work." {
		t.Fatalf("summary description = %q", got)
	}
	if got := strings.Join(schema.Properties["transition"].Enum, ","); got != "blocked,done" {
		t.Fatalf("transition enum = %q, want blocked,done", got)
	}
	wantRequired := []string{"transition", "commentary"}
	if strings.Join(schema.Required, ",") != strings.Join(wantRequired, ",") {
		t.Fatalf("required = %+v, want %+v", schema.Required, wantRequired)
	}
	if len(schema.OneOf) != 2 {
		t.Fatalf("oneOf branch count = %d, want 2 in %s", len(schema.OneOf), string(raw))
	}
	branchesByTransition := map[string]completionSchemaBranch{}
	for _, branch := range schema.OneOf {
		if branch.AdditionalProperties {
			t.Fatalf("branch allows additional properties: %+v", branch)
		}
		transitionEnum := branch.Properties["transition"].Enum
		if len(transitionEnum) != 1 {
			t.Fatalf("branch transition enum = %+v, want one value", transitionEnum)
		}
		branchesByTransition[transitionEnum[0]] = branch
	}
	assertBranchParameters(t, branchesByTransition["done"], []string{"summary"}, []string{"risk"})
	assertBranchParameters(t, branchesByTransition["blocked"], []string{"risk"}, []string{"summary"})
}

func assertBranchParameters(t *testing.T, branch completionSchemaBranch, expected []string, forbidden []string) {
	t.Helper()
	if len(branch.Properties) == 0 {
		t.Fatalf("branch missing properties")
	}
	required := map[string]bool{}
	for _, field := range branch.Required {
		required[field] = true
	}
	for _, field := range expected {
		if _, ok := branch.Properties[field]; !ok {
			t.Fatalf("branch properties missing %s: %+v", field, branch.Properties)
		}
		if !required[field] {
			t.Fatalf("branch required missing %s: %+v", field, branch.Required)
		}
	}
	for _, field := range forbidden {
		if _, ok := branch.Properties[field]; ok {
			t.Fatalf("branch properties should not include %s: %+v", field, branch.Properties)
		}
		if required[field] {
			t.Fatalf("branch required should not include %s: %+v", field, branch.Required)
		}
	}
}

func TestCompletionJSONSchemaRequiresSingleTransitionParameters(t *testing.T) {
	raw, err := CompletionJSONSchema(CompletionContract{
		Transitions: []CompletionTransition{
			{ID: "done", Parameters: []workflow.Parameter{
				{Key: "summary", Description: "Summary of work."},
				{Key: "risk", Description: "Risk note."},
			}},
		},
	})
	if err != nil {
		t.Fatalf("CompletionJSONSchema: %v", err)
	}
	var schema struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	if schema.Properties["summary"].Type != "string" {
		t.Fatalf("summary type = %q, want string", schema.Properties["summary"].Type)
	}
	if _, ok := schema.Properties["transition"]; ok {
		t.Fatalf("single-transition schema should omit transition property: %+v", schema.Properties)
	}
	wantRequired := map[string]bool{"commentary": true, "summary": true, "risk": true}
	if len(schema.Required) != len(wantRequired) {
		t.Fatalf("required = %+v, want exactly commentary and parameters", schema.Required)
	}
	for _, field := range schema.Required {
		if !wantRequired[field] {
			t.Fatalf("unexpected required field %q in %+v", field, schema.Required)
		}
	}
}

func TestDecodeCompletionRejectsLegacyTransitionID(t *testing.T) {
	_, err := DecodeCompletion(json.RawMessage(`{"transition_id":"done","commentary":"done","summary":"done"}`), CompletionContract{
		Transitions: []CompletionTransition{{ID: "done", Parameters: []workflow.Parameter{{Key: "summary", Description: "Summary."}}}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	validation, ok := err.(ValidationError)
	if !ok {
		t.Fatalf("error type = %T, want ValidationError", err)
	}
	codes := map[string]bool{}
	for _, issue := range validation.Issues {
		codes[issue.Code] = true
	}
	if !codes["unknown_field"] {
		t.Fatalf("codes = %+v, want unknown_field", codes)
	}
}

func TestDecodeCompletionInfersSingleTransitionAndRequiresParameters(t *testing.T) {
	_, err := DecodeCompletion(json.RawMessage(`{"commentary":"done"}`), CompletionContract{
		Transitions: []CompletionTransition{{ID: "done", Parameters: []workflow.Parameter{
			{Key: "summary", Description: "Summary."},
			{Key: "risk", Description: "Risk."},
		}}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	validation, ok := err.(ValidationError)
	if !ok {
		t.Fatalf("error type = %T, want ValidationError", err)
	}
	missing := map[string]bool{}
	for _, issue := range validation.Issues {
		if issue.Code == "required_parameter_missing" {
			missing[issue.Field] = true
		}
	}
	for _, field := range []string{"risk", "summary"} {
		if !missing[field] {
			t.Fatalf("missing required parameter %q in issues %+v", field, validation.Issues)
		}
	}

	parsed, err := DecodeCompletion(json.RawMessage(`{"commentary":"","summary":"done","risk":"low"}`), CompletionContract{
		Transitions: []CompletionTransition{{ID: "done", Parameters: []workflow.Parameter{
			{Key: "summary", Description: "Summary."},
			{Key: "risk", Description: "Risk."},
		}}},
	})
	if err != nil {
		t.Fatalf("DecodeCompletion valid single transition: %v", err)
	}
	if parsed.TransitionID != "done" {
		t.Fatalf("transition = %q, want done", parsed.TransitionID)
	}
	if parsed.OutputValues["summary"] != "done" || parsed.OutputValues["risk"] != "low" {
		t.Fatalf("parameter values = %+v", parsed.OutputValues)
	}
}

func TestDecodeCompletionRequiresTransitionWhenAmbiguous(t *testing.T) {
	_, err := DecodeCompletion(json.RawMessage(`{"commentary":"done","summary":"done"}`), CompletionContract{
		Transitions: []CompletionTransition{
			{ID: "done", Parameters: []workflow.Parameter{{Key: "summary", Description: "Summary."}}},
			{ID: "blocked", Parameters: []workflow.Parameter{{Key: "risk", Description: "Risk."}}},
		},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	validation, ok := err.(ValidationError)
	if !ok {
		t.Fatalf("error type = %T, want ValidationError", err)
	}
	for _, issue := range validation.Issues {
		if issue.Code == "required_field_missing" && issue.Field == "transition" {
			return
		}
	}
	t.Fatalf("missing required transition issue: %+v", validation.Issues)
}

func TestDecodeCompletionRejectsUndeclaredTransition(t *testing.T) {
	_, err := DecodeCompletion(json.RawMessage(`{"transition":"unknown","commentary":"done","summary":"done"}`), CompletionContract{
		Transitions: []CompletionTransition{
			{ID: "done", Parameters: []workflow.Parameter{{Key: "summary", Description: "Summary."}}},
			{ID: "blocked"},
		},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	validation, ok := err.(ValidationError)
	if !ok {
		t.Fatalf("error type = %T, want ValidationError", err)
	}
	for _, issue := range validation.Issues {
		if issue.Code == "invalid_transition" && issue.Field == "transition" {
			return
		}
	}
	t.Fatalf("missing invalid_transition issue: %+v", validation.Issues)
}

func TestDecodeCompletionRejectsParameterFromUnselectedTransition(t *testing.T) {
	_, err := DecodeCompletion(json.RawMessage(`{"transition":"done","commentary":"done","summary":"done","risk":"low"}`), CompletionContract{
		Transitions: []CompletionTransition{
			{ID: "done", Parameters: []workflow.Parameter{{Key: "summary", Description: "Summary."}}},
			{ID: "blocked", Parameters: []workflow.Parameter{{Key: "risk", Description: "Risk."}}},
		},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	validation, ok := err.(ValidationError)
	if !ok {
		t.Fatalf("error type = %T, want ValidationError", err)
	}
	for _, issue := range validation.Issues {
		if issue.Code == "unexpected_parameter" && issue.Field == "risk" {
			return
		}
	}
	t.Fatalf("missing unexpected_parameter issue: %+v", validation.Issues)
}
