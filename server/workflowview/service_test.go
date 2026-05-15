package workflowview

import (
	"context"
	"strings"
	"testing"

	"builder/server/metadata"
	"builder/server/workflow"
	"builder/server/workflowstore"
	"builder/shared/config"
)

func TestBoardAndTaskDetailUseDurableWorkflowMetadataOnly(t *testing.T) {
	ctx := context.Background()
	store, workflowStore, binding := newWorkflowViewTestStore(t)
	view, err := New(store)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := workflowStore.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	comment, err := workflowStore.AddComment(ctx, task.ID, "note", "user", "nek")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	deletedComment, err := workflowStore.AddComment(ctx, task.ID, "deleted", "user", "nek")
	if err != nil {
		t.Fatalf("AddComment deleted: %v", err)
	}
	if err := workflowStore.DeleteComment(ctx, deletedComment.ID); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}
	if _, err := workflowStore.CompleteRun(ctx, workflowstore.CompleteRunRequest{RunID: started.RunID, TransitionID: "done", OutputValues: map[string]string{"summary": "done"}}); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}

	board, err := view.GetBoard(ctx, binding.ProjectID)
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if len(board.Workflows) != 1 || len(board.Workflows[0].Tasks) != 1 {
		t.Fatalf("board = %+v", board)
	}
	if !board.Workflows[0].Tasks[0].Done {
		t.Fatalf("task summary should infer done from active terminal placement: %+v", board.Workflows[0].Tasks[0])
	}
	if len(board.Workflows[0].Nodes) < 2 || board.Workflows[0].Nodes[0].Node.Kind != string(workflow.NodeKindStart) {
		t.Fatalf("board node ordering = %+v", board.Workflows[0].Nodes)
	}
	foundDoneNodeTask := false
	for _, node := range board.Workflows[0].Nodes {
		if node.Node.Kind == string(workflow.NodeKindTerminal) && len(node.Tasks) == 1 {
			foundDoneNodeTask = true
		}
	}
	if !foundDoneNodeTask {
		t.Fatalf("board nodes do not contain task on terminal node: %+v", board.Workflows[0].Nodes)
	}

	detail, err := view.GetTask(ctx, string(task.ID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !detail.Summary.Done || len(detail.Placements) != 3 || len(detail.Runs) != 1 || len(detail.Transitions) != 2 || len(detail.Comments) != 1 {
		t.Fatalf("detail = %+v", detail)
	}
	if detail.Comments[0].ID != comment.ID || detail.Transitions[0].TransitionID != "start" || detail.Transitions[1].TransitionID != "done" || detail.Transitions[1].Edges[0].EdgeKey != "done" {
		t.Fatalf("detail history mismatch: %+v", detail)
	}
}

func TestBoardAndTaskDetailProjectParallelBranchPlacements(t *testing.T) {
	ctx := context.Background()
	store, workflowStore, binding := newWorkflowViewTestStore(t)
	view, err := New(store)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	workflowID := createWorkflowViewFanoutWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := workflowStore.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	split, err := workflowStore.CompleteRun(ctx, workflowstore.CompleteRunRequest{RunID: started.RunID, TransitionID: "split", OutputValues: map[string]string{"summary": "plan"}})
	if err != nil {
		t.Fatalf("CompleteRun split: %v", err)
	}

	board, err := view.GetBoard(ctx, binding.ProjectID)
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if len(board.Workflows) != 1 || len(board.Workflows[0].Tasks) != 1 || len(board.Workflows[0].Tasks[0].ActiveNodeIDs) != 2 {
		t.Fatalf("board task summary = %+v, want two active branch nodes", board)
	}
	activeBranchPlacements := 0
	for _, node := range board.Workflows[0].Nodes {
		for _, placement := range node.ActivePlacements {
			if placement.ParallelBatchTransitionID == string(split.TransitionID) && placement.ParallelBranchEdgeID != "" {
				activeBranchPlacements++
			}
		}
	}
	if activeBranchPlacements != 2 {
		t.Fatalf("board active placements = %+v, want two branch placements with batch/branch ids", board.Workflows[0].Nodes)
	}

	detail, err := view.GetTask(ctx, string(task.ID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	detailBranchPlacements := 0
	for _, placement := range detail.Placements {
		if placement.ParallelBatchTransitionID == string(split.TransitionID) && placement.ParallelBranchEdgeID != "" {
			detailBranchPlacements++
		}
	}
	if detailBranchPlacements != 2 {
		t.Fatalf("detail placements = %+v, want two branch placements with batch/branch ids", detail.Placements)
	}
}

func TestTaskDetailProjectsCancellationAndInterruptedRun(t *testing.T) {
	ctx := context.Background()
	store, workflowStore, binding := newWorkflowViewTestStore(t)
	view, err := New(store)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := workflowStore.StartTask(ctx, task.ID); err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if err := workflowStore.CancelTask(ctx, task.ID, "stop"); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	detail, err := view.GetTask(ctx, string(task.ID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if detail.Summary.CanceledAt == 0 || detail.Summary.CancelReason != "stop" {
		t.Fatalf("summary does not project cancellation: %+v", detail.Summary)
	}
	if len(detail.Runs) != 1 || detail.Runs[0].InterruptedAtUnixMs == 0 || detail.Runs[0].InterruptionReason != "task_canceled" {
		t.Fatalf("runs do not project interruption: %+v", detail.Runs)
	}
}

func TestTaskDetailProjectsWaitingAskRun(t *testing.T) {
	ctx := context.Background()
	store, workflowStore, binding := newWorkflowViewTestStore(t)
	view, err := New(store)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := workflowStore.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	claimed, err := workflowStore.ClaimRun(ctx, started.RunID, 0)
	if err != nil {
		t.Fatalf("ClaimRun: %v", err)
	}
	sessionID := "session-view-waiting-ask"
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO sessions (id, project_id, workspace_id, artifact_relpath, name, first_prompt_preview, input_draft, parent_session_id, created_at_unix_ms, updated_at_unix_ms, last_sequence, model_request_count, in_flight_step, agents_injected, launch_visible, cwd_relpath, continuation_json, locked_json, usage_state_json, metadata_json) VALUES (?, ?, ?, ?, '', '', '', '', 1, 1, 0, 0, 0, 0, 1, '.', '{}', '{}', '{}', '{}')`, sessionID, binding.ProjectID, binding.WorkspaceID, "sessions/"+sessionID); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if err := workflowStore.AttachRunSession(ctx, started.RunID, claimed.Generation, sessionID); err != nil {
		t.Fatalf("AttachRunSession: %v", err)
	}
	if err := workflowStore.SetRunWaitingAsk(ctx, started.RunID, claimed.Generation, "ask-view-1"); err != nil {
		t.Fatalf("SetRunWaitingAsk: %v", err)
	}

	detail, err := view.GetTask(ctx, string(task.ID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(detail.Runs) != 1 || detail.Runs[0].WaitingAskID != "ask-view-1" || detail.Runs[0].SessionID != sessionID {
		t.Fatalf("runs do not project waiting ask: %+v", detail.Runs)
	}
}

func newWorkflowViewTestStore(t *testing.T) (*metadata.Store, *workflowstore.Store, metadata.Binding) {
	t.Helper()
	home := t.TempDir()
	workspaceRoot := t.TempDir()
	t.Setenv("HOME", home)
	cfg, err := config.Load(workspaceRoot, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	metadataStore, err := metadata.Open(cfg.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = metadataStore.Close() })
	binding, err := metadataStore.RegisterWorkspaceBinding(context.Background(), cfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	if err := metadataStore.SetProjectKey(context.Background(), binding.ProjectID, "WOR"); err != nil {
		t.Fatalf("SetProjectKey: %v", err)
	}
	workflowStore, err := workflowstore.New(metadataStore, workflowstore.WithRoleResolver(workflow.StaticRoleResolver{"coder": true}))
	if err != nil {
		t.Fatalf("workflowstore.New: %v", err)
	}
	return metadataStore, workflowStore, binding
}

func createWorkflowViewValidWorkflow(t *testing.T, ctx context.Context, store *workflowstore.Store) workflow.WorkflowID {
	t.Helper()
	created, err := store.CreateWorkflow(ctx, workflowstore.CreateWorkflowRequest{Name: "Workflow"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	def, _, err := store.GetDefinition(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	start := workflowViewNodeByKind(t, def, workflow.NodeKindStart)
	done := workflowViewNodeByKind(t, def, workflow.NodeKindTerminal)
	agentID := workflow.NodeID("node-agent-" + string(created.ID))
	if _, err := store.AddNode(ctx, workflowstore.NodeRecord{ID: agentID, WorkflowID: created.ID, Key: "agent", Kind: workflow.NodeKindAgent, DisplayName: "Agent", SubagentRole: "coder", PromptTemplate: "Do work.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if _, err := store.AddTransitionGroup(ctx, workflowstore.TransitionGroupRecord{ID: workflow.TransitionGroupID("group-start-" + string(created.ID)), WorkflowID: created.ID, SourceNodeID: start.ID, TransitionID: "start", DisplayName: "Start"}); err != nil {
		t.Fatalf("AddTransitionGroup start: %v", err)
	}
	if _, err := store.AddEdge(ctx, workflowstore.EdgeRecord{ID: workflow.EdgeID("edge-start-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: workflow.TransitionGroupID("group-start-" + string(created.ID)), Key: "start", TargetNodeID: agentID, ContextMode: workflow.ContextModeNewSession}); err != nil {
		t.Fatalf("AddEdge start: %v", err)
	}
	if _, err := store.AddTransitionGroup(ctx, workflowstore.TransitionGroupRecord{ID: workflow.TransitionGroupID("group-done-" + string(created.ID)), WorkflowID: created.ID, SourceNodeID: agentID, TransitionID: "done", DisplayName: "Done"}); err != nil {
		t.Fatalf("AddTransitionGroup done: %v", err)
	}
	if _, err := store.AddEdge(ctx, workflowstore.EdgeRecord{ID: workflow.EdgeID("edge-done-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: workflow.TransitionGroupID("group-done-" + string(created.ID)), Key: "done", TargetNodeID: done.ID, ContextMode: workflow.ContextModeNewSession, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}}); err != nil {
		t.Fatalf("AddEdge done: %v", err)
	}
	return created.ID
}

func createWorkflowViewFanoutWorkflow(t *testing.T, ctx context.Context, store *workflowstore.Store) workflow.WorkflowID {
	t.Helper()
	created, err := store.CreateWorkflow(ctx, workflowstore.CreateWorkflowRequest{Name: "Fanout Workflow"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	def, _, err := store.GetDefinition(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	start := workflowViewNodeByKind(t, def, workflow.NodeKindStart)
	done := workflowViewNodeByKind(t, def, workflow.NodeKindTerminal)
	planID := workflow.NodeID("node-plan-" + string(created.ID))
	implAID := workflow.NodeID("node-impl-a-" + string(created.ID))
	implBID := workflow.NodeID("node-impl-b-" + string(created.ID))
	joinID := workflow.NodeID("node-join-" + string(created.ID))
	synthID := workflow.NodeID("node-synth-" + string(created.ID))
	for _, node := range []workflowstore.NodeRecord{
		{ID: planID, WorkflowID: created.ID, Key: "plan", Kind: workflow.NodeKindAgent, DisplayName: "Plan", SubagentRole: "coder", PromptTemplate: "Plan.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}},
		{ID: implAID, WorkflowID: created.ID, Key: "impl_a", Kind: workflow.NodeKindAgent, DisplayName: "Implement A", SubagentRole: "coder", PromptTemplate: "A.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}},
		{ID: implBID, WorkflowID: created.ID, Key: "impl_b", Kind: workflow.NodeKindAgent, DisplayName: "Implement B", SubagentRole: "coder", PromptTemplate: "B.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}},
		{ID: joinID, WorkflowID: created.ID, Key: "join", Kind: workflow.NodeKindJoin, DisplayName: "Join"},
		{ID: synthID, WorkflowID: created.ID, Key: "synth", Kind: workflow.NodeKindAgent, DisplayName: "Synthesize", SubagentRole: "coder", PromptTemplate: "Synthesize.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}},
	} {
		if _, err := store.AddNode(ctx, node); err != nil {
			t.Fatalf("AddNode %s: %v", node.Key, err)
		}
	}
	startGroup := workflow.TransitionGroupID("group-start-" + string(created.ID))
	splitGroup := workflow.TransitionGroupID("group-split-" + string(created.ID))
	joinAGroup := workflow.TransitionGroupID("group-join-a-" + string(created.ID))
	joinBGroup := workflow.TransitionGroupID("group-join-b-" + string(created.ID))
	synthGroup := workflow.TransitionGroupID("group-join-synth-" + string(created.ID))
	doneGroup := workflow.TransitionGroupID("group-synth-done-" + string(created.ID))
	for _, group := range []workflowstore.TransitionGroupRecord{
		{ID: startGroup, WorkflowID: created.ID, SourceNodeID: start.ID, TransitionID: "start", DisplayName: "Start"},
		{ID: splitGroup, WorkflowID: created.ID, SourceNodeID: planID, TransitionID: "split", DisplayName: "Split"},
		{ID: joinAGroup, WorkflowID: created.ID, SourceNodeID: implAID, TransitionID: "join", DisplayName: "Join"},
		{ID: joinBGroup, WorkflowID: created.ID, SourceNodeID: implBID, TransitionID: "join", DisplayName: "Join"},
		{ID: synthGroup, WorkflowID: created.ID, SourceNodeID: joinID, TransitionID: "done", DisplayName: "Done"},
		{ID: doneGroup, WorkflowID: created.ID, SourceNodeID: synthID, TransitionID: "done", DisplayName: "Done"},
	} {
		if _, err := store.AddTransitionGroup(ctx, group); err != nil {
			t.Fatalf("AddTransitionGroup %s: %v", group.TransitionID, err)
		}
	}
	for _, edge := range []workflowstore.EdgeRecord{
		{ID: workflow.EdgeID("edge-start-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: startGroup, Key: "start", TargetNodeID: planID, ContextMode: workflow.ContextModeNewSession},
		{ID: workflow.EdgeID("edge-split-a-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: splitGroup, Key: "split_a", TargetNodeID: implAID, ContextMode: workflow.ContextModeNewSession},
		{ID: workflow.EdgeID("edge-split-b-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: splitGroup, Key: "split_b", TargetNodeID: implBID, ContextMode: workflow.ContextModeNewSession},
		{ID: workflow.EdgeID("edge-join-a-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: joinAGroup, Key: "join_a", TargetNodeID: joinID, ContextMode: workflow.ContextModeNewSession, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}},
		{ID: workflow.EdgeID("edge-join-b-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: joinBGroup, Key: "join_b", TargetNodeID: joinID, ContextMode: workflow.ContextModeNewSession, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}},
		{ID: workflow.EdgeID("edge-join-synth-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: synthGroup, Key: "synth", TargetNodeID: synthID, ContextMode: workflow.ContextModeNewSession},
		{ID: workflow.EdgeID("edge-synth-done-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: doneGroup, Key: "done", TargetNodeID: done.ID, ContextMode: workflow.ContextModeNewSession, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}},
	} {
		if _, err := store.AddEdge(ctx, edge); err != nil {
			t.Fatalf("AddEdge %s: %v", edge.Key, err)
		}
	}
	return created.ID
}

func workflowViewNodeByKind(t *testing.T, def workflow.Definition, kind workflow.NodeKind) workflow.Node {
	t.Helper()
	for _, node := range def.Nodes {
		if node.Kind == kind {
			return node
		}
	}
	t.Fatalf("missing node kind %q in %+v", kind, def.Nodes)
	return workflow.Node{}
}

func TestWorkflowViewRejectsMissingIDs(t *testing.T) {
	store, _, _ := newWorkflowViewTestStore(t)
	view, err := New(store)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := view.GetBoard(context.Background(), " "); err == nil || !strings.Contains(err.Error(), "project_id") {
		t.Fatalf("GetBoard missing id error = %v", err)
	}
	if _, err := view.GetTask(context.Background(), " "); err == nil || !strings.Contains(err.Error(), "task_id") {
		t.Fatalf("GetTask missing id error = %v", err)
	}
}
