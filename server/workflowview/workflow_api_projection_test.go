package workflowview

import (
	"testing"

	"core/server/workflow"
	"core/shared/runtimeids"
	"core/shared/workflowcontract"
)

func TestValidationErrorsInheritOnlyAnExplicitOptionalWorkflowID(t *testing.T) {
	inheritedID := runtimeids.NewWorkflowID()
	explicitID := runtimeids.NewWorkflowID()
	projected := ValidationErrors(&inheritedID, []workflow.ValidationError{
		{Code: workflow.CodeMissingNodeID},
		{Code: workflow.CodeMissingEdgeID, WorkflowID: &explicitID},
	})

	if projected[0].WorkflowID == nil || *projected[0].WorkflowID != inheritedID {
		t.Fatalf("inherited workflow id = %v, want %q", projected[0].WorkflowID, inheritedID)
	}
	if projected[1].WorkflowID == nil || *projected[1].WorkflowID != explicitID {
		t.Fatalf("explicit workflow id = %v, want %q", projected[1].WorkflowID, explicitID)
	}
}

func TestValidationErrorsProjectTypedSessionReferenceReason(t *testing.T) {
	reason := workflowcontract.ValidationErrorReasonSessionSourceCannotOwnSession
	projected := ValidationErrors(nil, []workflow.ValidationError{
		{
			Code:   workflow.CodeInvalidTemplatePlaceholder,
			Reason: &reason,
		},
	})

	if len(projected) != 1 || projected[0].Details == nil || projected[0].Details.Reason == nil {
		t.Fatalf("projected Session reference reason = %+v, want typed reason", projected)
	}
	if *projected[0].Details.Reason != reason {
		t.Fatalf("projected Session reference reason = %q, want %q", *projected[0].Details.Reason, reason)
	}
}
