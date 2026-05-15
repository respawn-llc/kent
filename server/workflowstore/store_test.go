package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"builder/server/metadata"
	"builder/server/session"
	"builder/server/workflow"
	"builder/shared/config"
)

func TestWorkflowCreateUpdateReadAndGraphPersistence(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)

	created, err := store.CreateWorkflow(ctx, CreateWorkflowRequest{Name: "Default Pipeline", Description: "desc"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	def, record, err := store.GetDefinition(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	if record.GraphRevision != 1 {
		t.Fatalf("graph revision = %d, want 1", record.GraphRevision)
	}
	if !hasNode(def, "backlog", workflow.NodeKindStart) || !hasNode(def, "done", workflow.NodeKindTerminal) {
		t.Fatalf("default nodes missing from %+v", def.Nodes)
	}
	if err := store.UpdateWorkflowInfo(ctx, created.ID, "Renamed", "new desc"); err != nil {
		t.Fatalf("UpdateWorkflowInfo: %v", err)
	}
	_, renamed, err := store.GetDefinition(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetDefinition renamed: %v", err)
	}
	if renamed.Name != "Renamed" || renamed.GraphRevision != 1 {
		t.Fatalf("workflow info update = %+v, want name changed without graph revision bump", renamed)
	}
	if err := store.UpdateWorkflowInfo(ctx, created.ID, "   ", "new desc"); err == nil || !strings.Contains(err.Error(), "workflow name is required") {
		t.Fatalf("UpdateWorkflowInfo blank name error = %v", err)
	}

	start := nodeByKind(t, def, workflow.NodeKindStart)
	done := nodeByKind(t, def, workflow.NodeKindTerminal)
	revision, err := store.AddNode(ctx, NodeRecord{ID: "node-agent", WorkflowID: created.ID, Key: "agent", Kind: workflow.NodeKindAgent, DisplayName: "Agent", SubagentRole: "coder", PromptTemplate: "Do work.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}})
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if revision != 2 {
		t.Fatalf("revision after add node = %d, want 2", revision)
	}
	if _, err := store.AddTransitionGroup(ctx, TransitionGroupRecord{ID: "group-start", WorkflowID: created.ID, SourceNodeID: start.ID, TransitionID: "start", DisplayName: "Start"}); err != nil {
		t.Fatalf("AddTransitionGroup start: %v", err)
	}
	if _, err := store.AddEdge(ctx, EdgeRecord{ID: "edge-start", WorkflowID: created.ID, TransitionGroupID: "group-start", Key: "start", TargetNodeID: "node-agent", ContextMode: workflow.ContextModeNewSession}); err != nil {
		t.Fatalf("AddEdge start: %v", err)
	}
	if _, err := store.AddTransitionGroup(ctx, TransitionGroupRecord{ID: "group-done", WorkflowID: created.ID, SourceNodeID: "node-agent", TransitionID: "done", DisplayName: "Done"}); err != nil {
		t.Fatalf("AddTransitionGroup done: %v", err)
	}
	if _, err := store.AddEdge(ctx, EdgeRecord{ID: "edge-done", WorkflowID: created.ID, TransitionGroupID: "group-done", Key: "done", TargetNodeID: done.ID, ContextMode: workflow.ContextModeNewSession, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}}); err != nil {
		t.Fatalf("AddEdge done: %v", err)
	}
	updated, updatedRecord, err := store.GetDefinition(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetDefinition updated: %v", err)
	}
	if updatedRecord.GraphRevision != 6 {
		t.Fatalf("graph revision after graph edits = %d, want 6", updatedRecord.GraphRevision)
	}
	if len(updated.TransitionGroups) != 2 || len(updated.Edges) != 2 {
		t.Fatalf("graph persistence mismatch: groups=%+v edges=%+v", updated.TransitionGroups, updated.Edges)
	}
	workflows, err := store.ListWorkflows(ctx)
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(workflows) != 1 || workflows[0].ID != created.ID {
		t.Fatalf("ListWorkflows = %+v", workflows)
	}
}

func TestTaskCreateStartCancelAndComments(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Implement feature", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask default: %v", err)
	}
	if !strings.HasPrefix(task.ShortID, "WOR-1") {
		t.Fatalf("short id = %q, want WOR-1 prefix", task.ShortID)
	}
	placements, err := store.ListPlacements(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListPlacements after create: %v", err)
	}
	if len(placements) != 1 || placements[0].State != "active" {
		t.Fatalf("placements after create = %+v", placements)
	}

	started, err := store.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if started.RunID == "" || started.PlacementID == "" {
		t.Fatalf("start result missing run/placement ids: %+v", started)
	}
	runs, err := store.ListRuns(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].AutomationRequestedAt == 0 {
		t.Fatalf("runs after start = %+v", runs)
	}
	transitions, err := store.ListTransitions(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListTransitions: %v", err)
	}
	if len(transitions) != 1 || transitions[0].TransitionID != "start" {
		t.Fatalf("transitions after start = %+v", transitions)
	}
	transitionEdges, err := store.ListTransitionEdges(ctx, transitions[0].ID)
	if err != nil {
		t.Fatalf("ListTransitionEdges: %v", err)
	}
	if len(transitionEdges) != 1 || transitionEdges[0].EdgeKey != "start" || transitionEdges[0].TargetPlacementID != started.PlacementID {
		t.Fatalf("transition edge snapshot after start = %+v", transitionEdges)
	}

	comment, err := store.AddComment(ctx, task.ID, " first note ", "agent", "coder")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if err := store.ReplaceComment(ctx, comment.ID, "updated"); err != nil {
		t.Fatalf("ReplaceComment: %v", err)
	}
	comments, err := store.ListComments(ctx, task.ID, false)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(comments) != 1 || comments[0].Body != "updated" {
		t.Fatalf("comments after replace = %+v", comments)
	}
	if err := store.DeleteComment(ctx, comment.ID); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}
	comments, err = store.ListComments(ctx, task.ID, false)
	if err != nil {
		t.Fatalf("ListComments visible: %v", err)
	}
	if len(comments) != 0 {
		t.Fatalf("deleted comment should be hidden, got %+v", comments)
	}

	if err := store.CancelTask(ctx, task.ID, "stop"); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	if err := store.CancelTask(ctx, "task-missing", "stop"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("CancelTask missing = %v, want sql.ErrNoRows", err)
	}
	runs, err = store.ListRuns(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListRuns after cancel: %v", err)
	}
	if runs[0].InterruptedAt == 0 || runs[0].InterruptionReason != "task_canceled" {
		t.Fatalf("run not interrupted by cancel: %+v", runs[0])
	}
}

