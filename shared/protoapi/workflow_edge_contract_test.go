package protoapi

import (
	"testing"

	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/runtimeids"
	"google.golang.org/protobuf/proto"
)

func TestWorkflowGraphRequestsRejectInvalidSelectionsAndPurposes(t *testing.T) {
	workflowID := runtimeids.NewWorkflowID().String()
	graph := &pb.GraphDraft{Edges: []*pb.GraphDraftEdge{{
		Id: "edge", TransitionGroupId: "group", Key: "edge", TargetNodeId: "target",
		AssigneeSelection: pb.AssigneeSelection_WORKFLOW_ASSIGNEE_SELECTION_CONFIGURED,
		ThinkingSelection: pb.ThinkingSelection_WORKFLOW_THINKING_SELECTION_CONFIGURED,
		ContextMode:       pb.ContextMode_WORKFLOW_CONTEXT_MODE_NEW_SESSION,
		ContextSource:     &pb.ContextSource{Kind: pb.ContextSourceKind_WORKFLOW_CONTEXT_SOURCE_KIND_IMMEDIATE_SOURCE},
		Parameters:        []*pb.Parameter{{Key: "summary", Description: "Summary", Purpose: pb.ParameterPurpose_WORKFLOW_PARAMETER_PURPOSE_ORDINARY}},
	}}}
	requests := []func(*pb.GraphDraft) proto.Message{
		func(graph *pb.GraphDraft) proto.Message {
			return &pb.GraphValidateDraftRequest{WorkflowId: workflowID, Graph: graph, Modes: []pb.ValidationMode{pb.ValidationMode_WORKFLOW_VALIDATION_MODE_DRAFT}}
		},
		func(graph *pb.GraphDraft) proto.Message {
			return &pb.GraphDeriveWiringRequest{WorkflowId: workflowID, Graph: graph}
		},
		func(graph *pb.GraphDraft) proto.Message {
			return &pb.GraphSavePreviewRequest{WorkflowId: workflowID, Graph: graph}
		},
		func(graph *pb.GraphDraft) proto.Message {
			return &pb.GraphSaveRequest{WorkflowId: workflowID, Graph: graph}
		},
	}
	for _, request := range requests {
		if err := Validate(request(graph)); err != nil {
			t.Fatalf("valid draft: %v", err)
		}
		for _, mutate := range []func(*pb.GraphDraftEdge){
			func(edge *pb.GraphDraftEdge) {
				edge.AssigneeSelection = pb.AssigneeSelection_WORKFLOW_ASSIGNEE_SELECTION_UNSPECIFIED
			},
			func(edge *pb.GraphDraftEdge) { edge.AssigneeSelection = 999 },
			func(edge *pb.GraphDraftEdge) {
				edge.ThinkingSelection = pb.ThinkingSelection_WORKFLOW_THINKING_SELECTION_UNSPECIFIED
			},
			func(edge *pb.GraphDraftEdge) { edge.ThinkingSelection = 999 },
			func(edge *pb.GraphDraftEdge) {
				edge.Parameters[0].Purpose = pb.ParameterPurpose_WORKFLOW_PARAMETER_PURPOSE_UNSPECIFIED
			},
			func(edge *pb.GraphDraftEdge) { edge.Parameters[0].Purpose = 999 },
		} {
			invalid := proto.CloneOf(graph)
			mutate(invalid.Edges[0])
			if err := Validate(request(invalid)); err == nil {
				t.Fatalf("invalid selection or purpose accepted: %v", invalid)
			}
		}
	}
	encoded, err := proto.Marshal(graph)
	if err != nil {
		t.Fatal(err)
	}
	decoded := &pb.GraphDraft{}
	if err := proto.Unmarshal(encoded, decoded); err != nil || !proto.Equal(graph, decoded) {
		t.Fatalf("draft round trip = %v, %v", decoded, err)
	}
}
