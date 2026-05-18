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
