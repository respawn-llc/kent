package workflowstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"core/server/metadata"
	"core/server/metadata/sqlitegen"
	"core/server/session"
	"core/server/workflow"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"
)

func TestTaskStartPlanKeepsBacklogUntilAtomicAdmission(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	path := filepath.Join(binding.CanonicalRoot, "complete.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '{}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	workflowID := createScriptStartWorkflow(t, ctx, store, "complete.sh")
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	fixture := scriptExecutionFixture{ctx: ctx, store: store, task: createDefaultTask(t, ctx, store, binding.ProjectID)}
	target, err := fixture.store.GetTaskExecutionTargetContext(fixture.ctx, fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := fixture.store.PlanTaskStart(fixture.ctx, fixture.task.ID, &ExecutionTargetCandidate{
		Snapshot: ExecutionTargetSnapshot{Mode: workflow.ExecutionTargetModeNone, Provenance: ExecutionTargetProvenanceResolved},
		Root:     ExecutionRoot{SourceWorkspaceID: target.SourceWorkspaceID, SourceWorkspaceRoot: target.SourceWorkspaceRoot},
	})
	if err != nil {
		t.Fatal(err)
	}
	contexts := plan.StartContexts()
	if len(contexts) != 1 || contexts[0].Node.Kind != workflow.NodeKindScript {
		t.Fatalf("planned targets: %+v", contexts)
	}
	before, err := fixture.store.ListCurrentNodes(fixture.ctx, fixture.task.ID)
	if err != nil || len(before) != 1 || before[0].Scheduling != nil {
		t.Fatalf("preparation changed Backlog: %+v, %v", before, err)
	}
	target, err = fixture.store.GetTaskExecutionTargetContext(fixture.ctx, fixture.task.ID)
	if err != nil || target.Task.ExecutionTarget != nil {
		t.Fatalf("preparation locked execution target: %+v, %v", target, err)
	}
	result, err := fixture.store.CommitTaskStart(fixture.ctx, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Mutation.Created) != 1 || result.Mutation.Created[0].Scheduling.State != workflow.CurrentNodeSchedulingAdmitted {
		t.Fatalf("cutover did not admit Script: %+v", result)
	}
	if result.Mutation.Created[0].SessionID != nil {
		t.Fatal("Script acquired a Session")
	}
	target, err = fixture.store.GetTaskExecutionTargetContext(fixture.ctx, fixture.task.ID)
	if err != nil || target.Task.ExecutionTarget == nil {
		t.Fatalf("cutover did not lock execution target: %+v, %v", target, err)
	}
}

func plannedSessionsForStoreTest(t *testing.T, ctx context.Context, store *Store, inputs []CurrentNodeStartContext) []PlannedCurrentNodeSession {
	sessions, _ := plannedSessionArtifactsForStoreTest(t, ctx, store, inputs)
	return sessions
}

func plannedSessionArtifactsForStoreTest(t *testing.T, ctx context.Context, store *Store, inputs []CurrentNodeStartContext) ([]PlannedCurrentNodeSession, []session.CreationPlan) {
	t.Helper()
	var sessions []PlannedCurrentNodeSession
	var creations []session.CreationPlan
	for _, input := range inputs {
		if input.Node.Kind != workflow.NodeKindAgent {
			continue
		}
		assignment := PlannedCurrentNodeSession{CurrentNode: input.CurrentNode.Reference}
		source := workflow.CanonicalContextSource(input.EnteringEdge.ContextSource)
		clone := input.IsFanoutBranch && input.ContextMode != workflow.ContextModeNewSession &&
			(source.Kind == workflow.ContextSourceImmediateSource || source.Kind == workflow.ContextSourceSelectedNode)
		if input.CurrentNode.SessionID != nil && !clone {
			assignment.SessionID = *input.CurrentNode.SessionID
		} else {
			id := runtimeids.NewSessionID()
			descriptor, err := session.NewCreateSessionDescriptor(
				id,
				filepath.Join(store.metadata.PersistenceRoot(), "projects", input.Task.ProjectID, "sessions"),
				"workspace",
				input.ExecutionRoot.SourceWorkspaceRoot,
				sessioncontract.SessionCategoryMain,
			)
			if err != nil {
				t.Fatal(err)
			}
			creation, err := session.PrepareCreation(session.CreationRequest{Descriptor: descriptor})
			if err != nil {
				t.Fatal(err)
			}
			target := metadata.SessionExecutionTargetUpdate{
				SessionID: id.String(), Workspace: &metadata.SessionExecutionTargetUpdateWorkspace{ID: input.ExecutionRoot.SourceWorkspaceID},
				CwdRelpath: ".",
			}
			if input.ExecutionRoot.Managed != nil {
				target.Worktree = &metadata.SessionExecutionTargetUpdateWorktree{ID: input.ExecutionRoot.Managed.WorktreeID}
			}
			snapshot, err := store.metadata.PrepareSessionSnapshot(ctx, creation.Snapshot(), target)
			if err != nil {
				t.Fatal(err)
			}
			assignment.SessionID = id
			assignment.Snapshot = &snapshot
			creations = append(creations, creation)
		}
		sessions = append(sessions, assignment)
	}
	return sessions, creations
}

func TestTaskStartCutoverPublishesExactSessionAndRetainedProvenance(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	createLinkedValidWorkflow(t, ctx, store, binding.ProjectID)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan, err := store.PlanTaskStart(ctx, task.ID, noneManualMoveExecutionTargetCandidate(binding))
	if err != nil {
		t.Fatal(err)
	}
	sessions := plannedSessionsForStoreTest(t, ctx, store, plan.StartContexts())
	id := sessions[0].SessionID
	if _, err := store.metadata.ResolvePersistedSession(ctx, id.String()); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("uncommitted Session was published: %v", err)
	}
	result, err := store.CommitTaskStart(ctx, plan, sessions)
	if err != nil {
		t.Fatal(err)
	}
	current := result.Mutation.Created[0]
	if current.SessionID == nil || *current.SessionID != id || current.Scheduling.State != workflow.CurrentNodeSchedulingAdmitted {
		t.Fatalf("cutover Current Node: %+v", current)
	}
	owner, err := store.TaskIDForSession(ctx, id)
	if err != nil || owner == nil || *owner != task.ID {
		t.Fatalf("Session ownership: %v, %v", owner, err)
	}
	association, err := store.LatestTaskSessionForNode(ctx, current.Reference)
	if err != nil || association.SessionID != id {
		t.Fatalf("retained provenance: %+v, %v", association, err)
	}
	record, err := store.metadata.ResolvePersistedSession(ctx, id.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(record.SessionDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cutover created Session artifacts: %v", err)
	}
}

func TestTaskStartCutoverRejectsMissingSessionWithoutLockingTarget(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	createLinkedValidWorkflow(t, ctx, store, binding.ProjectID)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan, err := store.PlanTaskStart(ctx, task.ID, noneManualMoveExecutionTargetCandidate(binding))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitTaskStart(ctx, plan, nil); err == nil {
		t.Fatal("Agent cutover accepted missing Session")
	}
	state, err := store.GetTaskExecutionTargetContext(ctx, task.ID)
	if err != nil || state.Task.ExecutionTarget != nil {
		t.Fatalf("failed cutover locked target: %+v, %v", state, err)
	}
	nodes, err := store.ListCurrentNodes(ctx, task.ID)
	if err != nil || len(nodes) != 1 || nodes[0].Scheduling != nil {
		t.Fatalf("failed cutover changed Backlog: %+v, %v", nodes, err)
	}
}

func TestTaskResumeCutoverPreservesExactSessionAndResolvesAttention(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	createLinkedValidWorkflow(t, ctx, store, binding.ProjectID)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	start, err := store.PlanTaskStart(ctx, task.ID, noneManualMoveExecutionTargetCandidate(binding))
	if err != nil {
		t.Fatal(err)
	}
	sessions := plannedSessionsForStoreTest(t, ctx, store, start.StartContexts())
	started, err := store.CommitTaskStart(ctx, start, sessions)
	if err != nil {
		t.Fatal(err)
	}
	reference := started.Mutation.Created[0].Reference
	if err := store.InterruptCurrentNode(ctx, reference, "backend_restarted", workflow.CurrentNodeInterruptionDetail{Code: "backend_restarted"}); err != nil {
		t.Fatal(err)
	}
	plan, err := store.PlanTaskResume(ctx, task.ID, []workflow.CurrentNodeReference{reference}, nil)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := store.ListCurrentNodes(ctx, task.ID)
	if err != nil || nodes[0].Scheduling.State != workflow.CurrentNodeSchedulingInterrupted {
		t.Fatalf("Resume preparation cleared interruption: %+v, %v", nodes, err)
	}
	resumed, err := store.CommitTaskResume(ctx, plan, plannedSessionsForStoreTest(t, ctx, store, plan.StartContexts()))
	if err != nil {
		t.Fatal(err)
	}
	if len(resumed.CurrentNodes) != 1 || resumed.CurrentNodes[0].Scheduling.State != workflow.CurrentNodeSchedulingAdmitted ||
		*resumed.CurrentNodes[0].SessionID != sessions[0].SessionID || len(resumed.InterruptedCurrentNodes) != 1 {
		t.Fatalf("Resume cutover: %+v", resumed)
	}
}

func TestTaskStartRevalidatesGraphBeforePublishingSession(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createLinkedValidWorkflow(t, ctx, store, binding.ProjectID)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan, err := store.PlanTaskStart(ctx, task.ID, noneManualMoveExecutionTargetCandidate(binding))
	if err != nil {
		t.Fatal(err)
	}
	sessions := plannedSessionsForStoreTest(t, ctx, store, plan.StartContexts())
	// A completed preparation must not reserve the Workflow against edits.
	definition, record, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatal(err)
	}
	request := NewWorkflowGraphSaveRequest(definition, record.Version)
	request.Edges[0].PromptTemplate = "Updated execution instructions."
	saved, err := store.SaveWorkflowGraph(ctx, request)
	if err != nil || !saved.Saved {
		t.Fatalf("Workflow edit during preparation: %+v, %v", saved, err)
	}
	if _, err := store.CommitTaskStart(ctx, plan, sessions); err == nil {
		t.Fatal("stale Workflow plan committed")
	}
	if _, err := store.metadata.ResolvePersistedSession(ctx, sessions[0].SessionID.String()); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("stale plan published Session: %v", err)
	}
	current, err := store.ListCurrentNodes(ctx, task.ID)
	if err != nil || len(current) != 1 || current[0].Scheduling != nil {
		t.Fatalf("stale plan changed Backlog: %+v, %v", current, err)
	}
}

