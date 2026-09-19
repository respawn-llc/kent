package workflowsvc

import (
	"context"
	"slices"
	"testing"

	"core/internal/testharness/workflowfixture"
	"core/server/workflow"
	"core/server/workflowstore"
	protoapi "core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	proto "google.golang.org/protobuf/proto"
)

func TestServiceWorkflowGraphSaveAtomicallyRepairsSavedInvalidWorkflow(t *testing.T) {
	ctx, service, _ := newWorkflowServiceTestContext(t)
	created, err := service.CreateWorkflow(ctx, &pb.CreateRequest{Name: "Invalid Workflow"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	before := getWorkflowGraphAtomicDefinition(t, ctx, service, workflowServiceID(t, created.Workflow.Id))
	startID := workflowServiceNodeIDByKind(t, before, "start")
	terminalID := workflowServiceNodeIDByKind(t, before, "terminal")
	agentID := workflowServiceGraphEntityID("node-agent-" + workflowServiceID(t, created.Workflow.Id).String())
	graph := protoapi.WorkflowGraphDraftFromDefinition(before)
	graph.Nodes = append(graph.Nodes, &pb.GraphDraftNode{
		Id: agentID, Key: "agent", Kind: pb.NodeKind_WORKFLOW_NODE_KIND_AGENT, DisplayName: "Agent", SubagentRole: proto.String("coder"),
	})
	graph.TransitionGroups = append(graph.TransitionGroups, &pb.GraphDraftTransitionGroup{Id: workflowServiceGraphEntityID("group-start"), SourceNodeId: startID, TransitionId: "start", DisplayName: "Start"}, &pb.GraphDraftTransitionGroup{Id: workflowServiceGraphEntityID("group-done"), SourceNodeId: agentID, TransitionId: "done", DisplayName: "Done"})
	graph.Edges = append(graph.Edges,
		workflowGraphAtomicEdge(workflowServiceGraphEntityID("edge-start"), workflowServiceGraphEntityID("group-start"), "start", agentID, "Do work."),
		workflowGraphAtomicEdge(workflowServiceGraphEntityID("edge-done"), workflowServiceGraphEntityID("group-done"), "done", terminalID, ""),
	)
	assertWorkflowGraphAtomicChangedSave(t, ctx, service, before, graph)
}

func TestServiceWorkflowGraphSaveAtomicallyDeletesFanOutTransitionBranch(t *testing.T) {
	ctx, service, workflowID := newWorkflowGraphAtomicFanOutFixture(t)
	before := getWorkflowGraphAtomicDefinition(t, ctx, service, workflowID)
	graph := protoapi.WorkflowGraphDraftFromDefinition(before)
	graph.Edges = slices.DeleteFunc(graph.Edges, func(edge *pb.GraphDraftEdge) bool {
		return edge.Id == workflowServiceGraphEntityID("edge-split-b-"+workflowID.String())
	})
	assertWorkflowGraphAtomicChangedSave(t, ctx, service, before, graph)
}

func TestServiceWorkflowGraphSaveAtomicallyDeletesNodeAndTransitionGroup(t *testing.T) {
	ctx, service, _ := newWorkflowServiceTestContext(t)
	workflowID := createWorkflowServiceChainedWorkflow(t, ctx, service)
	before := getWorkflowGraphAtomicDefinition(t, ctx, service, workflowID)
	graph := workflowGraphDraftWithoutNode(
		before,
		workflowServiceNodeIDByKey(t, before, "implement"),
		workflowServiceNodeIDByKind(t, before, "terminal"),
	)
	assertWorkflowGraphAtomicChangedSave(t, ctx, service, before, graph)
}

func TestServiceWorkflowGraphSaveAtomicallyChangesFanOutSource(t *testing.T) {
	ctx, service, workflowID := newWorkflowGraphAtomicFanOutFixture(t)
	before := getWorkflowGraphAtomicDefinition(t, ctx, service, workflowID)
	graph := protoapi.WorkflowGraphDraftFromDefinition(before)
	for index := range graph.TransitionGroups {
		if graph.TransitionGroups[index].Id == workflowServiceGraphEntityID("group-split-"+workflowID.String()) {
			graph.TransitionGroups[index].SourceNodeId = workflowServiceGraphEntityID("node-prep-" + workflowID.String())
		}
	}
	assertWorkflowGraphAtomicChangedSave(t, ctx, service, before, graph)
}

func TestServiceWorkflowGraphSaveRejectsInvalidAndStaleWithoutMutation(t *testing.T) {
	ctx, service, _ := newWorkflowServiceTestContext(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	before := getWorkflowGraphAtomicDefinition(t, ctx, service, workflowID)
	invalid := protoapi.WorkflowGraphDraftFromDefinition(before)
	invalid.Nodes = append(invalid.Nodes, invalid.Nodes[0])
	rejected, err := service.SaveWorkflowGraph(ctx, &pb.GraphSaveRequest{
		WorkflowId: workflowID.String(), ExpectedVersion: before.Workflow.Version, Graph: invalid,
	})
	if err != nil || rejected.Saved || !workflowGraphSaveResponseHasBlocker(rejected, "validation_failed") {
		t.Fatalf("invalid save = %+v, err = %v", rejected, err)
	}
	assertWorkflowGraphAtomicUnchanged(t, ctx, service, before)

	changed := protoapi.WorkflowGraphDraftFromDefinition(before)
	changed.Nodes[0].DisplayName += " edited"
	assertWorkflowGraphAtomicChangedSave(t, ctx, service, before, changed)
	current := getWorkflowGraphAtomicDefinition(t, ctx, service, workflowID)
	rejected, err = service.SaveWorkflowGraph(ctx, &pb.GraphSaveRequest{
		WorkflowId: workflowID.String(), ExpectedVersion: before.Workflow.Version, Graph: protoapi.WorkflowGraphDraftFromDefinition(before),
	})
	if err != nil || rejected.Saved || !workflowGraphSaveResponseHasBlocker(rejected, "version_changed") {
		t.Fatalf("stale save = %+v, err = %v", rejected, err)
	}
	assertWorkflowGraphAtomicUnchanged(t, ctx, service, current)
}

func TestServiceWorkflowGraphSaveCurrentNodeDeletionIsBlocked(t *testing.T) {
	ctx, service, binding := newWorkflowServiceTestContext(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	task := createWorkflowServiceTask(t, ctx, service, serverapi.WorkflowTaskCreateRequest{
		ProjectID: binding.ProjectID, WorkflowID: &workflowID, Title: "Active graph reference", LabelIDs: []string{},
	})
	started := startWorkflowServiceTask(t, ctx, service, task.Task.ID)
	before := getWorkflowGraphAtomicDefinition(t, ctx, service, workflowID)
	removedNodeID := started.CurrentNodes[0].NodeID
	graph := workflowGraphDraftWithoutNode(before, removedNodeID, workflowServiceNodeIDByKind(t, before, "terminal"))

	preview := previewWorkflowGraphAtomicDraft(t, ctx, service, before, graph)
	nodeReference := &pb.GraphEntityReference{EntityType: pb.GraphEntityType_WORKFLOW_GRAPH_ENTITY_TYPE_NODE, EntityId: removedNodeID}
	if preview.Impact.ActiveCurrentNodeCount != 1 || !slices.ContainsFunc(preview.Impact.RemovedEntities, func(value *pb.GraphEntityReference) bool { return proto.Equal(value, nodeReference) }) ||
		!slices.EqualFunc(workflowServiceGraphSaveBlockerEntities(preview.Blockers, "node_task_references"), []*pb.GraphEntityReference{nodeReference}, workflowGraphReferenceEqual) {
		t.Fatalf("active Current Node preview = %+v", preview)
	}
	blocked := saveWorkflowGraphAtomicPreview(t, ctx, service, before, graph, preview)
	if blocked.Saved {
		t.Fatalf("active Current Node save = %+v, want blocked", blocked)
	}
	assertWorkflowGraphAtomicUnchanged(t, ctx, service, before)
}

func TestServiceWorkflowGraphSavePendingApprovalDeletionIsBlocked(t *testing.T) {
	ctx, service, binding, metadataStore := newWorkflowServiceTestContextWithMetadata(t)
	workflowID := createWorkflowServiceChainedWorkflow(t, ctx, service)
	requireWorkflowServiceEdgeApproval(t, ctx, service, workflowID, "next")
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	task := createWorkflowServiceTask(t, ctx, service, serverapi.WorkflowTaskCreateRequest{
		ProjectID: binding.ProjectID, WorkflowID: &workflowID, Title: "Pending Approval reference", LabelIDs: []string{},
	})
	started := startWorkflowServiceTask(t, ctx, service, task.Task.ID)
	source := workflowServiceCurrentNodeReference(t, workflow.TaskID(task.Task.ID), started.CurrentNodes[0])
	completed, err := workflowfixture.CompleteCurrentNode(t, ctx, metadataStore, service.store, workflowstore.CurrentNodeCompletionRequest{
		Source: source, TransitionID: "next", OutputValues: map[string]string{"prior_summary": "approved"},
	})
	if err != nil || completed.PendingApproval == nil {
		t.Fatalf("CompleteWorkflowTask = %+v, err = %v", completed, err)
	}
	before := getWorkflowGraphAtomicDefinition(t, ctx, service, workflowID)
	removedNodeID := workflowServiceNodeIDByKey(t, before, "implement")
	graph := workflowGraphDraftWithoutNode(before, removedNodeID, workflowServiceNodeIDByKind(t, before, "terminal"))

	preview := previewWorkflowGraphAtomicDraft(t, ctx, service, before, graph)
	nodeReference := &pb.GraphEntityReference{EntityType: pb.GraphEntityType_WORKFLOW_GRAPH_ENTITY_TYPE_NODE, EntityId: removedNodeID}
	if preview.Impact.PendingApprovalCount != 1 || !slices.ContainsFunc(preview.Impact.RemovedEntities, func(value *pb.GraphEntityReference) bool { return proto.Equal(value, nodeReference) }) ||
		!slices.EqualFunc(workflowServiceGraphSaveBlockerEntities(preview.Blockers, "node_task_references"), []*pb.GraphEntityReference{nodeReference}, workflowGraphReferenceEqual) {
		t.Fatalf("Pending Approval preview = %+v", preview)
	}
	blocked := saveWorkflowGraphAtomicPreview(t, ctx, service, before, graph, preview)
	if blocked.Saved {
		t.Fatalf("Pending Approval save = %+v, want blocked", blocked)
	}
	assertWorkflowGraphAtomicUnchanged(t, ctx, service, before)
}

func TestServiceWorkflowGraphSaveAllowsCompletedSessionProvenanceDeletion(t *testing.T) {
	ctx, service, binding, metadataStore := newWorkflowServiceTestContextWithMetadata(t)
	workflowID := createWorkflowServiceChainedWorkflow(t, ctx, service)
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	task := createWorkflowServiceTask(t, ctx, service, serverapi.WorkflowTaskCreateRequest{
		ProjectID: binding.ProjectID, WorkflowID: &workflowID, Title: "Retained Session provenance", LabelIDs: []string{},
	})
	started := startWorkflowServiceTask(t, ctx, service, task.Task.ID)
	beforeCompletion := getWorkflowGraphAtomicDefinition(t, ctx, service, workflowID)
	removedNodeID := started.CurrentNodes[0].NodeID
	taskID := workflow.TaskID(task.Task.ID)
	reference := workflowServiceCurrentNodeReference(t, taskID, started.CurrentNodes[0])
	sessionID := bindWorkflowServiceSessionToTask(t, service, metadataStore, binding, taskID, started.CurrentNodes[0])
	service.currentNodeExecution = newManualMoveExecutionStub(service)
	completed, err := service.CompleteWorkflowTask(ctx, serverapi.WorkflowTaskCompleteRequest{
		ActorKind: serverapi.WorkflowTaskCompleteActorUser, TaskID: task.Task.ID, TransitionID: "next",
		OutputValues: map[string]string{"prior_summary": "completed"}, Force: true,
	})
	implementNodeID := workflowServiceNodeIDByKey(t, beforeCompletion, "implement")
	if err != nil || completed.ForcedMove == nil || completed.ForcedMove.Outcome.Applied == nil ||
		len(completed.ForcedMove.Outcome.Applied.CurrentNodes) != 1 ||
		completed.ForcedMove.Outcome.Applied.CurrentNodes[0].NodeID != implementNodeID {
		t.Fatalf("CompleteWorkflowTask = %+v, err = %v", completed, err)
	}
	completed, err = service.CompleteWorkflowTask(ctx, serverapi.WorkflowTaskCompleteRequest{
		ActorKind: serverapi.WorkflowTaskCompleteActorUser, TaskID: task.Task.ID, TransitionID: "done", Force: true,
	})
	if err != nil || completed.ForcedMove == nil || completed.ForcedMove.Outcome.Applied == nil ||
		len(completed.ForcedMove.Outcome.Applied.CurrentNodes) != 1 ||
		completed.ForcedMove.Outcome.Applied.CurrentNodes[0].NodeID != workflowServiceNodeIDByKind(t, beforeCompletion, "terminal") {
		t.Fatalf("complete implement Node = %+v, err = %v", completed, err)
	}
	before := getWorkflowGraphAtomicDefinition(t, ctx, service, workflowID)
	graph := workflowGraphDraftWithoutNode(before, removedNodeID, implementNodeID)
	preview := previewWorkflowGraphAtomicDraft(t, ctx, service, before, graph)
	if preview.Impact.ActiveCurrentNodeCount != 0 || preview.Impact.PendingApprovalCount != 0 {
		t.Fatalf("completed Session preview = %+v", preview)
	}
	assertWorkflowGraphAtomicChangedSave(t, ctx, service, before, graph)
	if owner, err := service.store.TaskIDForSession(ctx, sessionID); err != nil || owner == nil || *owner != taskID {
		t.Fatalf("retained Session owner = %v, err = %v", owner, err)
	}
	if association, err := service.store.LatestTaskSessionForNode(ctx, reference); err != nil ||
		association.SessionID != sessionID || !association.CurrentNode.Equal(reference) {
		t.Fatalf("retained Session association = %+v, err = %v", association, err)
	}
}

func workflowGraphDraftWithoutNode(
	definition *pb.WorkflowDefinition,
	nodeID string,
	replacementTargetID string,
) *pb.GraphDraft {
	graph := protoapi.WorkflowGraphDraftFromDefinition(definition)
	graph.Nodes = slices.DeleteFunc(graph.Nodes, func(node *pb.GraphDraftNode) bool {
		return node.Id == nodeID
	})
	removedGroups := map[string]struct{}{}
	graph.TransitionGroups = slices.DeleteFunc(graph.TransitionGroups, func(group *pb.GraphDraftTransitionGroup) bool {
		if group.SourceNodeId != nodeID {
			return false
		}
		removedGroups[group.Id] = struct{}{}
		return true
	})
	graph.Edges = slices.DeleteFunc(graph.Edges, func(edge *pb.GraphDraftEdge) bool {
		if _, removed := removedGroups[edge.TransitionGroupId]; removed {
			return true
		}
		return false
	})
	for index := range graph.Edges {
		if graph.Edges[index].TargetNodeId == nodeID {
			graph.Edges[index].TargetNodeId = replacementTargetID
			if workflowServiceNodeByIDForGraphTest(definition, replacementTargetID).Kind == pb.NodeKind_WORKFLOW_NODE_KIND_TERMINAL {
				graph.Edges[index].PromptTemplate = ""
			}
		}
	}
	return graph
}

func workflowServiceNodeByIDForGraphTest(definition *pb.WorkflowDefinition, nodeID string) *pb.WorkflowNode {
	for _, node := range definition.Nodes {
		if node.Id == nodeID {
			return node
		}
	}
	panic("Workflow graph test replacement Node is missing: " + nodeID)
}

func newWorkflowGraphAtomicFanOutFixture(t *testing.T) (context.Context, *Service, runtimeids.WorkflowID) {
	t.Helper()
	ctx, service, _ := newWorkflowServiceTestContext(t)
	created, err := service.CreateWorkflow(ctx, &pb.CreateRequest{Name: "Fan-Out Workflow"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	workflowID := workflowServiceID(t, created.Workflow.Id)
	current := getWorkflowGraphAtomicDefinition(t, ctx, service, workflowID)
	startID := workflowServiceNodeIDByKind(t, current, "start")
	terminalID := workflowServiceNodeIDByKind(t, current, "terminal")
	planID := workflowServiceGraphEntityID("node-plan-" + workflowID.String())
	prepID := workflowServiceGraphEntityID("node-prep-" + workflowID.String())
	branchAID := workflowServiceGraphEntityID("node-a-" + workflowID.String())
	branchBID := workflowServiceGraphEntityID("node-b-" + workflowID.String())
	joinID := workflowServiceGraphEntityID("node-join-" + workflowID.String())
	graph := protoapi.WorkflowGraphDraftFromDefinition(current)
	graph.Nodes = append(graph.Nodes, &pb.GraphDraftNode{Id: planID, Key: "plan", Kind: pb.NodeKind_WORKFLOW_NODE_KIND_AGENT, DisplayName: "Plan", SubagentRole: proto.String("coder")}, &pb.GraphDraftNode{Id: prepID, Key: "prep", Kind: pb.NodeKind_WORKFLOW_NODE_KIND_AGENT, DisplayName: "Prep", SubagentRole: proto.String("coder")}, &pb.GraphDraftNode{Id: branchAID, Key: "a", Kind: pb.NodeKind_WORKFLOW_NODE_KIND_AGENT, DisplayName: "A", SubagentRole: proto.String("coder")}, &pb.GraphDraftNode{Id: branchBID, Key: "b", Kind: pb.NodeKind_WORKFLOW_NODE_KIND_AGENT, DisplayName: "B", SubagentRole: proto.String("coder")}, &pb.GraphDraftNode{Id: joinID, Key: "join", Kind: pb.NodeKind_WORKFLOW_NODE_KIND_JOIN, DisplayName: "Join"})
	startGroup := workflowServiceGraphEntityID("group-start-" + workflowID.String())
	prepGroup := workflowServiceGraphEntityID("group-prep-" + workflowID.String())
	splitGroup := workflowServiceGraphEntityID("group-split-" + workflowID.String())
	alternateGroup := workflowServiceGraphEntityID("group-alternate-" + workflowID.String())
	prepDoneGroup := workflowServiceGraphEntityID("group-prep-done-" + workflowID.String())
	joinAGroup := workflowServiceGraphEntityID("group-join-a-" + workflowID.String())
	joinBGroup := workflowServiceGraphEntityID("group-join-b-" + workflowID.String())
	joinDoneGroup := workflowServiceGraphEntityID("group-join-done-" + workflowID.String())
	graph.TransitionGroups = append(graph.TransitionGroups, &pb.GraphDraftTransitionGroup{Id: startGroup, SourceNodeId: startID, TransitionId: "start", DisplayName: "Start"}, &pb.GraphDraftTransitionGroup{Id: prepGroup, SourceNodeId: planID, TransitionId: "prepare", DisplayName: "Prepare"}, &pb.GraphDraftTransitionGroup{Id: splitGroup, SourceNodeId: planID, TransitionId: "split", DisplayName: "Split"}, &pb.GraphDraftTransitionGroup{Id: alternateGroup, SourceNodeId: planID, TransitionId: "alternate", DisplayName: "Alternate"}, &pb.GraphDraftTransitionGroup{Id: prepDoneGroup, SourceNodeId: prepID, TransitionId: "prep_done", DisplayName: "Done"}, &pb.GraphDraftTransitionGroup{Id: joinAGroup, SourceNodeId: branchAID, TransitionId: "join_a", DisplayName: "Join"}, &pb.GraphDraftTransitionGroup{Id: joinBGroup, SourceNodeId: branchBID, TransitionId: "join_b", DisplayName: "Join"}, &pb.GraphDraftTransitionGroup{Id: joinDoneGroup, SourceNodeId: joinID, TransitionId: "join_done", DisplayName: "Done"})
	graph.Edges = append(graph.Edges,
		workflowGraphAtomicEdge(workflowServiceGraphEntityID("edge-start-"+workflowID.String()), startGroup, "start", planID, "Plan."),
		workflowGraphAtomicEdge(workflowServiceGraphEntityID("edge-prep-"+workflowID.String()), prepGroup, "prepare", prepID, "Prepare."),
		workflowGraphAtomicEdge(workflowServiceGraphEntityID("edge-split-a-"+workflowID.String()), splitGroup, "a", branchAID, "A."),
		workflowGraphAtomicEdge(workflowServiceGraphEntityID("edge-split-b-"+workflowID.String()), splitGroup, "b", branchBID, "B."),
		workflowGraphAtomicEdge(workflowServiceGraphEntityID("edge-alternate-"+workflowID.String()), alternateGroup, "alternate", branchBID, "B."),
		workflowGraphAtomicEdge(workflowServiceGraphEntityID("edge-prep-done-"+workflowID.String()), prepDoneGroup, "prep_done", terminalID, ""),
		workflowGraphAtomicEdge(workflowServiceGraphEntityID("edge-join-a-"+workflowID.String()), joinAGroup, "join_a", joinID, ""),
		workflowGraphAtomicEdge(workflowServiceGraphEntityID("edge-join-b-"+workflowID.String()), joinBGroup, "join_b", joinID, ""),
		workflowGraphAtomicEdge(workflowServiceGraphEntityID("edge-join-done-"+workflowID.String()), joinDoneGroup, "join_done", terminalID, ""),
	)
	saved := previewWorkflowGraphAtomicDraft(t, ctx, service, current, graph)
	response := saveWorkflowGraphAtomicPreview(t, ctx, service, current, graph, saved)
	if !response.Saved || !response.Changed {
		t.Fatalf("seed graph = %+v", response)
	}
	return ctx, service, workflowID
}

func workflowGraphAtomicEdge(id, groupID, key, targetID, prompt string) *pb.GraphDraftEdge {
	return &pb.GraphDraftEdge{
		Id: id, TransitionGroupId: groupID, Key: key, TargetNodeId: targetID,
		AssigneeSelection: pb.AssigneeSelection_WORKFLOW_ASSIGNEE_SELECTION_CONFIGURED, ThinkingSelection: pb.ThinkingSelection_WORKFLOW_THINKING_SELECTION_CONFIGURED,
		ContextMode: pb.ContextMode_WORKFLOW_CONTEXT_MODE_NEW_SESSION, ContextSource: &pb.ContextSource{Kind: pb.ContextSourceKind_WORKFLOW_CONTEXT_SOURCE_KIND_IMMEDIATE_SOURCE},
		PromptTemplate: prompt,
	}
}

func getWorkflowGraphAtomicDefinition(t *testing.T, ctx context.Context, service *Service, workflowID runtimeids.WorkflowID) *pb.WorkflowDefinition {
	t.Helper()
	response, err := service.GetWorkflow(ctx, &pb.GetRequest{WorkflowId: workflowID.String()})
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	return response.Definition
}

func previewWorkflowGraphAtomicDraft(
	t *testing.T,
	ctx context.Context,
	service *Service,
	before *pb.WorkflowDefinition,
	graph *pb.GraphDraft,
) *pb.GraphSavePreviewSuccess {
	t.Helper()
	preview, err := service.PreviewWorkflowGraphSave(ctx, &pb.GraphSavePreviewRequest{
		WorkflowId: before.Workflow.Id, ExpectedVersion: before.Workflow.Version, Graph: graph,
	})
	if err != nil {
		t.Fatalf("PreviewWorkflowGraphSave: %v", err)
	}
	return preview
}

func saveWorkflowGraphAtomicPreview(
	t *testing.T,
	ctx context.Context,
	service *Service,
	before *pb.WorkflowDefinition,
	graph *pb.GraphDraft,
	preview *pb.GraphSavePreviewSuccess,
) *pb.GraphSaveSuccess {
	t.Helper()
	var confirmation *pb.GraphSaveConfirmation
	if preview.ConfirmationRequired {
		confirmation = &pb.GraphSaveConfirmation{
			ExpectedRemovedNodeGroupCount:       preview.Impact.RemovedNodeGroupCount,
			ExpectedRemovedNodeCount:            preview.Impact.RemovedNodeCount,
			ExpectedRemovedTransitionGroupCount: preview.Impact.RemovedTransitionGroupCount,
			ExpectedRemovedEdgeCount:            preview.Impact.RemovedEdgeCount,
			ExpectedNodeTaskReferenceCount:      preview.Impact.NodeTaskReferenceCount,
			ExpectedEdgeTaskReferenceCount:      preview.Impact.EdgeTaskReferenceCount,
		}
	}
	saved, err := service.SaveWorkflowGraph(ctx, &pb.GraphSaveRequest{
		WorkflowId: before.Workflow.Id, ExpectedVersion: before.Workflow.Version, Graph: graph, Confirmation: confirmation,
	})
	if err != nil {
		t.Fatalf("SaveWorkflowGraph: %v", err)
	}
	return saved
}

func assertWorkflowGraphAtomicChangedSave(
	t *testing.T,
	ctx context.Context,
	service *Service,
	before *pb.WorkflowDefinition,
	graph *pb.GraphDraft,
) {
	t.Helper()
	saved := saveWorkflowGraphAtomicPreview(t, ctx, service, before, graph, previewWorkflowGraphAtomicDraft(t, ctx, service, before, graph))
	if !saved.Saved || !saved.Changed {
		t.Fatalf("save = %+v", saved)
	}
	after := getWorkflowGraphAtomicDefinition(t, ctx, service, workflowServiceID(t, before.Workflow.Id))
	if after.Workflow.Version != before.Workflow.Version+1 {
		t.Fatalf("Workflow Version = %d, want %d", after.Workflow.Version, before.Workflow.Version+1)
	}
	reloaded := protoapi.WorkflowGraphDraftFromDefinition(after)
	if !proto.Equal(reloaded, graph) {
		t.Fatalf("reloaded authored graph = %v, want %v", reloaded, graph)
	}
	validated, err := service.ValidateWorkflow(ctx, &pb.ValidateRequest{
		WorkflowId: before.Workflow.Id, Mode: pb.ValidationMode_WORKFLOW_VALIDATION_MODE_EXECUTION.Enum(),
	})
	if err != nil {
		t.Fatalf("ValidateWorkflow execution: %v", err)
	}
	if !validated.Valid {
		t.Fatalf("reloaded Workflow execution validation = %+v", validated)
	}
}

func assertWorkflowGraphAtomicUnchanged(t *testing.T, ctx context.Context, service *Service, before *pb.WorkflowDefinition) {
	t.Helper()
	after := getWorkflowGraphAtomicDefinition(t, ctx, service, workflowServiceID(t, before.Workflow.Id))
	if !proto.Equal(after, before) {
		t.Fatalf("Workflow changed after rejected save")
	}
}
