package workflowstore

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"core/server/workflow"
)

type emptyTargetAgentCatalog struct{}

func (emptyTargetAgentCatalog) ResolveConfiguredRole(string) (workflow.TargetAgentRole, bool) {
	return workflow.TargetAgentRole{}, false
}

func (emptyTargetAgentCatalog) ExplicitCallableRoles() []workflow.TargetAgentRole {
	return nil
}

func TestPendingApprovalReloadUsesCurrentLabelsAndSubmittedValues(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	definition, _, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	reviewEdgeID := edgeByKey(t, definition, "review").ID
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		workflowGraphSaveEdgeRecord(t, req.Edges, reviewEdgeID).RequiresApproval = true
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	source := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	completed, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       source.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "ready"},
		Commentary:   "New snapshot commentary",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode: %v", err)
	}
	if completed.PendingApproval == nil {
		t.Fatal("CompleteCurrentNode did not create a pending Approval")
	}

	if _, err := store.db.ExecContext(ctx, `UPDATE workflow_nodes SET display_name = ? WHERE id = ?`, "Current label", testGraphEntityIDBlob(t, string(completed.PendingApproval.Branches[0].Target.Reference.NodeID))); err != nil {
		t.Fatal(err)
	}

	reloaded, err := store.PendingApproval(ctx, completed.PendingApproval.ID)
	if err != nil {
		t.Fatalf("PendingApproval: %v", err)
	}
	if reloaded.Commentary != "New snapshot commentary" || reloaded.OutputValues["summary"] != "ready" {
		t.Fatalf("pending Approval lost submitted values: %+v", reloaded)
	}
	if len(reloaded.Branches) != 1 {
		t.Fatalf("reloaded pending Approval branches = %d, want one", len(reloaded.Branches))
	}
	target := reloaded.Branches[0].Target
	if target.NodeKind != workflow.NodeKindAgent {
		t.Fatalf("pending Approval target kind = %q, want agent", target.NodeKind)
	}
	if target.DisplayName != "Current label" {
		t.Fatalf("pending Approval retained an obsolete target label: %+v", target)
	}
}

func TestPendingApprovalPreservesSubmittedAgentSelection(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		edge := workflowGraphSaveEdgeRecord(t, req.Edges, testEdgeID("edge-audit-"+workflowID.String()))
		edge.RequiresApproval = true
		edge.AssigneeSelection = workflow.AssigneeSelectionPreviousNode
		edge.Parameters = []workflow.Parameter{{
			Key:     "role",
			Purpose: workflow.ParameterPurposeTargetAssignee,
		}}
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	review, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       plan.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "plan complete"},
	})
	if err != nil {
		t.Fatalf("complete plan: %v", err)
	}
	completed, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       review.Mutation.Created[0].Reference,
		TransitionID: "audit",
		OutputValues: map[string]string{"role": "reviewer"},
	})
	if err != nil {
		t.Fatalf("complete review: %v", err)
	}
	if completed.PendingApproval == nil || len(completed.PendingApproval.Branches) != 1 {
		t.Fatalf("pending approval = %+v, want one frozen branch", completed.PendingApproval)
	}
	if completed.PendingApproval.OutputValues["role"] != "reviewer" {
		t.Fatalf("submitted selection was not retained: %+v", completed.PendingApproval)
	}
	applied, err := applyPendingApproval(t, store, ctx, completed.PendingApproval.ID)
	if err != nil {
		t.Fatalf("ApplyPendingApproval without catalog: %v", err)
	}
	if len(applied.Mutation.Created) != 1 {
		t.Fatalf("applied mutation = %+v, want one frozen target", applied.Mutation)
	}
	appliedSelection := applied.Mutation.Created[0].AgentExecutionSelection
	if appliedSelection == nil || appliedSelection.Assignee != "reviewer" ||
		appliedSelection.Origin != workflow.AssigneeOriginTransitionSelected {
		t.Fatalf("applied target selection = %+v, want frozen reviewer", appliedSelection)
	}
}