func TestManualMovePreflightDoesNotRequireExecutionTarget(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createChainedContextModeWorkflow(t, ctx, store, workflow.ContextModeNewSession, "coder")
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	definition, _, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatal(err)
	}
	target := nodeByKey(t, definition, "plan")
	request := ManualMoveRequest{TaskID: task.ID, TargetNodeID: workflow.NodeIDOf(target)}
	prepared, err := store.PrepareManualMove(ctx, request)
	if err != nil || !prepared.RequiresExecutionTarget() {
		t.Fatalf("unlocked preflight: %+v, %v", prepared, err)
	}
	preview, err := store.PreviewManualMove(ctx, request)
	if err != nil || preview.Outcome != ManualMovePreviewOutcomeTransition {
		t.Fatalf("unlocked preview: %+v, %v", preview, err)
	}
}

func TestTaskStartRejectsSessionPreparedForDifferentExecutionRoot(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	createLinkedValidWorkflow(t, ctx, store, binding.ProjectID)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	plan, err := store.PlanTaskStart(ctx, task.ID, noneManualMoveExecutionTargetCandidate(binding))
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.metadata.AttachWorkspaceToProject(ctx, binding.ProjectID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	contexts := plan.StartContexts()
	contexts[0].ExecutionRoot = &ExecutionRoot{SourceWorkspaceID: other.WorkspaceID, SourceWorkspaceRoot: other.CanonicalRoot}
	sessions := plannedSessionsForStoreTest(t, ctx, store, contexts)
	if _, err := store.CommitTaskStart(ctx, plan, sessions); err == nil {
		t.Fatal("cutover accepted a Session from a different execution root")
	}
	if _, err := store.metadata.ResolvePersistedSession(ctx, sessions[0].SessionID.String()); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("wrong-root Session persisted: %v", err)
	}
}