func TestCompleteRunUsesRunStartSnapshotAfterGraphChanges(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := store.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	beforeEdit := currentWorkflowRevision(t, ctx, store, workflowID)
	def, _, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	agent := nodeByKey(t, def, "agent")
	if _, err := store.AddNode(ctx, NodeRecord{ID: "node-extra-terminal", WorkflowID: workflowID, Key: "archived", Kind: workflow.NodeKindTerminal, DisplayName: "Archived"}); err != nil {
		t.Fatalf("AddNode extra terminal: %v", err)
	}
	if _, err := store.AddTransitionGroup(ctx, TransitionGroupRecord{ID: "group-archive", WorkflowID: workflowID, SourceNodeID: agent.ID, TransitionID: "archive", DisplayName: "Archive"}); err != nil {
		t.Fatalf("AddTransitionGroup archive: %v", err)
	}
	if _, err := store.AddEdge(ctx, EdgeRecord{ID: "edge-archive", WorkflowID: workflowID, TransitionGroupID: "group-archive", Key: "archive", TargetNodeID: "node-extra-terminal", ContextMode: workflow.ContextModeNewSession}); err != nil {
		t.Fatalf("AddEdge archive: %v", err)
	}
	if _, err := store.CompleteRun(ctx, CompleteRunRequest{RunID: started.RunID, TransitionID: "archive", OutputValues: map[string]string{"summary": "done"}}); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected completion to reject transition added after run start, got %v", err)
	}
	completed, err := store.CompleteRun(ctx, CompleteRunRequest{RunID: started.RunID, TransitionID: "done", OutputValues: map[string]string{"summary": "done"}, Commentary: "finished"})
	if err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	if completed.State != "applied" || len(completed.PlacementIDs) != 1 || len(completed.RunIDs) != 0 {
		t.Fatalf("completion result = %+v", completed)
	}
	transitions, err := store.ListTransitions(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListTransitions: %v", err)
	}
	if len(transitions) != 2 || transitions[1].TransitionID != "done" || transitions[1].State != "applied" {
		t.Fatalf("transitions after completion = %+v", transitions)
	}
	edges, err := store.ListTransitionEdges(ctx, transitions[1].ID)
	if err != nil {
		t.Fatalf("ListTransitionEdges: %v", err)
	}
	if len(edges) != 1 || edges[0].EdgeKey != "done" || edges[0].WorkflowRevisionSeen != beforeEdit || edges[0].TargetPlacementID != completed.PlacementIDs[0] {
		t.Fatalf("completion edge snapshot = %+v, want one done edge at revision %d", edges, beforeEdit)
	}
	placements, err := store.ListPlacements(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListPlacements: %v", err)
	}
	terminalActive := false
	sourceCompleted := false
	for _, placement := range placements {
		if placement.ID == started.PlacementID && placement.State == "completed" {
			sourceCompleted = true
		}
		if placement.ID == completed.PlacementIDs[0] && placement.State == "active" {
			terminalActive = true
		}
	}
	if !sourceCompleted || !terminalActive {
		t.Fatalf("placements after completion = %+v, want completed source and active terminal", placements)
	}
	runs, err := store.ListRuns(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].CompletedAt == 0 {
		t.Fatalf("runs after completion = %+v", runs)
	}
}

func TestCompleteRunBuildsChildSnapshotFromParentRevision(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createValidWorkflow(t, ctx, store)
	def, _, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	agent := nodeByKey(t, def, "agent")
	done := nodeByKind(t, def, workflow.NodeKindTerminal)
	reviewerID := workflow.NodeID("node-reviewer-" + string(workflowID))
	if _, err := store.AddNode(ctx, NodeRecord{ID: reviewerID, WorkflowID: workflowID, Key: "reviewer", Kind: workflow.NodeKindAgent, DisplayName: "Reviewer", SubagentRole: "coder", PromptTemplate: "Review work.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}}); err != nil {
		t.Fatalf("AddNode reviewer: %v", err)
	}
	if _, err := store.AddTransitionGroup(ctx, TransitionGroupRecord{ID: workflow.TransitionGroupID("group-review-" + string(workflowID)), WorkflowID: workflowID, SourceNodeID: agent.ID, TransitionID: "review", DisplayName: "Review"}); err != nil {
		t.Fatalf("AddTransitionGroup review: %v", err)
	}
	if _, err := store.AddEdge(ctx, EdgeRecord{ID: workflow.EdgeID("edge-review-" + string(workflowID)), WorkflowID: workflowID, TransitionGroupID: workflow.TransitionGroupID("group-review-" + string(workflowID)), Key: "review", TargetNodeID: reviewerID, ContextMode: workflow.ContextModeNewSession}); err != nil {
		t.Fatalf("AddEdge review: %v", err)
	}
	if _, err := store.AddTransitionGroup(ctx, TransitionGroupRecord{ID: workflow.TransitionGroupID("group-review-done-" + string(workflowID)), WorkflowID: workflowID, SourceNodeID: reviewerID, TransitionID: "review_done", DisplayName: "Review Done"}); err != nil {
		t.Fatalf("AddTransitionGroup review done: %v", err)
	}
	if _, err := store.AddEdge(ctx, EdgeRecord{ID: workflow.EdgeID("edge-review-done-" + string(workflowID)), WorkflowID: workflowID, TransitionGroupID: workflow.TransitionGroupID("group-review-done-" + string(workflowID)), Key: "review_done", TargetNodeID: done.ID, ContextMode: workflow.ContextModeNewSession, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}}); err != nil {
		t.Fatalf("AddEdge review done: %v", err)
	}
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := store.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	archiveID := workflow.NodeID("node-archive-" + string(workflowID))
	if _, err := store.AddNode(ctx, NodeRecord{ID: archiveID, WorkflowID: workflowID, Key: "archive", Kind: workflow.NodeKindTerminal, DisplayName: "Archive"}); err != nil {
		t.Fatalf("AddNode archive: %v", err)
	}
	if _, err := store.AddTransitionGroup(ctx, TransitionGroupRecord{ID: workflow.TransitionGroupID("group-review-archive-" + string(workflowID)), WorkflowID: workflowID, SourceNodeID: reviewerID, TransitionID: "archive", DisplayName: "Archive"}); err != nil {
		t.Fatalf("AddTransitionGroup archive: %v", err)
	}
	if _, err := store.AddEdge(ctx, EdgeRecord{ID: workflow.EdgeID("edge-review-archive-" + string(workflowID)), WorkflowID: workflowID, TransitionGroupID: workflow.TransitionGroupID("group-review-archive-" + string(workflowID)), Key: "archive", TargetNodeID: archiveID, ContextMode: workflow.ContextModeNewSession}); err != nil {
		t.Fatalf("AddEdge archive: %v", err)
	}
	completed, err := store.CompleteRun(ctx, CompleteRunRequest{RunID: started.RunID, TransitionID: "review", OutputValues: map[string]string{"summary": "done"}})
	if err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	if len(completed.RunIDs) != 1 {
		t.Fatalf("completion child runs = %+v, want one", completed.RunIDs)
	}
	runContext, err := store.GetRunStartContext(ctx, completed.RunIDs[0])
	if err != nil {
		t.Fatalf("GetRunStartContext: %v", err)
	}
	if len(runContext.TransitionIDs) != 1 || runContext.TransitionIDs[0] != "review_done" {
		t.Fatalf("child transition ids = %+v, want only review_done from parent snapshot", runContext.TransitionIDs)
	}
}

