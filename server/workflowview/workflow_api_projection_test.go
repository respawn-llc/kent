package workflowview

import (
	"testing"

	"core/server/workflow"
)

func TestValidationErrorsIncludesStructuredDetails(t *testing.T) {
	errors := ValidationErrors("workflow-1", []workflow.ValidationError{{
		Code:           workflow.CodeInvalidTemplatePlaceholder,
		Message:        "prompt template references an unknown node input",
		NodeID:         "node-1",
		InputName:      "summary",
		Placeholder:    ".Inputs.summary",
		ProviderEdgeID: "edge-provider",
		FieldName:      "summary",
	}})

	if len(errors) != 1 || errors[0].Details == nil {
		t.Fatalf("errors = %+v, want structured details", errors)
	}
	details := errors[0].Details
	if details.FieldName != "summary" || details.InputName != "summary" || details.Placeholder != ".Inputs.summary" || details.ProviderEdgeID != "edge-provider" {
		t.Fatalf("details = %+v", details)
	}
}