func TestManualMoveFanoutCutoverRollsBackEverySessionOnInvalidLastBinding(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createFanoutJoinWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	source := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	definition, _, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatal(err)
	}
	target := nodeByKey(t, definition, "impl_a")
	transition := workflow.TransitionID("split")
	preparation, err := store.PrepareManualMove(ctx, ManualMoveRequest{
		TaskID: task.ID, TargetNodeID: workflow.NodeIDOf(target), TransitionKey: &transition,
		Values: map[workflow.ModelKey]map[string]string{"plan": {"summary": "branch input"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := store.PlanManualMove(ctx, preparation, nil)
	if err != nil {
		t.Fatal(err)
	}
	sessions := plannedSessionsForStoreTest(t, ctx, store, plan.StartContexts())
	if len(sessions) != 2 {
		t.Fatalf("expected two Agent targets: %+v", sessions)
	}
	invalid := append([]PlannedCurrentNodeSession(nil), sessions...)
	invalid[1].SessionID = runtimeids.NewSessionID()
	if _, err := store.CommitManualMove(ctx, plan, invalid); err == nil {
		t.Fatal("cutover accepted mismatched Session identity")
	}
	for _, planned := range sessions {
		if _, err := store.metadata.ResolvePersistedSession(ctx, planned.SessionID.String()); !errors.Is(err, session.ErrSessionNotFound) {
			t.Fatalf("partial cutover published Session %s: %v", planned.SessionID, err)
		}
	}
	current, err := store.ListCurrentNodes(ctx, task.ID)
	if err != nil || len(current) != 1 || !current[0].Reference.Equal(source.Reference) {
		t.Fatalf("partial cutover changed Task: %+v, %v", current, err)
	}
	targetContext, err := store.GetTaskExecutionTargetContext(ctx, task.ID)
	if err != nil || targetContext.Task.ExecutionTarget == nil || targetContext.Task.ExecutionTarget.Mode != workflow.ExecutionTargetModeNone {
		t.Fatalf("partial cutover changed target: %+v, %v", targetContext, err)
	}
	result, err := store.CommitManualMove(ctx, plan, sessions)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range result.Mutation.Created {
		if node.Scheduling.State != workflow.CurrentNodeSchedulingAdmitted || node.SessionID == nil {
			t.Fatalf("branch not admitted with exact Session: %+v", node)
		}
		association, err := store.LatestTaskSessionForNode(ctx, node.Reference)
		if err != nil || association.SessionID != *node.SessionID {
			t.Fatalf("branch provenance: %+v, %v", association, err)
		}
	}
	for _, node := range result.Mutation.Created {
		if err := store.InterruptCurrentNode(ctx, node.Reference, "backend_restarted", workflow.CurrentNodeInterruptionDetail{Code: "backend_restarted"}); err != nil {
			t.Fatal(err)
		}
	}
	selected := result.Mutation.Created[0]
	resume, err := store.PlanTaskResume(ctx, task.ID, []workflow.CurrentNodeReference{selected.Reference}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitTaskResume(ctx, resume, plannedSessionsForStoreTest(t, ctx, store, resume.StartContexts())); err != nil {
		t.Fatal(err)
	}
	current, err = store.ListCurrentNodes(ctx, task.ID)
	if err != nil || len(current) != 2 {
		t.Fatalf("subset Resume changed fanout: %+v, %v", current, err)
	}
	for _, node := range current {
		if node.Reference.Equal(selected.Reference) {
			if node.Scheduling.State != workflow.CurrentNodeSchedulingAdmitted || *node.SessionID != *selected.SessionID {
				t.Fatalf("selected branch Resume: %+v", node)
			}
		} else if node.Scheduling.State != workflow.CurrentNodeSchedulingInterrupted {
			t.Fatalf("Resume admitted unselected branch: %+v", node)
		}
	}
}

func TestTaskResumeRejectsReplacedSessionAfterPreparation(t *testing.T) {
	ctx, store, binding, cfg := newTestStoreWithConfigContext(t)
	createLinkedValidWorkflow(t, ctx, store, binding.ProjectID)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	start, err := store.PlanTaskStart(ctx, task.ID, noneManualMoveExecutionTargetCandidate(binding))
	if err != nil {
		t.Fatal(err)
	}
	sessions := plannedSessionsForStoreTest(t, ctx, store, start.StartContexts())
	started, err := store.CommitTaskStart(ctx, start, sessions)
	if err != nil {
		t.Fatal(err)
	}
	current := started.Mutation.Created[0]
	if err := store.InterruptCurrentNode(ctx, current.Reference, "backend_restarted", workflow.CurrentNodeInterruptionDetail{Code: "backend_restarted"}); err != nil {
		t.Fatal(err)
	}
	plan, err := store.PlanTaskResume(ctx, task.ID, []workflow.CurrentNodeReference{current.Reference}, nil)
	if err != nil {
		t.Fatal(err)
	}
	freshInputs := plan.StartContexts()
	freshInputs[0].CurrentNode.SessionID = nil
	fresh := plannedSessionsForStoreTest(t, ctx, store, freshInputs)
	if _, err := store.CommitTaskResume(ctx, plan, fresh); err == nil {
		t.Fatal("Resume replaced a retained Session with a new identity")
	}
	if _, err := store.metadata.ResolvePersistedSession(ctx, fresh[0].SessionID.String()); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("rejected fresh Resume Session was published: %v", err)
	}
	replacement, err := runtimeids.ParseSessionID(createTestSession(t, ctx, store, binding, cfg))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AssociateTaskSession(ctx, TaskSessionAssociationRequest{
		CurrentNode: current.Reference, SessionID: replacement, AssociatedAt: store.now(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE task_current_nodes SET session_id = ? WHERE task_id = ? AND node_id = ?`, replacement.String(), string(current.Reference.TaskID), string(current.Reference.NodeID)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitTaskResume(ctx, plan, plannedSessionsForStoreTest(t, ctx, store, plan.StartContexts())); err == nil {
		t.Fatal("stale Resume replaced the new Session binding")
	}
	nodes, err := store.ListCurrentNodes(ctx, task.ID)
	if err != nil || len(nodes) != 1 || *nodes[0].SessionID != replacement || nodes[0].Scheduling.State != workflow.CurrentNodeSchedulingInterrupted {
		t.Fatalf("stale Resume changed current Session: %+v, %v", nodes, err)
	}
}

func TestTaskStartRevalidatesRegisteredManagedRootBeforeCutover(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	createLinkedValidWorkflow(t, ctx, store, binding.ProjectID)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	worktree := metadata.WorktreeRecord{
		ID: runtimeids.NewGraphEntityID(), WorkspaceID: binding.WorkspaceID,
		CanonicalRoot: t.TempDir(), Managed: true,
	}
	if err := store.metadata.UpsertWorktreeRecord(ctx, worktree); err != nil {
		t.Fatal(err)
	}
	worktree, err := store.metadata.GetWorktreeRecordByID(ctx, worktree.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.queries.BindInitialTaskManagedWorktree(ctx, sqlitegen.BindInitialTaskManagedWorktreeParams{
		TaskID: string(task.ID), ManagedWorktreeID: nullableString(worktree.ID), UpdatedAtUnixMs: store.now().UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	ref, commit := "HEAD", "prepared-commit"
	candidate := &ExecutionTargetCandidate{
		Snapshot: ExecutionTargetSnapshot{Mode: workflow.ExecutionTargetModeHead, RequestedRef: &ref, CommitOID: &commit, Provenance: ExecutionTargetProvenanceResolved},
		Root: ExecutionRoot{
			SourceWorkspaceID: binding.WorkspaceID, SourceWorkspaceRoot: binding.CanonicalRoot,
			Managed: &ManagedExecutionRoot{WorktreeID: worktree.ID, Root: worktree.CanonicalRoot},
		},
	}
	plan, err := store.PlanTaskStart(ctx, task.ID, candidate)
	if err != nil {
		t.Fatal(err)
	}
	sessions := plannedSessionsForStoreTest(t, ctx, store, plan.StartContexts())
	worktree.CanonicalRoot = t.TempDir()
	if _, err := store.queries.UpdateWorktreeCanonicalRoot(ctx, sqlitegen.UpdateWorktreeCanonicalRootParams{
		ID: worktree.ID, CanonicalRootPath: worktree.CanonicalRoot, UpdatedAtUnixMs: store.now().UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitTaskStart(ctx, plan, sessions); err == nil {
		t.Fatal("cutover accepted a changed registered execution root")
	}
	if _, err := store.metadata.ResolvePersistedSession(ctx, sessions[0].SessionID.String()); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("stale-root cutover published Session: %v", err)
	}
	state, err := store.GetTaskExecutionTargetContext(ctx, task.ID)
	if err != nil || state.Task.ExecutionTarget != nil {
		t.Fatalf("stale-root cutover locked target: %+v, %v", state, err)
	}
}

func TestManualMoveFanoutCloneBecomesExactContinuationSource(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createFanoutJoinWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(_ workflow.Definition, request *WorkflowGraphSaveRequest) {
		for index := range request.Edges {
			if request.Edges[index].Key == "split_a" || request.Edges[index].Key == "split_b" {
				request.Edges[index].ContextMode = workflow.ContextModeContinueSession
			}
		}
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	current := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	sourceID := currentNodeSessionForStoreTest(t, ctx, store, current.Reference)
	definition, _, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatal(err)
	}
	transition := workflow.TransitionID("split")
	prepared, err := store.PrepareManualMove(ctx, ManualMoveRequest{
		TaskID: task.ID, TargetNodeID: workflow.NodeIDOf(nodeByKey(t, definition, "impl_a")), TransitionKey: &transition,
		Values: map[workflow.ModelKey]map[string]string{"plan": {"summary": "clone source"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := store.PlanManualMove(ctx, prepared, nil)
	if err != nil {
		t.Fatal(err)
	}
	inputs := plan.StartContexts()
	for index := range inputs {
		if inputs[index].CurrentNode.SessionID == nil || *inputs[index].CurrentNode.SessionID != sourceID {
			t.Fatalf("branch source not retained: %+v", inputs[index].CurrentNode)
		}
		inputs[index].CurrentNode.SessionID = nil
	}
	sessions := plannedSessionsForStoreTest(t, ctx, store, inputs)
	result, err := store.CommitManualMove(ctx, plan, sessions)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range result.Mutation.Created {
		id, exact := node.ContinuationSource.ExactSessionID()
		if !exact || node.SessionID == nil || id != *node.SessionID || id == sourceID {
			t.Fatalf("clone does not own its active continuation source: %+v", node)
		}
	}
}