func TestStartTaskRejectsCanceledAndAlreadyStartedTasks(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	canceled, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Canceled", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask canceled: %v", err)
	}
	if err := store.CancelTask(ctx, canceled.ID, "stop"); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	if _, err := store.StartTask(ctx, canceled.ID); err == nil || !strings.Contains(err.Error(), "task is canceled") {
		t.Fatalf("StartTask canceled error = %v", err)
	}

	startedTask, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Started", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask started: %v", err)
	}
	if _, err := store.StartTask(ctx, startedTask.ID); err != nil {
		t.Fatalf("StartTask first: %v", err)
	}
	if _, err := store.StartTask(ctx, startedTask.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("StartTask second = %v, want sql.ErrNoRows", err)
	}
	runs, err := store.ListRuns(ctx, startedTask.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs after duplicate start = %+v, want exactly one", runs)
	}
}

func TestStartTaskConcurrentCallsCreateOneRun(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := store.StartTask(ctx, task.ID)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	successes := 0
	noRows := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, sql.ErrNoRows):
			noRows++
		default:
			t.Fatalf("StartTask concurrent unexpected error: %v", err)
		}
	}
	if successes != 1 || noRows != 1 {
		t.Fatalf("concurrent starts successes=%d noRows=%d, want 1/1", successes, noRows)
	}
	runs, err := store.ListRuns(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs after concurrent start = %+v, want exactly one", runs)
	}
}

func TestCompleteRunRejectsUnsupportedRuntimeSnapshots(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *runStartSnapshot)
		want   string
	}{
		{
			name: "approval gated transition",
			mutate: func(t *testing.T, snapshot *runStartSnapshot) {
				mutateSnapshotTransition(t, snapshot, "done", func(group *transitionContractSnapshot) {
					group.Edges[0].RequiresApproval = true
				})
			},
			want: "approval-gated edges cannot execute",
		},
		{
			name: "join target",
			mutate: func(t *testing.T, snapshot *runStartSnapshot) {
				mutateSnapshotTransition(t, snapshot, "done", func(group *transitionContractSnapshot) {
					group.Edges[0].TargetNode.Kind = workflow.NodeKindJoin
				})
			},
			want: "join targets cannot execute",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store, binding := newTestStore(t)
			workflowID := createValidWorkflow(t, ctx, store)
			if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
				t.Fatalf("LinkWorkflow: %v", err)
			}
			task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
			if err != nil {
				t.Fatalf("CreateTask: %v", err)
			}
			started, err := store.StartTask(ctx, task.ID)
			if err != nil {
				t.Fatalf("StartTask: %v", err)
			}
			row, err := store.queries.GetTaskRun(ctx, string(started.RunID))
			if err != nil {
				t.Fatalf("GetTaskRun: %v", err)
			}
			snapshot := runStartSnapshot{}
			if err := unmarshalJSON(row.RunStartSnapshotJson, &snapshot); err != nil {
				t.Fatalf("unmarshal snapshot: %v", err)
			}
			tt.mutate(t, &snapshot)
			snapshotJSON, err := marshalJSON(snapshot)
			if err != nil {
				t.Fatalf("marshal snapshot: %v", err)
			}
			if _, err := store.db.ExecContext(ctx, `UPDATE task_runs SET run_start_snapshot_json = ? WHERE id = ?`, snapshotJSON, string(started.RunID)); err != nil {
				t.Fatalf("update snapshot: %v", err)
			}

			_, err = store.CompleteRun(ctx, CompleteRunRequest{RunID: started.RunID, TransitionID: "done", OutputValues: map[string]string{"summary": "done"}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("CompleteRun error = %v, want %q", err, tt.want)
			}
			runs, err := store.ListRuns(ctx, task.ID)
			if err != nil {
				t.Fatalf("ListRuns: %v", err)
			}
			if len(runs) != 1 || runs[0].CompletedAt != 0 {
				t.Fatalf("run after rejected completion = %+v, want still active", runs)
			}
		})
	}
}

func TestCompleteRunCreatesTargetRunForContinueSessionContextMode(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createChainedContextModeWorkflow(t, ctx, store, workflow.ContextModeContinueSession, "coder")
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := store.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}

	completed, err := store.CompleteRun(ctx, CompleteRunRequest{RunID: started.RunID, TransitionID: "next", OutputValues: map[string]string{"summary": "plan done"}})
	if err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	if len(completed.RunIDs) != 1 {
		t.Fatalf("target run ids = %+v, want one continuation target", completed.RunIDs)
	}
	runs, err := store.ListRuns(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 || runs[0].CompletedAt == 0 || runs[1].CompletedAt != 0 || runs[1].InterruptedAt != 0 {
		t.Fatalf("runs after continuation completion = %+v, want completed source and active target", runs)
	}
	edges, err := store.ListTransitionEdges(ctx, completed.TransitionID)
	if err != nil {
		t.Fatalf("ListTransitionEdges: %v", err)
	}
	var persistedContextMode string
	if err := store.db.QueryRowContext(ctx, `SELECT context_mode FROM task_transition_edges WHERE id = ?`, edges[0].ID).Scan(&persistedContextMode); err != nil {
		t.Fatalf("query transition edge context mode: %v", err)
	}
	if len(edges) != 1 || persistedContextMode != string(workflow.ContextModeContinueSession) || edges[0].TargetPlacementID != runs[1].PlacementID {
		t.Fatalf("transition edge snapshot = %+v, want continue_session target edge", edges)
	}
	input, err := store.GetRunStartContext(ctx, completed.RunIDs[0])
	if err != nil {
		t.Fatalf("GetRunStartContext: %v", err)
	}
	if input.Node.Key != "implement" || input.InputValues["prior_summary"] != "plan done" {
		t.Fatalf("target run context = %+v, want implement node with bound prior output", input)
	}
	var runMetadataJSON string
	if err := store.db.QueryRowContext(ctx, `SELECT metadata_json FROM task_runs WHERE id = ?`, string(completed.RunIDs[0])).Scan(&runMetadataJSON); err != nil {
		t.Fatalf("query target run metadata: %v", err)
	}
	runMetadata := struct {
		ContextMode     string `json:"context_mode"`
		SourceRunID     string `json:"source_run_id"`
		SourceSessionID string `json:"source_session_id"`
	}{}
	if err := unmarshalJSON(runMetadataJSON, &runMetadata); err != nil {
		t.Fatalf("unmarshal target run metadata: %v", err)
	}
	if runMetadata.ContextMode != string(workflow.ContextModeContinueSession) || runMetadata.SourceRunID != string(started.RunID) {
		t.Fatalf("target run metadata = %+v, want context mode and source run", runMetadata)
	}
}

