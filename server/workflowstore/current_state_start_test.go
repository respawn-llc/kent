package workflowstore

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"core/server/workflow"
)

func TestTaskStartPlacementFreezesSourceWorkspace(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	createLinkedValidWorkflow(t, ctx, store, binding.ProjectID)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	alternate, err := store.metadata.AttachWorkspaceToProject(ctx, binding.ProjectID, t.TempDir())
	if err != nil {
		t.Fatalf("AttachWorkspaceToProject: %v", err)
	}
	if _, err := seedStartedTask(t, ctx, store, task.ID); err != nil {
		t.Fatalf("StartTask: %v", err)
	}

	_, err = store.UpdateTask(ctx, UpdateTaskRequest{
		TaskID:            task.ID,
		SourceWorkspaceID: alternate.WorkspaceID,
	})
	if !errors.Is(err, ErrSourceWorkspaceAfterAutomation) {
		t.Fatalf("UpdateTask source workspace error = %v, want %v", err, ErrSourceWorkspaceAfterAutomation)
	}
}

func TestAdmitCurrentNodeRecordsAdmission(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createLinkedValidWorkflow(t, ctx, store, binding.ProjectID)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	started, err := seedStartedTask(t, ctx, store, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	nodes, err := store.ListCurrentNodes(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListCurrentNodes: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Scheduling == nil || nodes[0].Scheduling.State != workflow.CurrentNodeSchedulingAdmitted {
		t.Fatalf("current nodes = %+v, want one admitted node in workflow %q", nodes, workflowID)
	}
	if _, err := store.AdmitCurrentNode(ctx, started.Mutation.Created[0].Reference); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("second AdmitCurrentNode error = %v, want stale-ready absence", err)
	}
}

func TestReconcileTaskResumeOnlyChangesSelectedTaskAndPreservesApprovalSources(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	createLinkedValidWorkflow(t, ctx, store, binding.ProjectID)
	readyTask := createDefaultTask(t, ctx, store, binding.ProjectID)
	ready := startTask(t, ctx, store, readyTask.ID).Mutation.Created[0]
	admittedTask := createDefaultTask(t, ctx, store, binding.ProjectID)
	admitted := startTask(t, ctx, store, admittedTask.ID).Mutation.Created[0]

	approvalWorkflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	definition, _, err := store.GetDefinition(ctx, approvalWorkflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	reviewEdgeID := edgeByKey(t, definition, "review").ID
	saveWorkflowGraphFixture(t, ctx, store, approvalWorkflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		workflowGraphSaveEdgeRecord(t, req.Edges, reviewEdgeID).RequiresApproval = true
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, approvalWorkflowID, false)
	approvalTask := createTask(t, ctx, store, CreateTaskRequest{
		ProjectID:  binding.ProjectID,
		WorkflowID: &approvalWorkflowID,
		Title:      "Approval task",
		Body:       "Preserve pending Approval",
	})
	approvalSource := startTask(t, ctx, store, approvalTask.ID).Mutation.Created[0]
	completed, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       approvalSource.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "preserve approval"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode: %v", err)
	}
	if completed.PendingApproval == nil {
		t.Fatal("completion did not create pending Approval")
	}

	publisher := &recordingCurrentNodeEventPublisher{}
	store.SetWorkflowEventPublisher(publisher)
	if err := store.ReconcileTaskResume(ctx, admittedTask.ID); err != nil {
		t.Fatalf("ReconcileTaskResume: %v", err)
	}
	if len(publisher.events) != 1 || publisher.events[0].PrimaryEntityID != string(admittedTask.ID) {
		t.Fatalf("task changes after reconciliation = %+v, want selected task change", publisher.events)
	}
	if err := store.ReconcileTaskResume(ctx, admittedTask.ID); err != nil {
		t.Fatalf("repeat ReconcileTaskResume: %v", err)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("task changes after repeated reconciliation = %+v, want no duplicate change", publisher.events)
	}
	readyNodes, err := store.ListCurrentNodes(ctx, readyTask.ID)
	if err != nil {
		t.Fatalf("ListCurrentNodes unrelated task: %v", err)
	}
	if len(readyNodes) != 1 || readyNodes[0].Scheduling.State != ready.Scheduling.State {
		t.Fatalf("unrelated task nodes = %+v, want unchanged scheduling", readyNodes)
	}
	for _, expected := range []workflow.CurrentNodeReference{ready.Reference, admitted.Reference} {
		if err := store.ReconcileTaskResume(ctx, expected.TaskID); err != nil {
			t.Fatalf("ReconcileTaskResume: %v", err)
		}
		nodes, err := store.ListCurrentNodes(ctx, expected.TaskID)
		if err != nil {
			t.Fatalf("ListCurrentNodes(%q): %v", expected.TaskID, err)
		}
		if len(nodes) != 1 ||
			nodes[0].Scheduling == nil ||
			nodes[0].Scheduling.State != workflow.CurrentNodeSchedulingInterrupted ||
			nodes[0].Scheduling.Interruption == nil {
			t.Fatalf("current nodes for %q = %+v, want explicit resume interruption", expected.TaskID, nodes)
		}
	}
	if err := store.ReconcileTaskResume(ctx, approvalTask.ID); err != nil {
		t.Fatalf("ReconcileTaskResume approval task: %v", err)
	}
	approvalNodes, err := store.ListCurrentNodes(ctx, approvalTask.ID)
	if err != nil {
		t.Fatalf("ListCurrentNodes approval task: %v", err)
	}
	if len(approvalNodes) != 1 ||
		!approvalNodes[0].Reference.Equal(approvalSource.Reference) ||
		approvalNodes[0].Scheduling == nil ||
		approvalNodes[0].Scheduling.State != approvalSource.Scheduling.State {
		t.Fatalf("approval source after recovery = %+v, want unchanged source", approvalNodes)
	}
}

func TestCurrentNodeStartContextDerivesContinuationFromOutgoingEdges(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createValidWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		for index := range req.Edges {
			if req.Edges[index].TransitionGroupID == testTransitionGroupID("group-done-"+workflowID.String()) {
				req.Edges[index].ContextMode = workflow.ContextModeContinueSession
			}
		}
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	started := startTask(t, ctx, store, task.ID)

	input, err := store.ResolveCurrentNodeStartContext(ctx, started.Mutation.Created[0].Reference)
	if err != nil {
		t.Fatalf("ResolveCurrentNodeStartContext: %v", err)
	}
	if input.ContextMode != workflow.ContextModeNewSession {
		t.Fatalf("entering context mode = %q, want new_session", input.ContextMode)
	}
	if !input.HasContinueSessionOutgoingEdge {
		t.Fatal("current-node start context did not derive its continuation fact from outgoing edges")
	}
}

func TestResolveCurrentNodeStartContextAppliesPreviousTargetOrNewEffectiveMode(t *testing.T) {
	t.Run("missing prior target session starts new", func(t *testing.T) {
		fixture := newReworkContextCompletionFixture(t, workflow.ContextSourcePreviousTargetOrNew)
		removeRetainedSessionHistoryForTest(t, fixture.ctx, fixture.store, fixture.review.Reference)
		target := completeReworkCurrentNodeForStartContextTest(t, fixture)

		start, err := fixture.store.ResolveCurrentNodeStartContext(fixture.ctx, target.Reference)
		if err != nil {
			t.Fatalf("ResolveCurrentNodeStartContext: %v", err)
		}
		if start.EnteringEdge.ContextMode != workflow.ContextModeContinueSession ||
			workflow.CanonicalContextSource(start.EnteringEdge.ContextSource).Kind != workflow.ContextSourcePreviousTargetOrNew {
			t.Fatalf("configured entering context = %+v, want previous_target_or_new continuation", start.EnteringEdge)
		}
		if start.SourceSessionID == nil || target.SessionID == nil || *start.SourceSessionID != *target.SessionID || *target.SessionID == *fixture.review.SessionID {
			t.Fatalf("effective start context = mode %q session %v, want exact fresh target", start.ContextMode, start.SourceSessionID)
		}
	})

	t.Run("retained prior target session continues", func(t *testing.T) {
		fixture := newReworkContextCompletionFixture(t, workflow.ContextSourcePreviousTargetOrNew)
		sessionID := associateTaskSessionForTest(
			t,
			fixture.ctx,
			fixture.store,
			fixture.binding,
			fixture.cfg,
			fixture.review.Reference,
			time.UnixMilli(1_700_000_000_000).UTC(),
		)
		target := completeReworkCurrentNodeForStartContextTest(t, fixture)

		start, err := fixture.store.ResolveCurrentNodeStartContext(fixture.ctx, target.Reference)
		if err != nil {
			t.Fatalf("ResolveCurrentNodeStartContext: %v", err)
		}
		if start.ContextMode != workflow.ContextModeContinueSession ||
			start.SourceSessionID == nil ||
			*start.SourceSessionID != sessionID {
			t.Fatalf("effective start context = mode %q session %v, want continuation from %q", start.ContextMode, start.SourceSessionID, sessionID)
		}
	})

	t.Run("other continuation source still requires retained session", func(t *testing.T) {
		fixture := newReworkContextCompletionFixture(t, workflow.ContextSourcePreviousTargetOrNew)
		removeRetainedSessionHistoryForTest(t, fixture.ctx, fixture.store, fixture.review.Reference)
		saveWorkflowGraphFixture(t, fixture.ctx, fixture.store, fixture.workflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
			for index := range req.Edges {
				if req.Edges[index].Key == "rework" {
					req.Edges[index].ContextSource = workflow.ContextSource{Kind: workflow.ContextSourcePreviousTarget}
				}
			}
		})

		if _, err := fixture.store.PlanCurrentNodeCompletion(fixture.ctx, CurrentNodeCompletionRequest{
			Source: fixture.audit.Reference, TransitionID: "rework", OutputValues: map[string]string{"summary": "retry"},
		}); err == nil {
			t.Fatal("completion prepared continuation without a retained session")
		}
	})
}

func completeReworkCurrentNodeForStartContextTest(t *testing.T, fixture reworkContextCompletionFixture) workflow.CurrentNode {
	t.Helper()
	result, err := completeCurrentNode(t, fixture.store, fixture.ctx, CurrentNodeCompletionRequest{
		Source:       fixture.audit.Reference,
		TransitionID: "rework",
		OutputValues: map[string]string{"summary": "review again"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode audit: %v", err)
	}
	if len(result.Mutation.Created) != 1 {
		t.Fatalf("CompleteCurrentNode created = %+v, want one rework target", result.Mutation.Created)
	}
	return result.Mutation.Created[0]
}
