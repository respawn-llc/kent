package workflowview

import (
	"testing"

	"core/server/workflow"
	"core/shared/runtimeids"
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

func TestValidationErrorsProjectSessionReferenceCodesAndDetails(t *testing.T) {
	for _, test := range []struct {
		name string
		code workflow.ValidationErrorCode
	}{
		{name: "source cannot own Session", code: workflow.CodeSessionSourceCannotOwnSession},
		{name: "transition missing", code: workflow.CodeSessionTransitionMissing},
		{name: "transition not guaranteed", code: workflow.CodeSessionTransitionNotGuaranteed},
		{name: "transition ambiguous", code: workflow.CodeSessionTransitionAmbiguous},
	} {
		t.Run(test.name, func(t *testing.T) {
			workflowID := runtimeids.NewWorkflowID()
			nodeID := workflow.NodeID("review")
			edgeID := workflow.EdgeID("edge-review")
			projected := ValidationErrors(&workflowID, []workflow.ValidationError{
				{
					Code:        test.code,
					Message:     "required diagnostic",
					NodeID:      &nodeID,
					EdgeID:      &edgeID,
					Placeholder: ".Params.review.session_id",
				},
			})

			if len(projected) != 1 {
				t.Fatalf("projected validation errors = %+v, want one error", projected)
			}
			if projected[0].Code != string(test.code) || projected[0].Message != "required diagnostic" {
				t.Fatalf("projected validation error = %+v, want code %q and diagnostic", projected[0], test.code)
			}
			if projected[0].WorkflowID == nil || *projected[0].WorkflowID != workflowID ||
				projected[0].NodeID == nil || *projected[0].NodeID != string(nodeID) ||
				projected[0].EdgeID == nil || *projected[0].EdgeID != string(edgeID) ||
				projected[0].Details == nil || projected[0].Details.Placeholder != ".Params.review.session_id" {
				t.Fatalf("projected validation details = %+v, want graph and placeholder details", projected[0])
			}
		})
	}
}