func TestCreateTaskRejectsCrossRoleContinueSessionContextMode(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createChainedContextModeWorkflow(t, ctx, store, workflow.ContextModeContinueSession, "reviewer")
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}

	_, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err == nil || !strings.Contains(err.Error(), string(workflow.CodeInvalidContinueSessionRole)) {
		t.Fatalf("CreateTask error = %v, want %s", err, workflow.CodeInvalidContinueSessionRole)
	}
}

func mutateSnapshotTransition(t *testing.T, snapshot *runStartSnapshot, transitionID string, mutate func(*transitionContractSnapshot)) {
	t.Helper()
	for index := range snapshot.TransitionGroups {
		if snapshot.TransitionGroups[index].TransitionID == transitionID {
			mutate(&snapshot.TransitionGroups[index])
			return
		}
	}
	t.Fatalf("snapshot transition %q missing from %+v", transitionID, snapshot.TransitionGroups)
}

func TestCompleteRunValidatesOutputRequirements(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := store.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if _, err := store.CompleteRun(ctx, CompleteRunRequest{RunID: started.RunID, TransitionID: "done", OutputValues: map[string]string{"summary": "  "}}); err == nil || !strings.Contains(err.Error(), "required output") {
		t.Fatalf("expected missing required output error, got %v", err)
	}
	transitions, err := store.ListTransitions(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListTransitions after rejected completion: %v", err)
	}
	if len(transitions) != 1 || transitions[0].TransitionID != "start" {
		t.Fatalf("rejected completion left partial transition rows: %+v", transitions)
	}
	runs, err := store.ListRuns(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListRuns after rejected completion: %v", err)
	}
	if len(runs) != 1 || runs[0].CompletedAt != 0 || runs[0].InterruptedAt != 0 {
		t.Fatalf("rejected completion mutated run outcome: %+v", runs)
	}
}

func TestCompleteRunInfersSingleTransitionID(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := store.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if _, err := store.CompleteRun(ctx, CompleteRunRequest{RunID: started.RunID, OutputValues: map[string]string{"summary": "done"}}); err != nil {
		t.Fatalf("CompleteRun inferred transition: %v", err)
	}
}

func TestCompleteRunRejectsMissingTransitionIDWhenAmbiguous(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createValidWorkflow(t, ctx, store)
	def, _, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	agent := nodeByKey(t, def, "agent")
	done := nodeByKind(t, def, workflow.NodeKindTerminal)
	if _, err := store.AddTransitionGroup(ctx, TransitionGroupRecord{ID: workflow.TransitionGroupID("group-blocked-" + string(workflowID)), WorkflowID: workflowID, SourceNodeID: agent.ID, TransitionID: "blocked", DisplayName: "Blocked"}); err != nil {
		t.Fatalf("AddTransitionGroup blocked: %v", err)
	}
	if _, err := store.AddEdge(ctx, EdgeRecord{ID: workflow.EdgeID("edge-blocked-" + string(workflowID)), WorkflowID: workflowID, TransitionGroupID: workflow.TransitionGroupID("group-blocked-" + string(workflowID)), Key: "blocked", TargetNodeID: done.ID, ContextMode: workflow.ContextModeNewSession}); err != nil {
		t.Fatalf("AddEdge blocked: %v", err)
	}
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := store.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if _, err := store.CompleteRun(ctx, CompleteRunRequest{RunID: started.RunID, OutputValues: map[string]string{"summary": "done"}}); err == nil || !strings.Contains(err.Error(), "transition id is required") {
		t.Fatalf("expected missing transition id error, got %v", err)
	}
}

func TestCompleteRunRejectsUnknownOutputField(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := store.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if _, err := store.CompleteRun(ctx, CompleteRunRequest{RunID: started.RunID, TransitionID: "done", OutputValues: map[string]string{"summary": "done", "extra": "nope"}}); err == nil || !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("expected unknown output error, got %v", err)
	}
}

func TestCompleteRunReturnsStructuredValidationIssues(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := store.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	_, err = store.CompleteRun(ctx, CompleteRunRequest{RunID: started.RunID, TransitionID: "done", OutputValues: map[string]string{"extra": "nope"}})
	validation, ok := err.(CompletionValidationError)
	if !ok {
		t.Fatalf("error = %T %v, want CompletionValidationError", err, err)
	}
	codes := map[string]bool{}
	for _, issue := range validation.Issues {
		codes[issue.Code] = true
	}
	if !codes["unknown_output_field"] || !codes["required_output_missing"] {
		t.Fatalf("validation codes = %+v, want unknown_output_field and required_output_missing", codes)
	}
}

func TestCompleteRunRejectsOversizedCompletionFields(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := store.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	_, err = store.CompleteRun(ctx, CompleteRunRequest{RunID: started.RunID, TransitionID: "done", OutputValues: map[string]string{"summary": strings.Repeat("a", workflow.MaxOutputValueBytes+1)}})
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected oversized output error, got %v", err)
	}
	_, err = store.CompleteRun(ctx, CompleteRunRequest{RunID: started.RunID, TransitionID: "done", Commentary: strings.Repeat("a", workflow.MaxCommentaryBytes+1), OutputValues: map[string]string{"summary": "done"}})
	if err == nil || !strings.Contains(err.Error(), "commentary is too large") {
		t.Fatalf("expected oversized commentary error, got %v", err)
	}
}

func TestRecordProtocolViolationInterruptsAtCap(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := store.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	first, err := store.RecordProtocolViolation(ctx, RecordProtocolViolationRequest{RunID: started.RunID, Kind: ProtocolViolationFinalAnswer, MaxCount: 2, Detail: `{"detail":"first"}`})
	if err != nil {
		t.Fatalf("RecordProtocolViolation first: %v", err)
	}
	if first.Count != 1 || first.Interrupted {
		t.Fatalf("first violation = %+v, want count 1 active", first)
	}
	second, err := store.RecordProtocolViolation(ctx, RecordProtocolViolationRequest{RunID: started.RunID, Kind: ProtocolViolationFinalAnswer, MaxCount: 2, Detail: `{"detail":"second"}`})
	if err != nil {
		t.Fatalf("RecordProtocolViolation second: %v", err)
	}
	if second.Count != 2 || !second.Interrupted {
		t.Fatalf("second violation = %+v, want count 2 interrupted", second)
	}
	runs, err := store.ListRuns(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].FinalAnswerViolations != 2 || runs[0].InterruptedAt == 0 || runs[0].InterruptionReason != "workflow_protocol_violation_limit" {
		t.Fatalf("run after cap = %+v", runs)
	}
}

