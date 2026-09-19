package workflowrunner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"core/server/llm"
	"core/server/session"
	"core/server/workflow"
	"core/server/workflowexecution"
	"core/server/workflowruntime"
	"core/server/workflowstore"
	"core/shared/config"
	"core/shared/runtimeids"
)

func TestApprovalCloneStartupWaitsForSourcePostTurnCompaction(t *testing.T) {
	client := &heldLazyCompactionClient{
		compactingScriptedClient: NewCompactingScriptedClient(
			llm.ProviderCapabilities{ProviderID: "test", SupportsResponsesAPI: true, SupportsResponsesCompact: true},
			[]llm.CompactionResponse{
				workflowPostCompletionCompactionResponse("source"),
				workflowPostCompletionCompactionResponse("branch a"),
				workflowPostCompletionCompactionResponse("branch b"),
			},
			ScriptedFinalAnswer(`{"transition":"split","commentary":"source"}`),
			ScriptedCancellation(), ScriptedCancellation(),
		),
		started: make(chan context.Context, 3),
		release: make(chan struct{}),
	}
	f := newCurrentNodeRunnerFixtureWithClient(t, client)
	f.starter.cfg.Settings.CompactionMode = config.CompactionModeNative
	release := sync.OnceFunc(func() { close(client.release) })
	t.Cleanup(release)
	workflowID, _ := createCurrentNodeFanoutWorkflow(t, f.store, true, workflow.ContextModeCompactAndContinueSession)
	task := f.createTask(t, workflowID)
	f.startTask(t, task)
	select {
	case <-client.started:
	case <-time.After(currentNodeRunnerWait):
		t.Fatal("source did not enter post-turn compaction")
	}
	approval := f.waitForPendingApproval(t, task.ID)
	applied := make(chan error, 1)
	go func() {
		_, err := f.controller.ApplyPendingApproval(t.Context(), approval.ID)
		applied <- err
	}()
	crossed := false
	select {
	case <-client.started:
		crossed = true
	case err := <-applied:
		release()
		t.Fatalf("Approval startup completed before the source barrier: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	release()
	select {
	case err := <-applied:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(currentNodeRunnerWait):
		t.Fatal("Approval did not continue after the source barrier")
	}
	if crossed {
		t.Fatal("Approval cloned and compacted a target before source post-turn compaction finished")
	}
	f.waitForModelRequests(t, 3)
	f.waitForTaskQuiescence(t, task.ID)
	if calls := client.CompactionCalls(); len(calls) != 1 {
		t.Fatalf("clones did not reuse the source summary: compaction calls=%d", len(calls))
	}
}

func TestFanoutPreparationFailureLeavesSourceBeforeExplicitRetry(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseSource := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseSource)
	f := newCurrentNodeRunnerFixture(t,
		ScriptedRuntimeStep{
			BeforeResponse: func(ctx context.Context) error {
				close(entered)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return context.Cause(ctx)
				}
			},
			Response: ScriptedFinalAnswer(`{"transition":"split","commentary":"source"}`).Response,
		},
		ScriptedFinalAnswer(`{"transition":"split","commentary":"retry"}`),
		ScriptedCancellation(), ScriptedCancellation(),
	)
	workflowID, _ := createCurrentNodeFanoutContinuationWorkflow(t, f.store, false)
	task := f.createTask(t, workflowID)
	source := f.startTask(t, task)
	select {
	case <-entered:
	case <-time.After(currentNodeRunnerWait):
		t.Fatal("source did not reach provider execution")
	}
	f.mu.Lock()
	f.clientErr = errors.New("provider readiness failed before branch materialization")
	f.mu.Unlock()
	releaseSource()
	f.waitForCurrentNode(t, task.ID, func(nodes []workflow.CurrentNode) bool {
		return len(nodes) == 1 && nodes[0].Reference.Equal(source) &&
			nodes[0].Scheduling != nil && nodes[0].Scheduling.Interruption != nil
	})
	f.waitForTaskQuiescence(t, task.ID)
	sourceBinding, err := f.store.LatestTaskSessionForNode(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := f.store.CountTaskSessions(t.Context(), task.ID); err != nil || count != 1 {
		t.Fatalf("preparation published branch Sessions: count=%d error=%v", count, err)
	}
	f.mu.Lock()
	f.clientErr = nil
	f.mu.Unlock()
	_, err = f.controller.ResumeTask(t.Context(), task.ID, nil)
	if err != nil {
		t.Fatalf("explicit Resume after readiness failure: %v", err)
	}
	seen := map[runtimeids.SessionID]bool{sourceBinding.SessionID: true}
	branches := f.waitForCurrentNode(t, task.ID, func(nodes []workflow.CurrentNode) bool {
		if len(nodes) != 2 {
			return false
		}
		for _, node := range nodes {
			if node.SessionID == nil || node.Scheduling == nil || node.Scheduling.Interruption == nil {
				return false
			}
		}
		return true
	})
	for _, node := range branches {
		if node.SessionID == nil || seen[*node.SessionID] {
			t.Fatalf("fanout reused source or sibling Session: %+v", branches)
		}
		seen[*node.SessionID] = true
		if err := f.store.ValidateCurrentNodeSessionBinding(t.Context(), *node.SessionID, node.Reference); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("fanout bindings = %d; want source and two exact clones", len(seen))
	}
	f.waitForModelRequests(t, 4)
	f.waitForTaskQuiescence(t, task.ID)
}

func TestManualMoveDrainAndShutdownCanFinishTogether(t *testing.T) {
	requestStarted := make(chan struct{})
	requestStopping := make(chan struct{})
	releaseRequest := make(chan struct{})
	release := sync.OnceFunc(func() { close(releaseRequest) })
	f := newCurrentNodeRunnerFixture(t, ScriptedRuntimeStep{
		BeforeResponse: func(ctx context.Context) error {
			close(requestStarted)
			<-ctx.Done()
			close(requestStopping)
			<-releaseRequest
			return context.Cause(ctx)
		},
		Response: llm.Response{},
	})
	t.Cleanup(release)
	task := f.createTask(t, createCurrentNodeAgentWorkflow(t, f.store))
	f.startTask(t, task)
	select {
	case <-requestStarted:
	case <-time.After(currentNodeRunnerWait):
		t.Fatal("provider execution did not start")
	}
	shutdownObserved := make(chan struct{})
	moveDone := make(chan error, 1)
	go func() {
		moveDone <- f.controller.RunTaskOperation(context.Background(), func(ctx context.Context) error {
			stop := context.AfterFunc(ctx, func() { close(shutdownObserved) })
			defer stop()
			return f.controller.InterruptForManualMove(ctx, task.ID, nil)
		})
	}()
	select {
	case <-requestStopping:
	case <-time.After(currentNodeRunnerWait):
		t.Fatal("Manual Move did not stop the exact live execution")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- f.controller.Close() }()
	select {
	case <-shutdownObserved:
	case <-time.After(currentNodeRunnerWait):
		t.Fatal("shutdown did not reach the accepted operation")
	}
	release()
	select {
	case err := <-moveDone:
		if err != nil {
			t.Fatalf("drain accepted Manual Move: %v", err)
		}
	case <-time.After(currentNodeRunnerWait):
		t.Fatal("Manual Move and shutdown deadlocked while finalizing the exact execution")
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(currentNodeRunnerWait):
		t.Fatal("shutdown did not drain the accepted operation")
	}
}

func TestPreparationDoesNotPublishSessionBeforeCutover(t *testing.T) {
	f := newCurrentNodeRunnerFixture(t)
	ctx := context.Background()
	task := f.createTask(t, createCurrentNodeAgentWorkflow(t, f.store))
	plan, err := f.store.PlanTaskStart(ctx, task.ID, &workflowstore.ExecutionTargetCandidate{
		Snapshot: workflowstore.ExecutionTargetSnapshot{Mode: workflow.ExecutionTargetModeNone, Provenance: workflowstore.ExecutionTargetProvenanceResolved},
		Root:     workflowstore.ExecutionRoot{SourceWorkspaceID: f.workspaceID, SourceWorkspaceRoot: f.workspace},
	})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := f.starter.PrepareCurrentNode(ctx, plan.StartContexts()[0], workflowruntime.TaskPromptDeliveryAssignment)
	if err != nil {
		t.Fatal(err)
	}
	id := prepared.Session.SessionID
	if _, err := f.metadata.ResolvePersistedSession(ctx, id.String()); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("private Session lookup = %v; want absent", err)
	}
	dir := filepath.Join(f.cfg.PersistenceRoot, "projects", f.projectID, "sessions", id.String())
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private Session artifacts = %v; want absent", err)
	}
	nodes, err := f.store.ListCurrentNodes(ctx, task.ID)
	if err != nil || len(nodes) != 1 || nodes[0].Scheduling != nil {
		t.Fatalf("Task during preparation = %+v, %v; want Start", nodes, err)
	}
	started, err := f.store.CommitTaskStart(ctx, plan, []workflowstore.PlannedCurrentNodeSession{*prepared.Session})
	if err != nil {
		t.Fatal(err)
	}
	if len(started.Mutation.Created) != 1 || started.Mutation.Created[0].SessionID == nil || *started.Mutation.Created[0].SessionID != id {
		t.Fatalf("committed binding = %+v; want %s", started.Mutation.Created, id)
	}
	assignment := prepared.Assignment.(workflowexecution.CurrentNodeAssignmentPreparation)
	for range 2 {
		if err := assignment.Prepare(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if count := f.workflowAssignmentRecordCount(t, id); count != 1 {
		t.Fatalf("repeated startup assignments = %d; want one", count)
	}
	if requests := f.client.Requests(); len(requests) != 0 {
		t.Fatalf("provider requests before execution admission = %d; want none", len(requests))
	}
}
