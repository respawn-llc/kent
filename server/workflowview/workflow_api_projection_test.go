package workflowview

import (
	"testing"

	"core/server/workflow"
	"core/shared/protoapi"
	"core/shared/runtimeids"
)

func TestValidationErrorsInheritOnlyAnExplicitOptionalWorkflowID(t *testing.T) {
	inheritedID := runtimeids.NewWorkflowID()
	explicitID := runtimeids.NewWorkflowID()
	projected, err := ValidationErrors(&inheritedID, []workflow.ValidationError{
		{Code: workflow.CodeMissingNodeID},
		{Code: workflow.CodeMissingEdgeID, WorkflowID: &explicitID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if projected[0].GetWorkflowId() != inheritedID.String() {
		t.Fatalf("inherited workflow id = %v, want %q", projected[0].WorkflowId, inheritedID)
	}
	if projected[1].GetWorkflowId() != explicitID.String() {
		t.Fatalf("explicit workflow id = %v, want %q", projected[1].WorkflowId, explicitID)
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
			projected, err := ValidationErrors(&workflowID, []workflow.ValidationError{
				{
					Code:        test.code,
					Message:     "required diagnostic",
					NodeID:      &nodeID,
					EdgeID:      &edgeID,
					Placeholder: ".Params.review.session_id",
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(projected) != 1 {
				t.Fatalf("projected validation errors = %+v, want one error", projected)
			}
			code, err := protoapi.WorkflowValidationErrorCode.Decode(projected[0].Code)
			if err != nil {
				t.Fatal(err)
			}
			if code != string(test.code) || projected[0].Message != "required diagnostic" {
				t.Fatalf("projected validation error = %+v, want code %q and diagnostic", projected[0], test.code)
			}
			if projected[0].GetWorkflowId() != workflowID.String() ||
				projected[0].GetNodeId() != string(nodeID) ||
				projected[0].GetEdgeId() != string(edgeID) ||
				projected[0].Details == nil || projected[0].Details.Placeholder != ".Params.review.session_id" {
				t.Fatalf("projected validation details = %+v, want graph and placeholder details", projected[0])
			}
		})
	}
}