func TestCompleteRunRejectsStaleGeneration(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := store.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	claimed, err := store.ClaimRun(ctx, started.RunID, 0)
	if err != nil {
		t.Fatalf("ClaimRun: %v", err)
	}
	if _, err := store.CompleteRun(ctx, CompleteRunRequest{RunID: started.RunID, TransitionID: "done", OutputValues: map[string]string{"summary": "done"}, ExpectedGeneration: 0, RequireGeneration: true}); err == nil || !strings.Contains(err.Error(), "stale workflow run generation") {
		t.Fatalf("expected stale generation error, got %v", err)
	}
	if _, err := store.CompleteRun(ctx, CompleteRunRequest{RunID: started.RunID, TransitionID: "done", OutputValues: map[string]string{"summary": "done"}, ExpectedGeneration: claimed.Generation, RequireGeneration: true}); err != nil {
		t.Fatalf("CompleteRun current generation: %v", err)
	}
}

func TestRunStartContextHandlesMissingInputEdgeAndMalformedJSON(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := store.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM task_transition_edges WHERE target_placement_id = ?`, string(started.PlacementID)); err != nil {
		t.Fatalf("delete transition edge snapshot: %v", err)
	}
	input, err := store.GetRunStartContext(ctx, started.RunID)
	if err != nil {
		t.Fatalf("GetRunStartContext without input edge: %v", err)
	}
	if len(input.InputValues) != 0 {
		t.Fatalf("input values without input edge = %+v, want empty", input.InputValues)
	}
	taskWithMalformedInputs, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task 2", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask malformed inputs: %v", err)
	}
	startedMalformedInputs, err := store.StartTask(ctx, taskWithMalformedInputs.ID)
	if err != nil {
		t.Fatalf("StartTask malformed inputs: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE task_transition_edges SET input_bindings_json = '{}' WHERE target_placement_id = ?`, string(startedMalformedInputs.PlacementID)); err != nil {
		t.Fatalf("corrupt transition edge inputs: %v", err)
	}
	if _, err := store.GetRunStartContext(ctx, startedMalformedInputs.RunID); err == nil {
		t.Fatalf("expected malformed transition edge input bindings error")
	}
	taskWithJoinInputs, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task 3", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask join inputs: %v", err)
	}
	startedJoinInputs, err := store.StartTask(ctx, taskWithJoinInputs.ID)
	if err != nil {
		t.Fatalf("StartTask join inputs: %v", err)
	}
	joinInputsJSON, err := marshalJSON([]workflow.InputBinding{{Name: "joined", Source: workflow.BindingSourceJoin, Field: "aggregate"}})
	if err != nil {
		t.Fatalf("marshal join inputs: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE task_transition_edges SET input_bindings_json = ? WHERE target_placement_id = ?`, joinInputsJSON, string(startedJoinInputs.PlacementID)); err != nil {
		t.Fatalf("set join transition edge inputs: %v", err)
	}
	if _, err := store.GetRunStartContext(ctx, startedJoinInputs.RunID); err == nil || !strings.Contains(err.Error(), "join-sourced input bindings cannot execute") {
		t.Fatalf("GetRunStartContext join inputs error = %v", err)
	}
}