func TestPendingApprovalCannotFollowTransitionMovedToAnotherSource(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	id := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	reviewEdge := testEdgeID("edge-review-" + id.String())
	saveWorkflowGraphFixture(t, ctx, store, id, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		workflowGraphSaveEdgeRecord(t, req.Edges, reviewEdge).RequiresApproval = true
		workflowGraphSaveEdgeRecord(t, req.Edges, testEdgeID("edge-audit-"+id.String())).PromptTemplate = "Audit."
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, id, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	source := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	completed, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source: source.Reference, TransitionID: "review", OutputValues: map[string]string{"summary": "ready"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Keep a valid route from Plan to Review, but move the originally selected
	// Transition (and its Branch) to Audit. Neither route is the pending choice.
	saveWorkflowGraphFixture(t, ctx, store, id, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		edge := *workflowGraphSaveEdgeRecord(t, req.Edges, reviewEdge)
		for i, group := range req.TransitionGroups {
			if group.ID != edge.TransitionGroupID {
				continue
			}
			req.TransitionGroups[i].SourceNodeID = testNodeID("node-audit-" + id.String())
			group.ID, group.TransitionID = testTransitionGroupID("replacement"), "replacement"
			edge.ID, edge.TransitionGroupID, edge.Key = testEdgeID("replacement"), group.ID, "replacement"
			req.TransitionGroups = append(req.TransitionGroups, group)
			req.Edges = append(req.Edges, edge)
			return
		}
		t.Fatal("missing review Transition")
	})
	approvalID := completed.PendingApproval.ID
	_, err = applyPendingApproval(t, store, ctx, approvalID)
	var manualMove *TaskContextSelectionRequiredError
	if !errors.As(err, &manualMove) || manualMove.TaskID != task.ID {
		t.Fatalf("approve moved Transition: got %v, want Manual Move for original Task", err)
	}
	pending, err := store.ListPendingApprovals(ctx, task.ID)
	if err != nil || len(pending) != 1 || pending[0].ID != approvalID || pending[0].ChoiceAvailable() ||
		!pending[0].Source.Equal(source.Reference) || pending[0].OutputValues["summary"] != "ready" {
		t.Fatalf("original Approval should remain pending and unavailable: %+v, %v", pending, err)
	}
}

func TestPendingApprovalInputMismatchKeepsOriginalChoicePending(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	requireApprovalOnWorkflowEdge(t, ctx, store, workflowID, "review")
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	source := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	completed, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source: source.Reference, TransitionID: "review", OutputValues: map[string]string{"summary": "submitted"},
	})
	if err != nil {
		t.Fatal(err)
	}
	approval := completed.PendingApproval
	replaceTestParameters(t, store, string(approval.Branches[0].EdgeID), []workflow.Parameter{
		{Key: "summary", Description: "Summary.", Purpose: workflow.ParameterPurposeOrdinary},
		{Key: "risk", Description: "Risk.", Purpose: workflow.ParameterPurposeOrdinary},
	})
	before, err := store.PendingApproval(ctx, approval.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.PlanPendingApproval(ctx, approval.ID)
	var manualMove *TaskContextSelectionRequiredError
	if !errors.As(err, &manualMove) || manualMove.TaskID != task.ID {
		t.Fatalf("approval mismatch = %v, want typed Manual Move requirement", err)
	}
	after, err := store.PendingApproval(ctx, approval.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("pending choice changed: before=%+v after=%+v error=%v", before, after, err)
	}
	nodes, err := store.ListCurrentNodes(ctx, task.ID)
	if err != nil || len(nodes) != 1 || !nodes[0].Reference.Equal(source.Reference) {
		t.Fatalf("approval mismatch changed source: %+v, %v", nodes, err)
	}
}

func TestPendingApprovalKeepsParallelSourceScopeAndSibling(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	requireApprovalOnWorkflowEdge(t, ctx, store, workflowID, "review")
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	source := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	leftKey, rightKey := workflow.TransitionBranchKey("left"), workflow.TransitionBranchKey("right")
	left, err := workflow.NewCurrentNodeReference(task.ID, source.Reference.NodeID, &leftKey)
	if err != nil {
		t.Fatal(err)
	}
	right, err := workflow.NewCurrentNodeReference(task.ID, source.Reference.NodeID, &rightKey)
	if err != nil {
		t.Fatal(err)
	}
	seedParallelCurrentApprovalSources(t, ctx, store, task.ID, source.Reference, workflow.CurrentNode{Reference: left}, workflow.CurrentNode{Reference: right})
	completed, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source: left, TransitionID: "review", OutputValues: map[string]string{"summary": "left only"},
	})
	if err != nil {
		t.Fatal(err)
	}
	applied, err := applyPendingApproval(t, store, ctx, completed.PendingApproval.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied.Mutation.Removed) != 1 || !applied.Mutation.Removed[0].Equal(left) || len(applied.Mutation.Created) != 1 {
		t.Fatalf("approval changed the wrong scope: %+v", applied.Mutation)
	}
	key, scoped := applied.Mutation.Created[0].Reference.TransitionBranchKey()
	if !scoped || key != leftKey {
		t.Fatalf("approval lost original branch scope: %+v", applied.Mutation.Created[0])
	}
	nodes, err := store.ListCurrentNodes(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range nodes {
		if current.Reference.Equal(right) {
			return
		}
	}
	t.Fatalf("approval removed the parallel sibling: %+v", nodes)
}

