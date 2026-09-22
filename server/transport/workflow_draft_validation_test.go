package transport

import (
	"testing"

	remoteclient "core/shared/client"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/runtimeids"
)

func TestWorkflowBinaryDraftValidationReportsMissingIdentities(t *testing.T) {
	app, server := newGatewayTestServer(t)
	defer server.Close()
	defer func() { _ = app.Close() }()
	remote, err := remoteclient.DialRemoteURL(t.Context(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = remote.Close() }()
	created, err := remote.CreateWorkflow(t.Context(), &pb.CreateRequest{Name: "Invalid Draft"})
	if err != nil {
		t.Fatal(err)
	}
	graph := &pb.GraphDraft{Nodes: []*pb.GraphDraftNode{{
		Key: "backlog", Kind: pb.NodeKind_WORKFLOW_NODE_KIND_START, DisplayName: "Backlog",
	}}}
	response, err := remote.ValidateWorkflowGraphDraft(t.Context(), &pb.GraphValidateDraftRequest{
		WorkflowId: created.Workflow.Id, Graph: graph,
		Modes: []pb.ValidationMode{pb.ValidationMode_WORKFLOW_VALIDATION_MODE_DRAFT},
	})
	if err != nil {
		t.Fatal(err)
	}
	requireWorkflowDraftDiagnostic(t, response, pb.ValidationErrorCode_VALIDATION_ERROR_CODE_MISSING_NODE_ID)
	if len(response.DerivedWiring.Nodes) != 1 || response.DerivedWiring.Nodes[0].NodeId != "" {
		t.Fatalf("invalid authored node identity was changed: %v", response.DerivedWiring)
	}
	graph.Nodes[0].Id = runtimeids.NewGraphEntityID()
	graph.TransitionGroups = []*pb.GraphDraftTransitionGroup{{
		SourceNodeId: graph.Nodes[0].Id, TransitionId: "next", DisplayName: "Next",
	}}
	response, err = remote.ValidateWorkflowGraphDraft(t.Context(), &pb.GraphValidateDraftRequest{
		WorkflowId: created.Workflow.Id, Graph: graph,
		Modes: []pb.ValidationMode{pb.ValidationMode_WORKFLOW_VALIDATION_MODE_DRAFT},
	})
	if err != nil {
		t.Fatal(err)
	}
	requireWorkflowDraftDiagnostic(t, response, pb.ValidationErrorCode_VALIDATION_ERROR_CODE_MISSING_TRANSITION_GROUP_ID)
	if len(response.DerivedWiring.TransitionGroups) != 1 || response.DerivedWiring.TransitionGroups[0].TransitionGroupId != "" {
		t.Fatalf("invalid authored transition identity was changed: %v", response.DerivedWiring)
	}
	graph.TransitionGroups[0].Id = runtimeids.NewGraphEntityID()
	graph.Edges = []*pb.GraphDraftEdge{{
		TransitionGroupId: graph.TransitionGroups[0].Id, Key: "next", TargetNodeId: graph.Nodes[0].Id,
		AssigneeSelection: pb.AssigneeSelection_WORKFLOW_ASSIGNEE_SELECTION_CONFIGURED,
		ThinkingSelection: pb.ThinkingSelection_WORKFLOW_THINKING_SELECTION_CONFIGURED,
		ContextMode:       pb.ContextMode_WORKFLOW_CONTEXT_MODE_NEW_SESSION,
		ContextSource:     &pb.ContextSource{Kind: pb.ContextSourceKind_WORKFLOW_CONTEXT_SOURCE_KIND_IMMEDIATE_SOURCE},
	}}
	response, err = remote.ValidateWorkflowGraphDraft(t.Context(), &pb.GraphValidateDraftRequest{
		WorkflowId: created.Workflow.Id, Graph: graph,
		Modes: []pb.ValidationMode{pb.ValidationMode_WORKFLOW_VALIDATION_MODE_DRAFT},
	})
	if err != nil {
		t.Fatal(err)
	}
	requireWorkflowDraftDiagnostic(t, response, pb.ValidationErrorCode_VALIDATION_ERROR_CODE_MISSING_EDGE_ID)
	if len(response.DerivedWiring.Edges) != 1 || response.DerivedWiring.Edges[0].EdgeId != "" {
		t.Fatalf("invalid authored edge identity was changed: %v", response.DerivedWiring)
	}
}

func requireWorkflowDraftDiagnostic(t *testing.T, response *pb.GraphValidateDraftSuccess, code pb.ValidationErrorCode) {
	t.Helper()
	for _, result := range response.Results {
		for _, diagnostic := range result.Result.Errors {
			if diagnostic.Code == code {
				return
			}
		}
	}
	t.Fatalf("missing semantic diagnostic %v: %v", code, response)
}