func TestAttachRunSessionGenerationGuard(t *testing.T) {
	ctx := context.Background()
	store, binding, cfg := newTestStoreWithConfig(t)
	workflowID := createValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	sessionID := createTestSession(t, ctx, store, binding, cfg)
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := store.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	claimed, err := store.ClaimRun(ctx, started.RunID, 0)
	if err != nil {
		t.Fatalf("ClaimRun: %v", err)
	}
	if err := store.AttachRunSession(ctx, started.RunID, claimed.Generation-1, "session-stale"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("stale AttachRunSession error = %v, want sql.ErrNoRows", err)
	}
	if err := store.AttachRunSession(ctx, started.RunID, claimed.Generation, sessionID); err != nil {
		t.Fatalf("AttachRunSession current generation: %v", err)
	}
	if err := store.AttachRunSession(ctx, started.RunID, claimed.Generation, "session-second"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("second AttachRunSession error = %v, want sql.ErrNoRows", err)
	}
	runs, err := store.ListRuns(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if runs[0].SessionID != sessionID {
		t.Fatalf("attached session = %q, want %q", runs[0].SessionID, sessionID)
	}
}

func TestSetAndClearRunWaitingAskGenerationGuard(t *testing.T) {
	ctx := context.Background()
	store, binding, cfg := newTestStoreWithConfig(t)
	workflowID := createValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	sessionID := createTestSession(t, ctx, store, binding, cfg)
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := store.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	claimed, err := store.ClaimRun(ctx, started.RunID, 0)
	if err != nil {
		t.Fatalf("ClaimRun: %v", err)
	}
	if err := store.AttachRunSession(ctx, started.RunID, claimed.Generation, sessionID); err != nil {
		t.Fatalf("AttachRunSession: %v", err)
	}

	if err := store.SetRunWaitingAsk(ctx, started.RunID, claimed.Generation-1, "ask-stale"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("stale SetRunWaitingAsk error = %v, want sql.ErrNoRows", err)
	}
	if err := store.SetRunWaitingAsk(ctx, started.RunID, claimed.Generation, "ask-1"); err != nil {
		t.Fatalf("SetRunWaitingAsk current generation: %v", err)
	}
	waiting, err := store.ListWaitingAskRuns(ctx)
	if err != nil {
		t.Fatalf("ListWaitingAskRuns: %v", err)
	}
	if len(waiting) != 1 || waiting[0].ID != started.RunID || waiting[0].WaitingAskID != "ask-1" || waiting[0].SessionID != sessionID {
		t.Fatalf("waiting runs = %+v", waiting)
	}
	if _, err := store.ClaimRun(ctx, started.RunID, claimed.Generation); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("ClaimRun while waiting error = %v, want sql.ErrNoRows", err)
	}
	if err := store.ClearRunWaitingAsk(ctx, started.RunID, claimed.Generation-1, "ask-1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("stale ClearRunWaitingAsk error = %v, want sql.ErrNoRows", err)
	}
	if err := store.ClearRunWaitingAsk(ctx, started.RunID, claimed.Generation, "ask-other"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("wrong ask ClearRunWaitingAsk error = %v, want sql.ErrNoRows", err)
	}
	if err := store.ClearRunWaitingAsk(ctx, started.RunID, claimed.Generation, "ask-1"); err != nil {
		t.Fatalf("ClearRunWaitingAsk current ask: %v", err)
	}
	runs, err := store.ListRuns(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if runs[0].WaitingAskID != "" || runs[0].CompletedAt != 0 || runs[0].InterruptedAt != 0 {
		t.Fatalf("run after clear = %+v", runs[0])
	}
}

func TestInterruptRunGenerationGuard(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := store.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	claimed, err := store.ClaimRun(ctx, started.RunID, 0)
	if err != nil {
		t.Fatalf("ClaimRun: %v", err)
	}
	if err := store.InterruptRunGeneration(ctx, started.RunID, claimed.Generation-1, "stale", "{}"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("stale InterruptRunGeneration error = %v, want sql.ErrNoRows", err)
	}
	runs, err := store.ListRuns(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListRuns stale: %v", err)
	}
	if runs[0].InterruptedAt != 0 {
		t.Fatalf("stale generation interrupted run: %+v", runs[0])
	}
	if err := store.InterruptRunGeneration(ctx, started.RunID, claimed.Generation, "current", "{}"); err != nil {
		t.Fatalf("InterruptRunGeneration current generation: %v", err)
	}
	if err := store.InterruptRunGeneration(ctx, started.RunID, claimed.Generation, "second", "{}"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("second InterruptRunGeneration error = %v, want sql.ErrNoRows", err)
	}
	runs, err = store.ListRuns(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListRuns current: %v", err)
	}
	if runs[0].InterruptedAt == 0 || runs[0].InterruptionReason != "current" {
		t.Fatalf("run after interrupt = %+v, want current interruption", runs[0])
	}
}

func TestTaskStartRejectsCurrentInvalidWorkflow(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	def, _, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	done := nodeByKind(t, def, workflow.NodeKindTerminal)
	if _, err := store.AddTransitionGroup(ctx, TransitionGroupRecord{ID: "group-terminal-invalid", WorkflowID: workflowID, SourceNodeID: done.ID, TransitionID: "invalid", DisplayName: "Invalid"}); err != nil {
		t.Fatalf("AddTransitionGroup invalid terminal group: %v", err)
	}
	if _, err := store.StartTask(ctx, task.ID); err == nil || !strings.Contains(err.Error(), string(workflow.CodeTerminalHasOutgoingEdge)) {
		t.Fatalf("expected current workflow validation error, got %v", err)
	}
}

func TestTaskCreateRejectsInvalidOrUnlinkedWorkflow(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	invalid, err := store.CreateWorkflow(ctx, CreateWorkflowRequest{Name: "Invalid"})
	if err != nil {
		t.Fatalf("CreateWorkflow invalid: %v", err)
	}
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, invalid.ID, true); err != nil {
		t.Fatalf("LinkWorkflow invalid: %v", err)
	}
	if _, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"}); err == nil || !strings.Contains(err.Error(), "workflow validation failed") {
		t.Fatalf("expected invalid default workflow error, got %v", err)
	}
	valid := createValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, valid, false); err != nil {
		t.Fatalf("LinkWorkflow valid explicit: %v", err)
	}
	if task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, WorkflowID: valid, Title: "Explicit", Body: "Body"}); err != nil {
		t.Fatalf("CreateTask explicit valid workflow: %v", err)
	} else if !strings.HasPrefix(task.ShortID, "WOR-1") {
		t.Fatalf("explicit task short id = %q, want WOR-1", task.ShortID)
	}
	unlinked, err := store.CreateWorkflow(ctx, CreateWorkflowRequest{Name: "Unlinked"})
	if err != nil {
		t.Fatalf("CreateWorkflow unlinked: %v", err)
	}
	if _, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, WorkflowID: unlinked.ID, Title: "Task", Body: "Body"}); err == nil {
		t.Fatalf("expected unlinked workflow task creation to fail")
	}
}

func TestProjectWorkflowUnlinkGuardsActiveAndDefaultLinks(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createValidWorkflow(t, ctx, store)
	link, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true)
	if err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	otherWorkflowID := createValidWorkflow(t, ctx, store)
	otherLink, err := store.LinkWorkflow(ctx, binding.ProjectID, otherWorkflowID, false)
	if err != nil {
		t.Fatalf("LinkWorkflow other: %v", err)
	}
	spareWorkflowID := createValidWorkflow(t, ctx, store)
	spareLink, err := store.LinkWorkflow(ctx, binding.ProjectID, spareWorkflowID, false)
	if err != nil {
		t.Fatalf("LinkWorkflow spare: %v", err)
	}
	if err := store.UnlinkProjectWorkflow(ctx, link.ID, ""); err == nil || !strings.Contains(err.Error(), "replacement default") {
		t.Fatalf("expected replacement default guard, got %v", err)
	}
	if err := store.UnlinkProjectWorkflow(ctx, link.ID, "missing-link"); err == nil || !strings.Contains(err.Error(), "replacement default") {
		t.Fatalf("expected invalid replacement default guard, got %v", err)
	}
	links, err := store.ListProjectWorkflowLinks(ctx, binding.ProjectID)
	if err != nil {
		t.Fatalf("ListProjectWorkflowLinks after invalid replacement: %v", err)
	}
	if len(links) != 3 || !links[0].IsDefault {
		t.Fatalf("links after invalid replacement = %+v, want original default preserved", links)
	}
	if err := store.UnlinkProjectWorkflow(ctx, spareLink.ID, ""); err != nil {
		t.Fatalf("unlink unused non-default link should physically delete: %v", err)
	}
	if err := store.UnlinkProjectWorkflow(ctx, link.ID, otherLink.ID); err != nil {
		t.Fatalf("unlink default with valid replacement: %v", err)
	}
	links, err = store.ListProjectWorkflowLinks(ctx, binding.ProjectID)
	if err != nil {
		t.Fatalf("ListProjectWorkflowLinks after replacement: %v", err)
	}
	if len(links) != 1 || links[0].ID != otherLink.ID || !links[0].IsDefault {
		t.Fatalf("links after valid replacement = %+v, want replacement default", links)
	}
	link = otherLink
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := store.UnlinkProjectWorkflow(ctx, link.ID, ""); err == nil || !strings.Contains(err.Error(), "non-terminal") {
		t.Fatalf("expected non-terminal unlink guard, got %v", err)
	}
	if _, err := store.StartTask(ctx, task.ID); err != nil {
		t.Fatalf("StartTask: %v", err)
	}
}

func TestProjectWorkflowUnlinkSoftDisablesTerminalHistory(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createValidWorkflow(t, ctx, store)
	link, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true)
	if err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := store.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if _, err := store.CompleteRun(ctx, CompleteRunRequest{RunID: started.RunID, TransitionID: "done", OutputValues: map[string]string{"summary": "done"}}); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	if err := store.UnlinkProjectWorkflow(ctx, link.ID, ""); err != nil {
		t.Fatalf("UnlinkProjectWorkflow terminal history: %v", err)
	}
	links, err := store.ListProjectWorkflowLinks(ctx, binding.ProjectID)
	if err != nil {
		t.Fatalf("ListProjectWorkflowLinks: %v", err)
	}
	if len(links) != 1 || links[0].ID != link.ID || links[0].UnlinkedAtUnixMs == 0 || links[0].IsDefault {
		t.Fatalf("links after soft unlink = %+v", links)
	}
	if _, err := store.queries.GetTask(ctx, string(task.ID)); err != nil {
		t.Fatalf("task history should remain readable after soft unlink: %v", err)
	}
}