func TestPendingApprovalDeletedContextRequiresManualMove(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		edge := workflowGraphSaveEdgeRecord(t, req.Edges, testEdgeID("edge-audit-"+workflowID.String()))
		edge.RequiresApproval = true
		edge.ContextMode = workflow.ContextModeContinueSession
		edge.ContextSource = workflow.ContextSource{Kind: workflow.ContextSourceSelectedNode, NodeKey: "plan"}
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	planSession := currentNodeSessionForStoreTest(t, ctx, store, plan.Reference)
	review, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source: plan.Reference, TransitionID: "review", OutputValues: map[string]string{"summary": "submitted"},
	})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source: review.Mutation.Created[0].Reference, TransitionID: "audit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.metadata.DeleteSession(ctx, planSession.String()); err != nil {
		t.Fatalf("obsolete Approval context prevented explicit deletion: %v", err)
	}
	_, err = store.PlanPendingApproval(ctx, completed.PendingApproval.ID)
	var manualMove *TaskContextSelectionRequiredError
	if !errors.As(err, &manualMove) || manualMove.TaskID != task.ID {
		t.Fatalf("missing Approval context = %v, want typed Manual Move error", err)
	}
	remaining, err := store.PendingApproval(ctx, completed.PendingApproval.ID)
	if err != nil || !reflect.DeepEqual(remaining.OutputValues, completed.PendingApproval.OutputValues) {
		t.Fatalf("unavailable context lost pending work: %+v, %v", remaining, err)
	}
}

func TestListPendingApprovalsKeepsParallelSourcesIndependent(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createFanoutJoinWorkflow(t, ctx, store)
	requireApprovalOnWorkflowEdge(t, ctx, store, workflowID, "join_a")
	requireApprovalOnWorkflowEdge(t, ctx, store, workflowID, "join_b")
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	started := startTask(t, ctx, store, task.ID)
	definition, _, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	implementationA := nodeByKey(t, definition, "impl_a")
	implementationB := nodeByKey(t, definition, "impl_b")
	join := nodeByKey(t, definition, "join")
	joinAEdge := edgeByKey(t, definition, "join_a")
	joinBEdge := edgeByKey(t, definition, "join_b")
	groups := map[workflow.TransitionGroupID]workflow.TransitionGroup{}
	for _, group := range definition.TransitionGroups {
		groups[group.ID] = group
	}

	branchA := workflow.TransitionBranchKey("split_a")
	branchB := workflow.TransitionBranchKey("split_b")
	sourceAReference, err := workflow.NewCurrentNodeReference(task.ID, workflow.NodeIDOf(implementationA), &branchA)
	if err != nil {
		t.Fatalf("NewCurrentNodeReference branch A: %v", err)
	}
	sourceBReference, err := workflow.NewCurrentNodeReference(task.ID, workflow.NodeIDOf(implementationB), &branchB)
	if err != nil {
		t.Fatalf("NewCurrentNodeReference branch B: %v", err)
	}
	sourceA, err := workflow.NewCurrentNode(sourceAReference, nil, &workflow.CurrentNodeScheduling{State: workflow.CurrentNodeSchedulingReady})
	if err != nil {
		t.Fatalf("NewCurrentNode branch A: %v", err)
	}
	sourceB, err := workflow.NewCurrentNode(sourceBReference, nil, &workflow.CurrentNodeScheduling{State: workflow.CurrentNodeSchedulingReady})
	if err != nil {
		t.Fatalf("NewCurrentNode branch B: %v", err)
	}
	targetAReference, err := workflow.NewCurrentNodeReference(task.ID, workflow.NodeIDOf(join), &branchA)
	if err != nil {
		t.Fatalf("NewCurrentNodeReference join A: %v", err)
	}
	targetBReference, err := workflow.NewCurrentNodeReference(task.ID, workflow.NodeIDOf(join), &branchB)
	if err != nil {
		t.Fatalf("NewCurrentNodeReference join B: %v", err)
	}
	targetA, err := workflow.NewCurrentNode(targetAReference, nil, nil)
	if err != nil {
		t.Fatalf("NewCurrentNode join A: %v", err)
	}
	targetB, err := workflow.NewCurrentNode(targetBReference, nil, nil)
	if err != nil {
		t.Fatalf("NewCurrentNode join B: %v", err)
	}

	seedParallelCurrentApprovalSources(t, ctx, store, task.ID, started.Mutation.Created[0].Reference, sourceA, sourceB)
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx pending approvals: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := store.queries.WithTx(tx)
	approvalA, err := newPendingApproval(
		sourceA,
		groups[joinAEdge.TransitionGroupID],
		workflow.NodeDisplayName(implementationA),
		joinAEdge,
		join,
		targetA,
		"Branch A is ready.",
		map[string]string{"joined": "A"},
		time.UnixMilli(1_700_000_000_001).UTC(),
	)
	if err != nil {
		t.Fatalf("newPendingApproval branch A: %v", err)
	}
	approvalB, err := newPendingApproval(
		sourceB,
		groups[joinBEdge.TransitionGroupID],
		workflow.NodeDisplayName(implementationB),
		joinBEdge,
		join,
		targetB,
		"Branch B is ready.",
		map[string]string{"joined": "B"},
		time.UnixMilli(1_700_000_000_002).UTC(),
	)
	if err != nil {
		t.Fatalf("newPendingApproval branch B: %v", err)
	}
	if err := insertPendingApproval(ctx, q, approvalA); err != nil {
		t.Fatalf("insertPendingApproval branch A: %v", err)
	}
	if err := insertPendingApproval(ctx, q, approvalB); err != nil {
		t.Fatalf("insertPendingApproval branch B: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit pending approvals: %v", err)
	}

	approvals, err := store.ListPendingApprovals(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListPendingApprovals: %v", err)
	}
	if len(approvals) != 2 {
		t.Fatalf("pending approvals = %+v, want two independent branch approvals", approvals)
	}
	for _, source := range []workflow.CurrentNodeReference{sourceAReference, sourceBReference} {
		var approval *workflow.PendingApproval
		for index := range approvals {
			if approvals[index].Source.Equal(source) {
				approval = &approvals[index]
				break
			}
		}
		if approval == nil || !approval.Source.IsBranchScoped() || len(approval.Branches) != 1 {
			t.Fatalf("branch approval for source %v = %+v, want one independent pending approval", source, approval)
		}
		eligible, err := store.IsCurrentNodeExecutionEligible(ctx, source)
		if err != nil {
			t.Fatalf("IsCurrentNodeExecutionEligible %v: %v", source, err)
		}
		if eligible {
			t.Fatalf("branch source %v remained eligible while pending approval exists", source)
		}
	}
}

