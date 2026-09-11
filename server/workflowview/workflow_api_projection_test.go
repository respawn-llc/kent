package workflowview

import (
	"testing"

	"core/server/workflow"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/workflowcontract"
)

func TestValidationErrorsInheritOnlyAnExplicitOptionalWorkflowID(t *testing.T) {
	inheritedID := runtimeids.NewWorkflowID()
	explicitID := runtimeids.NewWorkflowID()
	projected, err := ValidationErrors(&inheritedID, []workflow.ValidationError{
		{Code: workflow.CodeMissingNodeID},
		{Code: workflow.CodeMissingEdgeID, WorkflowID: &explicitID},
	})
	if err != nil {
		t.Fatalf("project validation errors: %v", err)
	}

	if projected[0].WorkflowID == nil || *projected[0].WorkflowID != inheritedID {
		t.Fatalf("inherited workflow id = %v, want %q", projected[0].WorkflowID, inheritedID)
	}
	if projected[1].WorkflowID == nil || *projected[1].WorkflowID != explicitID {
		t.Fatalf("explicit workflow id = %v, want %q", projected[1].WorkflowID, explicitID)
	}
}

func TestValidationErrorsProjectTypedSessionReferenceReason(t *testing.T) {
	for _, reason := range []workflowcontract.ValidationErrorReason{
		workflowcontract.ValidationErrorReasonSessionSourceCannotOwnSession,
		workflowcontract.ValidationErrorReasonSessionTransitionMissing,
		workflowcontract.ValidationErrorReasonSessionTransitionNotGuaranteed,
		workflowcontract.ValidationErrorReasonSessionTransitionAmbiguous,
	} {
		t.Run(string(reason), func(t *testing.T) {
			projected, err := ValidationErrors(nil, []workflow.ValidationError{
				{
					Code:   workflow.CodeInvalidTemplatePlaceholder,
					Reason: &reason,
				},
			})
			if err != nil {
				t.Fatalf("project validation errors: %v", err)
			}

			if len(projected) != 1 || projected[0].Details == nil || projected[0].Details.Reason == nil {
				t.Fatalf("projected Session reference reason = %+v, want typed reason", projected)
			}
			want, err := serverapi.WorkflowValidationErrorReasonFromDomain(reason)
			if err != nil {
				t.Fatalf("project validation reason: %v", err)
			}
			if *projected[0].Details.Reason != want {
				t.Fatalf("projected Session reference reason = %v, want %v", *projected[0].Details.Reason, want)
			}
		})
	}
}

func TestValidationErrorsReturnsUnsupportedReasonError(t *testing.T) {
	t.Setenv("KENT_INVARIANT_MODE", "diagnostic")
	reason := workflowcontract.ValidationErrorReason("unsupported")

	projected, err := ValidationErrors(nil, []workflow.ValidationError{{Reason: &reason}})
	if err == nil {
		t.Fatal("unsupported workflow validation reason projected without an error")
	}
	if projected != nil {
		t.Fatalf("projected validation errors = %+v, want no response on projection error", projected)
	}
}
