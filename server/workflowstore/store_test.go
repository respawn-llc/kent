package workflowstore

import (
	"context"
	"strings"
	"testing"

	"builder/server/metadata"
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

func TestCompleteRunPersistsPendingApprovalSnapshots(t *testing.T) {
	ctx := context.Background()
	store, binding := newTestStore(t)
	workflowID := createApprovalWorkflow(t, ctx, store)
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
	if _, err := store.AddNode(ctx, NodeRecord{ID: "node-later", WorkflowID: workflowID, Key: "later", Kind: workflow.NodeKindTerminal, DisplayName: "Later"}); err != nil {
		t.Fatalf("AddNode graph edit: %v", err)
	}
	completed, err := store.CompleteRun(ctx, CompleteRunRequest{RunID: started.RunID, TransitionID: "done", OutputValues: map[string]string{"summary": "done"}})
	if err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	if completed.State != "pending_approval" || len(completed.PlacementIDs) != 0 || len(completed.RunIDs) != 0 {
		t.Fatalf("pending approval result = %+v", completed)
	}
	transitions, err := store.ListTransitions(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListTransitions: %v", err)
	}
	if len(transitions) != 2 || transitions[1].State != "pending_approval" {
		t.Fatalf("transitions after pending completion = %+v", transitions)
	}
	rows, err := store.queries.ListTaskTransitionEdges(ctx, string(transitions[1].ID))
	if err != nil {
		t.Fatalf("ListTaskTransitionEdges: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("pending edge rows = %+v", rows)
	}
	row := rows[0]
	if row.State != "pending" || row.TargetPlacementID.Valid || row.WorkflowRevisionSeen != beforeEdit || row.RequiresApproval != 1 || row.EdgeKey != "done" || row.TargetNodeKind != string(workflow.NodeKindTerminal) {
		t.Fatalf("pending approval edge snapshot = %+v, want stable pending approval snapshot at revision %d", row, beforeEdit)
	}
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
	if err := store.UnlinkProjectWorkflow(ctx, link.ID, ""); err == nil || !strings.Contains(err.Error(), "replacement default") {
		t.Fatalf("expected replacement default guard, got %v", err)
	}
	if err := store.UnlinkProjectWorkflow(ctx, otherLink.ID, ""); err != nil {
		t.Fatalf("unlink unused non-default link should physically delete: %v", err)
	}
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
	store, err := New(metadataStore, WithRoleResolver(workflow.StaticRoleResolver{"coder": true}))
	if err != nil {
		t.Fatalf("workflowstore.New: %v", err)
	}
	return store, binding
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