func seedParallelCurrentApprovalSources(
	t *testing.T,
	ctx context.Context,
	store *Store,
	taskID workflow.TaskID,
	serialSource workflow.CurrentNodeReference,
	sourceA workflow.CurrentNode,
	sourceB workflow.CurrentNode,
) {
	t.Helper()
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx parallel source seed: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
DELETE FROM task_current_nodes
WHERE task_id = ?
  AND node_id = ?
  AND transition_branch_key IS NULL`, string(serialSource.TaskID), testGraphEntityIDBlob(t, string(serialSource.NodeID))); err != nil {
		t.Fatalf("delete serial current node: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_active_fanouts (task_id) VALUES (?)`, string(taskID)); err != nil {
		t.Fatalf("insert task active fanout: %v", err)
	}
	for _, branch := range []workflow.CurrentNode{sourceA, sourceB} {
		branchKey, present := branch.Reference.TransitionBranchKey()
		if !present {
			t.Fatalf("parallel source %v has no branch key", branch.Reference)
		}
		var subagentRole string
		if err := tx.QueryRowContext(ctx, `SELECT subagent_role FROM workflow_nodes WHERE id = ?`, testGraphEntityIDBlob(t, string(branch.Reference.NodeID))).Scan(&subagentRole); err != nil {
			t.Fatalf("resolve branch Agent fallback %q: %v", branchKey, err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO task_active_fanout_branches (
    task_id,
    transition_branch_key,
    arrival_state,
    arrival_values_json
) VALUES (?, ?, 'pending', NULL)`, string(taskID), string(branchKey)); err != nil {
			t.Fatalf("insert active fanout branch %q: %v", branchKey, err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO task_current_nodes (
    task_id,
    node_id,
    transition_branch_key,
    current_input_values_json,
    prior_node_values_json,
    session_id,
    scheduling_state,
    interruption_reason,
    interruption_detail_json,
    interrupted_at_unix_ms,
    effective_assignee,
    assignee_origin
) VALUES (?, ?, ?, '{}', '{"transition_parameters":{}}', NULL, 'ready', NULL, NULL, NULL, ?, 'configured_fallback')`,
			string(branch.Reference.TaskID),
			testGraphEntityIDBlob(t, string(branch.Reference.NodeID)),
			string(branchKey),
			subagentRole,
		); err != nil {
			t.Fatalf("insert branch current node %q: %v", branchKey, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit parallel source seed: %v", err)
	}
}
