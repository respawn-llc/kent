package workflowruntime

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"core/server/llm"
	"core/server/workflow"
	"core/shared/config"
)

type completionSchemaProperty struct {
	Type        any      `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
}

func TestSelectCompletionMode(t *testing.T) {
	supported := llm.ProviderCapabilities{SupportsResponsesAPI: true}
	unsupported := llm.ProviderCapabilities{}
	tests := []struct {
		name    string
		mode    config.WorkflowCompletionMode
		caps    llm.ProviderCapabilities
		want    CompletionMode
		wantErr error
	}{
		{name: "auto structured", mode: config.WorkflowCompletionModeAuto, caps: supported, want: CompletionModeStructuredOutput},
		{name: "auto tool", mode: config.WorkflowCompletionModeAuto, caps: unsupported, want: CompletionModeTool},
		{name: "forced tool", mode: config.WorkflowCompletionModeTool, caps: supported, want: CompletionModeTool},
		{name: "forced structured unsupported", mode: config.WorkflowCompletionModeStructuredOutput, caps: unsupported, wantErr: ErrStructuredOutputUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SelectCompletionMode(tt.mode, tt.caps)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("SelectCompletionMode error = %v, want %v", err, tt.wantErr)
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

func TestCompletionJSONSchemaUsesOpenAICompatibleNullableTransitionParameters(t *testing.T) {
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
		OneOf                []any                               `json:"oneOf"`
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
	if got := strings.Join(schema.Properties["transition"].Enum, ","); got != "blocked,done" {
		t.Fatalf("transition enum = %q, want blocked,done", got)
	}
	if len(schema.OneOf) != 0 {
		t.Fatalf("schema should not use oneOf: %s", string(raw))
	}
	assertNullableParameterProperty(t, schema.Properties["summary"])
	assertNullableParameterProperty(t, schema.Properties["risk"])
	wantRequired := []string{"transition", "commentary", "risk", "summary"}
	if strings.Join(schema.Required, ",") != strings.Join(wantRequired, ",") {
		t.Fatalf("required = %+v, want %+v", schema.Required, wantRequired)
	}
}

func assertNullableParameterProperty(t *testing.T, property completionSchemaProperty) {
	t.Helper()
	values, ok := property.Type.([]any)
	if !ok || len(values) != 2 {
		t.Fatalf("property type = %+v, want nullable string", property.Type)
	}
	if values[0] != "string" || values[1] != "null" {
		t.Fatalf("property type = %+v, want [string null]", values)
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

func TestDecodeCompletionAcceptsNullForUnselectedTransitionParameter(t *testing.T) {
	parsed, err := DecodeCompletion(json.RawMessage(`{"transition":"done","commentary":"done","summary":"done","risk":null}`), CompletionContract{
		Transitions: []CompletionTransition{
			{ID: "done", Parameters: []workflow.Parameter{{Key: "summary", Description: "Summary."}}},
			{ID: "blocked", Parameters: []workflow.Parameter{{Key: "risk", Description: "Risk."}}},
		},
	})
	if err != nil {
		t.Fatalf("DecodeCompletion: %v", err)
	}
	if parsed.OutputValues["summary"] != "done" {
		t.Fatalf("summary = %q", parsed.OutputValues["summary"])
	}
	if _, exists := parsed.OutputValues["risk"]; exists {
		t.Fatalf("risk should be omitted after null input: %+v", parsed.OutputValues)
	}
}