func TestGuardedGraphDeletesRespectTaskHistory(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := store.CreateTask(ctx, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := store.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	agentID := workflow.NodeID("node-agent-" + string(workflowID))
	if err := store.DeleteNode(ctx, agentID); err == nil || !strings.Contains(err.Error(), "non-terminal") {
		t.Fatalf("expected non-terminal node delete guard, got %v", err)
	}
	if err := store.DeleteEdge(ctx, workflow.EdgeID("edge-start-"+string(workflowID))); err == nil || !strings.Contains(err.Error(), "non-terminal") {
		t.Fatalf("expected non-terminal edge delete guard, got %v", err)
	}
	if _, err := store.CompleteRun(ctx, CompleteRunRequest{RunID: started.RunID, TransitionID: "done", OutputValues: map[string]string{"summary": "done"}}); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	if err := store.DeleteNode(ctx, agentID); err == nil || !strings.Contains(err.Error(), "task history") {
		t.Fatalf("expected node history delete guard, got %v", err)
	}
	if err := store.DeleteEdge(ctx, workflow.EdgeID("edge-done-"+string(workflowID))); err == nil || !strings.Contains(err.Error(), "task history") {
		t.Fatalf("expected edge history delete guard, got %v", err)
	}
	def, _, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	done := nodeByKind(t, def, workflow.NodeKindTerminal)
	if err := store.ArchiveNode(ctx, done.ID); err != nil {
		t.Fatalf("ArchiveNode terminal history: %v", err)
	}
	if err := store.DeleteNode(ctx, done.ID); err == nil || !strings.Contains(err.Error(), "task history") {
		t.Fatalf("expected terminal physical delete guard, got %v", err)
	}
	if _, err := store.AddNode(ctx, NodeRecord{ID: "node-unused", WorkflowID: workflowID, Key: "unused", Kind: workflow.NodeKindTerminal, DisplayName: "Unused"}); err != nil {
		t.Fatalf("AddNode unused: %v", err)
	}
	if err := store.DeleteNode(ctx, "node-unused"); err != nil {
		t.Fatalf("DeleteNode unused: %v", err)
	}
	if _, err := store.queries.GetWorkflowNode(ctx, "node-unused"); err == nil {
		t.Fatalf("unused node still exists after guarded delete")
	}
	if _, err := store.AddTransitionGroup(ctx, TransitionGroupRecord{ID: "group-unused", WorkflowID: workflowID, SourceNodeID: agentID, TransitionID: "unused", DisplayName: "Unused"}); err != nil {
		t.Fatalf("AddTransitionGroup unused: %v", err)
	}
	if _, err := store.AddEdge(ctx, EdgeRecord{ID: "edge-unused", WorkflowID: workflowID, TransitionGroupID: "group-unused", Key: "unused", TargetNodeID: done.ID, ContextMode: workflow.ContextModeNewSession}); err != nil {
		t.Fatalf("AddEdge unused: %v", err)
	}
	if err := store.DeleteEdge(ctx, "edge-unused"); err != nil {
		t.Fatalf("DeleteEdge unused: %v", err)
	}
	if _, err := store.queries.GetWorkflowEdge(ctx, "edge-unused"); err == nil {
		t.Fatalf("unused edge still exists after guarded delete")
	}
}

func newTestStore(t *testing.T) (*Store, metadata.Binding) {
	t.Helper()
	store, binding, _ := newTestStoreWithConfig(t)
	return store, binding
}

func newTestStoreWithConfig(t *testing.T) (*Store, metadata.Binding, config.App) {
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
	store, err := New(metadataStore, WithRoleResolver(workflow.StaticRoleResolver{"coder": true, "reviewer": true}))
	if err != nil {
		t.Fatalf("workflowstore.New: %v", err)
	}
	return store, binding, cfg
}

func createTestSession(t *testing.T, ctx context.Context, store *Store, binding metadata.Binding, cfg config.App) string {
	t.Helper()
	sessionRoot := config.ProjectSessionsRoot(cfg, binding.ProjectID)
	sessionStore, err := session.Create(sessionRoot, filepath.Base(cfg.WorkspaceRoot), cfg.WorkspaceRoot, store.metadata.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	if err := sessionStore.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}
	if _, err := store.metadata.ResolvePersistedSession(ctx, sessionStore.Meta().SessionID); err != nil {
		t.Fatalf("ResolvePersistedSession: %v", err)
	}
	return sessionStore.Meta().SessionID
}

func createValidWorkflow(t *testing.T, ctx context.Context, store *Store) workflow.WorkflowID {
	t.Helper()
	created, err := store.CreateWorkflow(ctx, CreateWorkflowRequest{Name: "Workflow"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	def, _, err := store.GetDefinition(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	start := nodeByKind(t, def, workflow.NodeKindStart)
	done := nodeByKind(t, def, workflow.NodeKindTerminal)
	if _, err := store.AddNode(ctx, NodeRecord{ID: workflow.NodeID("node-agent-" + string(created.ID)), WorkflowID: created.ID, Key: "agent", Kind: workflow.NodeKindAgent, DisplayName: "Agent", SubagentRole: "coder", PromptTemplate: "Do work.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	agentID := workflow.NodeID("node-agent-" + string(created.ID))
	if _, err := store.AddTransitionGroup(ctx, TransitionGroupRecord{ID: workflow.TransitionGroupID("group-start-" + string(created.ID)), WorkflowID: created.ID, SourceNodeID: start.ID, TransitionID: "start", DisplayName: "Start"}); err != nil {
		t.Fatalf("AddTransitionGroup start: %v", err)
	}
	if _, err := store.AddEdge(ctx, EdgeRecord{ID: workflow.EdgeID("edge-start-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: workflow.TransitionGroupID("group-start-" + string(created.ID)), Key: "start", TargetNodeID: agentID, ContextMode: workflow.ContextModeNewSession}); err != nil {
		t.Fatalf("AddEdge start: %v", err)
	}
	if _, err := store.AddTransitionGroup(ctx, TransitionGroupRecord{ID: workflow.TransitionGroupID("group-done-" + string(created.ID)), WorkflowID: created.ID, SourceNodeID: agentID, TransitionID: "done", DisplayName: "Done"}); err != nil {
		t.Fatalf("AddTransitionGroup done: %v", err)
	}
	if _, err := store.AddEdge(ctx, EdgeRecord{ID: workflow.EdgeID("edge-done-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: workflow.TransitionGroupID("group-done-" + string(created.ID)), Key: "done", TargetNodeID: done.ID, ContextMode: workflow.ContextModeNewSession, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}}); err != nil {
		t.Fatalf("AddEdge done: %v", err)
	}
	return created.ID
}

func createChainedContextModeWorkflow(t *testing.T, ctx context.Context, store *Store, contextMode workflow.ContextMode, targetRole string) workflow.WorkflowID {
	t.Helper()
	created, err := store.CreateWorkflow(ctx, CreateWorkflowRequest{Name: "Chained Context Workflow"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	def, _, err := store.GetDefinition(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	start := nodeByKind(t, def, workflow.NodeKindStart)
	done := nodeByKind(t, def, workflow.NodeKindTerminal)
	planID := workflow.NodeID("node-plan-" + string(created.ID))
	implID := workflow.NodeID("node-impl-" + string(created.ID))
	for _, node := range []NodeRecord{
		{ID: planID, WorkflowID: created.ID, Key: "plan", Kind: workflow.NodeKindAgent, DisplayName: "Plan", SubagentRole: "coder", PromptTemplate: "Plan work.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}},
		{ID: implID, WorkflowID: created.ID, Key: "implement", Kind: workflow.NodeKindAgent, DisplayName: "Implement", SubagentRole: targetRole, PromptTemplate: "Implement {{.Inputs.prior_summary}}.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}},
	} {
		if _, err := store.AddNode(ctx, node); err != nil {
			t.Fatalf("AddNode %s: %v", node.Key, err)
		}
	}
	startGroup := workflow.TransitionGroupID("group-start-" + string(created.ID))
	nextGroup := workflow.TransitionGroupID("group-next-" + string(created.ID))
	doneGroup := workflow.TransitionGroupID("group-done-" + string(created.ID))
	for _, group := range []TransitionGroupRecord{
		{ID: startGroup, WorkflowID: created.ID, SourceNodeID: start.ID, TransitionID: "start", DisplayName: "Start"},
		{ID: nextGroup, WorkflowID: created.ID, SourceNodeID: planID, TransitionID: "next", DisplayName: "Next"},
		{ID: doneGroup, WorkflowID: created.ID, SourceNodeID: implID, TransitionID: "done", DisplayName: "Done"},
	} {
		if _, err := store.AddTransitionGroup(ctx, group); err != nil {
			t.Fatalf("AddTransitionGroup %s: %v", group.TransitionID, err)
		}
	}
	for _, edge := range []EdgeRecord{
		{ID: workflow.EdgeID("edge-start-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: startGroup, Key: "start", TargetNodeID: planID, ContextMode: workflow.ContextModeNewSession},
		{ID: workflow.EdgeID("edge-next-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: nextGroup, Key: "next", TargetNodeID: implID, ContextMode: contextMode, InputBindings: []workflow.InputBinding{{Name: "prior_summary", Source: workflow.BindingSourceTransitionOutput, Field: "summary"}}, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}},
		{ID: workflow.EdgeID("edge-done-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: doneGroup, Key: "done", TargetNodeID: done.ID, ContextMode: workflow.ContextModeNewSession, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}},
	} {
		if _, err := store.AddEdge(ctx, edge); err != nil {
			t.Fatalf("AddEdge %s: %v", edge.Key, err)
		}
	}
	return created.ID
}

func createApprovalWorkflow(t *testing.T, ctx context.Context, store *Store) workflow.WorkflowID {
	t.Helper()
	created, err := store.CreateWorkflow(ctx, CreateWorkflowRequest{Name: "Approval Workflow"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	workflowID := created.ID
	def, _, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	start := nodeByKind(t, def, workflow.NodeKindStart)
	done := nodeByKind(t, def, workflow.NodeKindTerminal)
	agentID := workflow.NodeID("node-agent-" + string(workflowID))
	if _, err := store.AddNode(ctx, NodeRecord{ID: agentID, WorkflowID: workflowID, Key: "agent", Kind: workflow.NodeKindAgent, DisplayName: "Agent", SubagentRole: "coder", PromptTemplate: "Do work.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if _, err := store.AddTransitionGroup(ctx, TransitionGroupRecord{ID: workflow.TransitionGroupID("group-start-" + string(workflowID)), WorkflowID: workflowID, SourceNodeID: start.ID, TransitionID: "start", DisplayName: "Start"}); err != nil {
		t.Fatalf("AddTransitionGroup start: %v", err)
	}
	if _, err := store.AddEdge(ctx, EdgeRecord{ID: workflow.EdgeID("edge-start-" + string(workflowID)), WorkflowID: workflowID, TransitionGroupID: workflow.TransitionGroupID("group-start-" + string(workflowID)), Key: "start", TargetNodeID: agentID, ContextMode: workflow.ContextModeNewSession}); err != nil {
		t.Fatalf("AddEdge start: %v", err)
	}
	if _, err := store.AddTransitionGroup(ctx, TransitionGroupRecord{ID: workflow.TransitionGroupID("group-done-" + string(workflowID)), WorkflowID: workflowID, SourceNodeID: agentID, TransitionID: "done", DisplayName: "Done"}); err != nil {
		t.Fatalf("AddTransitionGroup done: %v", err)
	}
	if _, err := store.AddEdge(ctx, EdgeRecord{ID: workflow.EdgeID("edge-done-approval-" + string(workflowID)), WorkflowID: workflowID, TransitionGroupID: workflow.TransitionGroupID("group-done-" + string(workflowID)), Key: "done", TargetNodeID: done.ID, ContextMode: workflow.ContextModeNewSession, RequiresApproval: true, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}}); err != nil {
		t.Fatalf("AddEdge approval done: %v", err)
	}
	return workflowID
}

func currentWorkflowRevision(t *testing.T, ctx context.Context, store *Store, workflowID workflow.WorkflowID) int64 {
	t.Helper()
	_, record, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	return record.GraphRevision
}

func hasNode(def workflow.Definition, key string, kind workflow.NodeKind) bool {
	for _, node := range def.Nodes {
		if string(node.Key) == key && node.Kind == kind {
			return true
		}
	}
	return false
}

func nodeByKey(t *testing.T, def workflow.Definition, key string) workflow.Node {
	t.Helper()
	for _, node := range def.Nodes {
		if string(node.Key) == key {
			return node
		}
	}
	t.Fatalf("missing node key %q in %+v", key, def.Nodes)
	return workflow.Node{}
}

func nodeByKind(t *testing.T, def workflow.Definition, kind workflow.NodeKind) workflow.Node {
	t.Helper()
	for _, node := range def.Nodes {
		if node.Kind == kind {
			return node
		}
	}
	t.Fatalf("missing node kind %q in %+v", kind, def.Nodes)
	return workflow.Node{}
}
