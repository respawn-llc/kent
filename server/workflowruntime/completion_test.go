package workflowruntime

import (
	"encoding/json"
	"strings"
	"testing"

	"builder/server/llm"
	"builder/server/workflow"
	"builder/shared/config"
)

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

func TestCompletionJSONSchemaIncludesTopLevelOutputFields(t *testing.T) {
	raw, err := CompletionJSONSchema(CompletionContract{
		TransitionIDs: []string{"done", "blocked"},
		OutputFields: []workflow.OutputField{
			{Name: "summary", Description: "Summary of work."},
			{Name: "risk", Description: "Risk note."},
		},
	})
	if err != nil {
		t.Fatalf("CompletionJSONSchema: %v", err)
	}
	var schema struct {
		AdditionalProperties bool     `json:"additionalProperties"`
		Required             []string `json:"required"`
		Properties           map[string]struct {
			Type        string   `json:"type"`
			Description string   `json:"description"`
			Enum        []string `json:"enum,omitempty"`
		} `json:"properties"`
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
	if got := strings.Join(schema.Properties["transition_id"].Enum, ","); got != "blocked,done" {
		t.Fatalf("transition_id enum = %q, want blocked,done", got)
	}
	wantRequired := []string{"transition_id", "commentary", "risk", "summary"}
	if strings.Join(schema.Required, ",") != strings.Join(wantRequired, ",") {
		t.Fatalf("required = %+v, want %+v", schema.Required, wantRequired)
	}
}

func TestDecodeCompletionRejectsUnknownFields(t *testing.T) {
	_, err := DecodeCompletion(json.RawMessage(`{"transition_id":"done","commentary":"done","summary":"done","extra":"x"}`), CompletionContract{
		TransitionIDs: []string{"done"},
		OutputFields:  []workflow.OutputField{{Name: "summary", Description: "Summary."}},
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
	if !codes["unknown_output_field"] {
		t.Fatalf("codes = %+v, want unknown_output_field", codes)
	}
}

func TestDecodeCompletionRequiresProtocolAndOutputFields(t *testing.T) {
	_, err := DecodeCompletion(json.RawMessage(`{"transition_id":"done"}`), CompletionContract{
		OutputFields: []workflow.OutputField{
			{Name: "summary", Description: "Summary."},
			{Name: "risk", Description: "Risk."},
		},
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
		if issue.Code == "required_field_missing" {
			missing[issue.Field] = true
		}
	}
	for _, field := range []string{"commentary", "risk", "summary"} {
		if !missing[field] {
			t.Fatalf("missing required field %q in issues %+v", field, validation.Issues)
		}
	}

	parsed, err := DecodeCompletion(json.RawMessage(`{"transition_id":"done","commentary":"","summary":"","risk":""}`), CompletionContract{
		OutputFields: []workflow.OutputField{
			{Name: "summary", Description: "Summary."},
			{Name: "risk", Description: "Risk."},
		},
	})
	if err != nil {
		t.Fatalf("DecodeCompletion valid empty strings: %v", err)
	}
	if parsed.TransitionID != "done" {
		t.Fatalf("transition_id = %q, want done", parsed.TransitionID)
	}
}

func TestDecodeCompletionRejectsUndeclaredTransitionID(t *testing.T) {
	_, err := DecodeCompletion(json.RawMessage(`{"transition_id":"unknown","commentary":"done","summary":"done"}`), CompletionContract{
		TransitionIDs: []string{"done", "blocked"},
		OutputFields:  []workflow.OutputField{{Name: "summary", Description: "Summary."}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	validation, ok := err.(ValidationError)
	if !ok {
		t.Fatalf("error type = %T, want ValidationError", err)
	}
	for _, issue := range validation.Issues {
		if issue.Code == "invalid_transition_id" && issue.Field == "transition_id" {
			return
		}
	}
	t.Fatalf("missing invalid_transition_id issue: %+v", validation.Issues)
}
