package workflowexecution

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/sessionruntime"
	"core/server/workflow"
	"core/server/workflowruntime"
	"core/shared/runtimeids"
)

func TestCurrentNodeControllerInterruptPersistsAfterCallerDeadline(t *testing.T) {
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh executable unavailable: %v", err)
	}
	queue := newControllerQueueFixture(t, 1)
	reference := queue.tasks[0].reference(t, 0)
	store := &currentNodeControllerStore{Store: queue.tasks[0].store, queueFixture: queue,
		interruptStarted: make(chan struct{}),
		interruptRelease: make(chan struct{}),
	}
	var controller *CurrentNodeController
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runner := &recordingScriptRunner{
		authority: authority,
		command: sessionruntime.ScriptCommand{
			Path: shellPath,
			Args: []string{"-c", "trap '' TERM; while :; do sleep 1; done"},
		},
		started: make(chan workflow.CurrentNodeReference, 1),
	}
	controller = newCurrentNodeControllerForTest(t, store, runner, authority, 1)
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	if err := queue.approve(context.Background(), controller, reference.TaskID); err != nil {
		t.Fatalf("start current node: %v", err)
	}
	<-runner.started
	waitForRunningCurrentNode(t, authority, reference)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- controller.Interrupt(ctx, InterruptSelector{TaskID: reference.TaskID})
	}()
	select {
	case <-store.interruptStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("interrupt did not begin its durable cleanup")
	}
	<-ctx.Done()
	close(store.interruptRelease)
	if err := <-result; err != nil {
		t.Fatalf("interrupt current node after caller deadline: %v", err)
	}
	if interruption, interrupted := store.interruption(reference); !interrupted || interruption.reason != workflow.CurrentNodeInterruptionReasonUserInterrupt {
		t.Fatalf("interruption = %+v, interrupted = %t, want durable user interruption", interruption, interrupted)
	}
	if hasLiveCurrentNode(authority, reference) {
		t.Fatal("interrupted current node remains live")
	}
}

func TestCurrentNodeControllerTaskInterruptPreservesSiblingPreparation(t *testing.T) {
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh executable unavailable: %v", err)
	}
	queue := newControllerQueueFixture(t, 2)
	running := queue.tasks[0].reference(t, 0)
	preparing := queue.tasks[0].reference(t, 1)
	store := &currentNodeControllerStore{Store: queue.tasks[0].store, queueFixture: queue}
	var controller *CurrentNodeController
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	preparationRelease := make(chan struct{})
	preparationStarted := make(chan struct{})
	runner := &preparingSiblingScriptRunner{
		preparing: preparing, entered: preparationStarted, release: preparationRelease,
		recordingScriptRunner: recordingScriptRunner{
			authority: authority,
			command: sessionruntime.ScriptCommand{
				Path: shellPath,
				Args: []string{"-c", "while :; do sleep 1; done"},
			},
			started: make(chan workflow.CurrentNodeReference, 2),
		},
	}
	controller = newCurrentNodeControllerForTest(t, store, runner, authority, 2)
	t.Cleanup(func() {
		select {
		case <-preparationRelease:
		default:
			close(preparationRelease)
		}
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	if err := queue.approve(context.Background(), controller, running.TaskID); err != nil {
		t.Fatalf("start running Current Node: %v", err)
	}
	select {
	case started := <-runner.started:
		if !started.Equal(running) {
			t.Fatalf("started Current Node = %v, want %v", started, running)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("running Current Node did not start")
	}
	waitForRunningCurrentNode(t, authority, running)

	select {
	case <-preparationStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("sibling preparation did not start")
	}
	interruptDone := make(chan error, 1)
	go func() {
		interruptDone <- controller.Interrupt(context.Background(), InterruptSelector{TaskID: running.TaskID})
	}()
	select {
	case err := <-interruptDone:
		if err != nil {
			t.Fatalf("interrupt running sibling: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Task Interrupt waited for non-selected sibling preparation")
	}
	if interruption, interrupted := store.interruption(preparing); interrupted {
		t.Fatalf("preparing sibling was interrupted: %+v", interruption)
	}

	close(preparationRelease)
	select {
	case started := <-runner.started:
		if !started.Equal(preparing) {
			t.Fatalf("started Current Node = %v, want preserved sibling %v", started, preparing)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("preserved sibling did not start after Task Interrupt")
	}
}

func TestCurrentNodeControllerTaskInterruptDoesNotCoordinateFinalizingSibling(t *testing.T) {
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh executable unavailable: %v", err)
	}
	queue := newControllerQueueFixture(t, 2)
	running := queue.tasks[0].reference(t, 0)
	finalizing := queue.tasks[0].reference(t, 1)
	store := &currentNodeControllerStore{Store: queue.tasks[0].store, queueFixture: queue,
		interruptStarted: make(chan struct{}),
		interruptRelease: make(chan struct{}),
	}
	var controller *CurrentNodeController
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runner := &runningAndFinalizingScriptRunner{
		authority:           authority,
		shellPath:           shellPath,
		running:             running,
		finalizing:          finalizing,
		finalizerEntered:    make(chan struct{}),
		releaseFinalizer:    make(chan struct{}),
		finalizerCompletion: make(chan error, 1),
		successorStarted:    make(chan struct{}, 1),
	}
	controller = newCurrentNodeControllerForTest(t, store, runner, authority, 1)
	var releaseFinalizerOnce sync.Once
	releaseFinalizer := func() {
		releaseFinalizerOnce.Do(func() {
			close(runner.releaseFinalizer)
		})
	}
	var releaseInterruptOnce sync.Once
	releaseInterrupt := func() {
		releaseInterruptOnce.Do(func() {
			close(store.interruptRelease)
		})
	}
	t.Cleanup(func() {
		releaseFinalizer()
		releaseInterrupt()
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	if err := queue.approve(context.Background(), controller, queue.tasks[0].task.ID); err != nil {
		t.Fatalf("approve parallel branches: %v", err)
	}
	select {
	case <-runner.finalizerEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("sibling Script did not enter completion finalization")
	}
	waitForRunningCurrentNode(t, authority, running)

	interruptDone := make(chan error, 1)
	go func() {
		interruptDone <- controller.Interrupt(context.Background(), InterruptSelector{TaskID: running.TaskID})
	}()
	select {
	case <-store.interruptStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("Task Interrupt did not begin durable cleanup")
	}
	testsetup.RequireUntil(t, time.Now().Add(3*time.Second), 10*time.Millisecond, func() bool {
		return !hasLiveCurrentNode(authority, running)
	}, "running sibling did not retire while interrupt persistence was blocked")

	releaseFinalizer()
	releaseInterrupt()
	select {
	case completionErr := <-runner.finalizerCompletion:
		if completionErr != nil {
			t.Fatalf("finalizing sibling completion: %v", completionErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("finalizing sibling completion did not resolve through the Task fence")
	}
	select {
	case err := <-interruptDone:
		if err != nil {
			t.Fatalf("Task Interrupt: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Task Interrupt did not complete")
	}
	if calls := store.completionCount(); calls != 1 {
		t.Fatalf("finalizing sibling durable completions = %d, want 1", calls)
	}
}

func TestCurrentNodeControllerTaskInterruptDoesNotOverrideFinalizingScopeFailure(t *testing.T) {
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh executable unavailable: %v", err)
	}
	queue := newControllerQueueFixture(t, 2)
	running := queue.tasks[0].reference(t, 0)
	finalizing := queue.tasks[0].reference(t, 1)
	store := &currentNodeControllerStore{Store: queue.tasks[0].store, queueFixture: queue,
		interruptStarted: make(chan struct{}),
		interruptRelease: make(chan struct{}),
	}
	var controller *CurrentNodeController
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runningRetirement := make(chan struct{})
	var retireOnce sync.Once
	retireRunning := func() { retireOnce.Do(func() { close(runningRetirement) }) }
	runner := &runningAndFinalizingScriptRunner{
		authority:           authority,
		shellPath:           shellPath,
		running:             running,
		finalizing:          finalizing,
		finalizerEntered:    make(chan struct{}),
		releaseFinalizer:    make(chan struct{}),
		finalizerCompletion: make(chan error, 1),
		successorStarted:    make(chan struct{}, 1),
		runningRetirement:   runningRetirement,
		finalize: func(ctx context.Context, scope sessionruntime.ExecutionScope, controller *CurrentNodeController) error {
			return controller.FailCurrentNodeScope(ctx, scope.ID(), "workflow_script_failed", errors.New("script failed"))
		},
	}
	controller = newCurrentNodeControllerForTest(t, store, runner, authority, 1)
	var releaseFinalizerOnce sync.Once
	releaseFinalizer := func() {
		releaseFinalizerOnce.Do(func() {
			close(runner.releaseFinalizer)
		})
	}
	var releaseInterruptOnce sync.Once
	releaseInterrupt := func() {
		releaseInterruptOnce.Do(func() {
			close(store.interruptRelease)
		})
	}
	t.Cleanup(func() {
		releaseFinalizer()
		releaseInterrupt()
		retireRunning()
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	if err := queue.approve(context.Background(), controller, queue.tasks[0].task.ID); err != nil {
		t.Fatalf("approve parallel branches: %v", err)
	}
	select {
	case <-runner.finalizerEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("sibling Script did not enter failure finalization")
	}
	waitForRunningCurrentNode(t, authority, running)

	interruptDone := make(chan error, 1)
	go func() {
		interruptDone <- controller.Interrupt(context.Background(), InterruptSelector{TaskID: running.TaskID})
	}()
	select {
	case <-store.interruptStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("Task Interrupt did not begin")
	}
	releaseFinalizer()
	releaseInterrupt()
	select {
	case finalizerErr := <-runner.finalizerCompletion:
		if finalizerErr != nil {
			t.Fatalf("unselected finalizing scope failure: %v", finalizerErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("finalizing scope failure did not resolve")
	}
	retireRunning()
	select {
	case err := <-interruptDone:
		if err != nil {
			t.Fatalf("Task Interrupt: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Task Interrupt did not complete")
	}
	nodes, err := store.Store.ListCurrentNodes(t.Context(), running.TaskID)
	if err != nil || len(nodes) != 2 {
		t.Fatalf("interrupted Task placement: %+v, %v", nodes, err)
	}
	for _, node := range nodes {
		want := workflow.CurrentNodeInterruptionReasonUserInterrupt
		if node.Reference.Equal(finalizing) {
			want = "workflow_script_failed"
		} else if !node.Reference.Equal(running) {
			t.Fatalf("unexpected Current Node: %+v", node)
		}
		if node.Scheduling == nil || node.Scheduling.State != workflow.CurrentNodeSchedulingInterrupted ||
			node.Scheduling.Interruption == nil || node.Scheduling.Interruption.Reason != want {
			t.Fatalf("Current Node %v lost its own interruption reason %q: %+v", node.Reference, want, node.Scheduling)
		}
	}
}

func TestCurrentNodeControllerScopeFailurePersistsDespiteUnrelatedWorkerError(t *testing.T) {
	reference := currentNodeReferenceForControllerTest(t, "task-scope-failure-worker-error", "node-script")
	store := &currentNodeControllerStore{}
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	controller := newCurrentNodeControllerForTest(t, store, &countingCurrentNodeRunner{}, authority, 1)
	t.Cleanup(func() {
		controller.mu.Lock()
		controller.workerErr = nil
		controller.mu.Unlock()
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh executable unavailable: %v", err)
	}
	handle := startLiveTestWorkflowScript(t, controller, authority, reference, sessionruntime.ScriptExecutionRequest{
		Command: sessionruntime.ScriptCommand{
			Path: shellPath,
			Args: []string{"-c", "trap 'exit 0' TERM; while :; do sleep 1; done"},
		},
	})
	scopeID := handle.Scope().ID()
	controller.mu.Lock()
	controller.workerErr = errors.New("unrelated admission persistence failed")
	controller.mu.Unlock()

	if err := controller.FailCurrentNodeScope(
		context.Background(),
		scopeID,
		"workflow_script_failed",
		errors.New("script failed"),
	); err != nil {
		t.Fatalf("FailCurrentNodeScope: %v", err)
	}
	interruption, interrupted := store.interruption(reference)
	if !interrupted || interruption.reason != "workflow_script_failed" {
		t.Fatalf("scope failure interruption = %+v, interrupted = %t", interruption, interrupted)
	}
}

func TestCurrentNodeControllerExplicitResumeReconcilesStoppedTaskWithoutNotification(t *testing.T) {
	reference := currentNodeReferenceForControllerTest(t, "task-resume", "node-1")
	store := &currentNodeControllerStore{currentNodes: []workflow.CurrentNode{
		{Reference: reference, Scheduling: &workflow.CurrentNodeScheduling{State: workflow.CurrentNodeSchedulingAdmitted}},
	}}
	attention := &currentNodeAttentionRecorder{}
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runner := &countingCurrentNodeRunner{}
	controller := newCurrentNodeControllerWithAttentionForTest(t, store, runner, authority, 1, attention)
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	preflight, err := controller.PreflightTaskResume(context.Background(), reference.TaskID)
	if err != nil {
		t.Fatalf("resume preflight: %v", err)
	}
	if preflight.Outcome != TaskResumePreflightResumable || len(preflight.CurrentNodes) != 1 {
		t.Fatalf("resume preflight = %+v, want resumable node", preflight)
	}
	if runner.starts() != 0 {
		t.Fatalf("preflight started %d current nodes, want no automatic start", runner.starts())
	}
	if attention.pendingCount() != 0 {
		t.Fatalf("resume preflight attention notifications = %d, want none", attention.pendingCount())
	}
}

func TestCurrentNodeControllerTaskInterruptDrainsReservationOnlyAlongsideLiveScope(t *testing.T) {
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh executable unavailable: %v", err)
	}
	queue := newControllerQueueFixture(t, 2)
	queue.tasks[0].moveBranches(t, queue.tasks[0].branchPlan(t))
	live := queue.tasks[0].reference(t, 0)
	reserved := queue.tasks[0].reference(t, 1)
	store := &currentNodeControllerStore{Store: queue.tasks[0].store, queueFixture: queue}
	var controller *CurrentNodeController
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runner := &recordingScriptRunner{
		authority: authority,
		command: sessionruntime.ScriptCommand{
			Path: shellPath,
			Args: []string{"-c", "trap 'exit 0' TERM; while :; do sleep 1; done"},
		},
		started: make(chan workflow.CurrentNodeReference, 1),
	}
	controller = newCurrentNodeControllerForTest(t, store, runner, authority, 1)
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	startLiveTestWorkflowScript(t, controller, authority, live, sessionruntime.ScriptExecutionRequest{Command: runner.command})
	waitForRunningCurrentNode(t, authority, live)
	reservedKey, err := reserved.Key()
	if err != nil {
		t.Fatalf("reserved key: %v", err)
	}
	controller.mu.Lock()
	controller.agentCapacityActive = 1
	controller.automaticReservations[reservedKey] = currentNodeQueuedStart{
		reference:  reserved,
		policy:     currentNodeAdmissionAutomaticAgent,
		completion: newCurrentNodeAdmissionCompletion(),
		agentCapacityLease: &currentNodeAgentCapacityLease{
			owner: currentNodeAgentCapacityReservation,
		},
	}
	controller.mu.Unlock()

	if err := controller.Interrupt(context.Background(), InterruptSelector{TaskID: live.TaskID}); err != nil {
		t.Fatalf("task interrupt: %v", err)
	}
	if _, interrupted := store.interruption(reserved); !interrupted {
		t.Fatal("task interrupt did not persist the drained reservation interruption")
	}
	if err := controller.EnsureTaskQuiescent(live.TaskID); err != nil {
		t.Fatalf("task remains non-quiescent after interrupt: %v", err)
	}
	controller.mu.Lock()
	if controller.agentCapacityActive != 0 {
		controller.mu.Unlock()
		t.Fatalf("Agent capacity after draining reservation = %d, want 0", controller.agentCapacityActive)
	}
	controller.mu.Unlock()
}

func TestCurrentNodeControllerInterruptingScriptDoesNotReleaseAgentCapacity(t *testing.T) {
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh executable unavailable: %v", err)
	}
	queue := newControllerQueueFixture(t, 1, 1, 1)
	occupyingAgent := queue.tasks[0].reference(t, 0)
	script := queue.tasks[1].reference(t, 0)
	queuedAgent := queue.tasks[2].reference(t, 0)
	store := &currentNodeControllerStore{Store: queue.tasks[0].store, queueFixture: queue}
	var controller *CurrentNodeController
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runner := &recordingScriptRunner{
		authority: authority,
		command: sessionruntime.ScriptCommand{
			Path: shellPath,
			Args: []string{"-c", "trap 'exit 0' TERM; while :; do sleep 1; done"},
		},
		started: make(chan workflow.CurrentNodeReference, 3),
		scripts: map[workflow.CurrentNodeReference]struct{}{
			script: {},
		},
	}
	controller = newCurrentNodeControllerForTest(t, store, runner, authority, 1)
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	queue.automaticIntents(controller, []CurrentNodeAutomaticIntent{{
		CurrentNode: occupyingAgent,
		NodeKind:    workflow.NodeKindAgent,
	}})
	select {
	case started := <-runner.started:
		if !started.Equal(occupyingAgent) {
			t.Fatalf("occupying Agent start = %v, want %v", started, occupyingAgent)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("occupying Agent did not start")
	}
	waitForRunningCurrentNode(t, authority, occupyingAgent)
	queue.automaticIntents(controller, []CurrentNodeAutomaticIntent{
		{CurrentNode: script, NodeKind: workflow.NodeKindScript},
		{CurrentNode: queuedAgent, NodeKind: workflow.NodeKindAgent},
	})
	select {
	case started := <-runner.started:
		if !started.Equal(script) {
			t.Fatalf("Script start = %v, want %v", started, script)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Script did not start while Agent capacity was occupied")
	}
	waitForRunningCurrentNode(t, authority, script)
	if err := controller.Interrupt(context.Background(), InterruptSelector{TaskID: script.TaskID}); err != nil {
		t.Fatalf("interrupt Script: %v", err)
	}
	if hasLiveCurrentNode(authority, script) {
		t.Fatal("interrupted Script remains live")
	}
	occupyingHandle, live := authority.ExecutionByScope(singleLiveScope(t, authority, occupyingAgent))
	if !live {
		t.Fatal("occupying Agent is not live")
	}
	if err := occupyingHandle.Stop(context.Background()); err != nil {
		t.Fatalf("stop occupying Agent: %v", err)
	}
	select {
	case started := <-runner.started:
		if !started.Equal(queuedAgent) {
			t.Fatalf("queued Agent start = %v, want %v", started, queuedAgent)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued Agent did not start after occupying Agent stopped")
	}
}

func TestCurrentNodeControllerTaskInterruptDrainsConcurrencyQueuedWorkAlongsideRunningScope(t *testing.T) {
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh executable unavailable: %v", err)
	}
	queue := newControllerQueueFixture(t, 2)
	running := queue.tasks[0].reference(t, 0)
	queued := queue.tasks[0].reference(t, 1)
	store := &currentNodeControllerStore{Store: queue.tasks[0].store, queueFixture: queue,
		interruptStarted: make(chan struct{}),
		interruptRelease: make(chan struct{}),
	}
	var controller *CurrentNodeController
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runner := &runningAndQueuedGateRunner{
		authority:      authority,
		shellPath:      shellPath,
		running:        running,
		runningStarted: make(chan struct{}),
	}
	controller = newCurrentNodeControllerForTest(t, store, runner, authority, 1)
	t.Cleanup(func() {
		select {
		case <-store.interruptRelease:
		default:
			close(store.interruptRelease)
		}
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	queue.automaticIntents(controller, []CurrentNodeAutomaticIntent{
		{CurrentNode: running, NodeKind: workflow.NodeKindAgent},
		{CurrentNode: queued, NodeKind: workflow.NodeKindAgent},
	})
	select {
	case <-runner.runningStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("running scope did not start")
	}
	waitForRunningCurrentNode(t, authority, running)
	testsetup.RequireUntil(t, time.Now().Add(3*time.Second), 10*time.Millisecond, func() bool {
		observation, observationErr := controller.ObserveWorkflowTaskExecutions(nil)
		return observationErr == nil && len(observation.ConcurrencyQueued[running.TaskID]) == 1 &&
			observation.ConcurrencyQueued[running.TaskID][0].Equal(queued)
	}, "queued sibling did not enter controller-owned concurrency queue")

	interruptDone := make(chan error, 1)
	go func() {
		interruptDone <- controller.Interrupt(context.Background(), InterruptSelector{TaskID: running.TaskID})
	}()
	select {
	case <-store.interruptStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("Task Interrupt did not drain controller-owned queued work")
	}
	close(store.interruptRelease)
	select {
	case err := <-interruptDone:
		if err != nil {
			t.Fatalf("Task Interrupt: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Task Interrupt did not finish after queued work retired")
	}
	for _, reference := range []workflow.CurrentNodeReference{running, queued} {
		interruption, interrupted := store.interruption(reference)
		if !interrupted {
			t.Fatalf("current node %v was not durably interrupted", reference)
		}
		if interruption.reason != workflow.CurrentNodeInterruptionReasonUserInterrupt {
			t.Fatalf("current node %v interruption reason = %q, want user interrupt", reference, interruption.reason)
		}
	}
}

func TestCurrentNodeControllerReservationDoesNotAuthorizeTaskInterrupt(t *testing.T) {
	reference := currentNodeReferenceForControllerTest(t, "task-reservation-no-live", "node-agent")
	store := &currentNodeControllerStore{}
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	controller := newCurrentNodeControllerForTest(t, store, &countingCurrentNodeRunner{}, authority, 1)
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	key, err := reference.Key()
	if err != nil {
		t.Fatalf("reference key: %v", err)
	}
	controller.mu.Lock()
	controller.automaticReservations[key] = currentNodeQueuedStart{reference: reference, policy: currentNodeAdmissionAutomaticAgent}
	controller.mu.Unlock()

	if err := controller.Interrupt(context.Background(), InterruptSelector{TaskID: reference.TaskID}); !errors.Is(err, ErrNoInterruptibleExecution) {
		t.Fatalf("reservation-only task interrupt error = %v, want %v", err, ErrNoInterruptibleExecution)
	}
	if err := controller.EnsureTaskQuiescent(reference.TaskID); !errors.Is(err, ErrTaskExecutionNotQuiescent) {
		t.Fatalf("reservation-only task quiescence = %v, want %v", err, ErrTaskExecutionNotQuiescent)
	}
}

func TestCurrentNodeControllerInterruptNoOpsWhenTaskIsAlreadyInterrupted(t *testing.T) {
	reference := currentNodeReferenceForControllerTest(t, "task-already-interrupted", "node-agent")
	store := &currentNodeControllerStore{
		interrupted: []workflow.CurrentNode{{
			Reference: reference,
			Scheduling: &workflow.CurrentNodeScheduling{
				State: workflow.CurrentNodeSchedulingInterrupted,
			},
		}},
	}
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	controller := newCurrentNodeControllerForTest(t, store, &countingCurrentNodeRunner{}, authority, 1)
	t.Cleanup(func() {
		_ = controller.Close()
		_ = authority.Close(context.Background())
	})

	if err := controller.Interrupt(context.Background(), InterruptSelector{TaskID: reference.TaskID}); err != nil {
		t.Fatalf("Interrupt already-interrupted Task: %v", err)
	}
}

func TestCurrentNodeControllerInterruptNoOpsOnlyForMatchingInterruptedSession(t *testing.T) {
	reference := currentNodeReferenceForControllerTest(t, "task-already-interrupted-session", "node-agent")
	interruptedSessionID := runtimeids.NewSessionID()
	otherSessionID := runtimeids.NewSessionID()
	store := &currentNodeControllerStore{
		interrupted: []workflow.CurrentNode{{
			Reference: reference,
			SessionID: &interruptedSessionID,
			Scheduling: &workflow.CurrentNodeScheduling{
				State: workflow.CurrentNodeSchedulingInterrupted,
			},
		}},
	}
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	controller := newCurrentNodeControllerForTest(t, store, &countingCurrentNodeRunner{}, authority, 1)
	t.Cleanup(func() {
		_ = controller.Close()
		_ = authority.Close(context.Background())
	})

	if err := controller.Interrupt(context.Background(), InterruptSelector{
		TaskID:    reference.TaskID,
		SessionID: &interruptedSessionID,
	}); err != nil {
		t.Fatalf("Interrupt already-interrupted Session: %v", err)
	}
	if err := controller.Interrupt(context.Background(), InterruptSelector{
		TaskID:    reference.TaskID,
		SessionID: &otherSessionID,
	}); !errors.Is(err, ErrNoInterruptibleExecution) {
		t.Fatalf("Interrupt another Session error = %v, want %v", err, ErrNoInterruptibleExecution)
	}
}

func TestCurrentNodeControllerTaskInterruptRejectsPartiallyInterruptedTask(t *testing.T) {
	interrupted := currentNodeReferenceForControllerTest(t, "task-partially-interrupted", "node-interrupted")
	waiting := currentNodeReferenceForControllerTest(t, string(interrupted.TaskID), "node-waiting")
	store := &currentNodeControllerStore{
		currentNodes: []workflow.CurrentNode{
			{
				Reference: interrupted,
				Scheduling: &workflow.CurrentNodeScheduling{
					State: workflow.CurrentNodeSchedulingInterrupted,
				},
			},
			{
				Reference: waiting,
				Scheduling: &workflow.CurrentNodeScheduling{
					State: workflow.CurrentNodeSchedulingAdmitted,
				},
			},
		},
	}
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	controller := newCurrentNodeControllerForTest(t, store, &countingCurrentNodeRunner{}, authority, 1)
	t.Cleanup(func() {
		_ = controller.Close()
		_ = authority.Close(context.Background())
	})

	if err := controller.Interrupt(context.Background(), InterruptSelector{
		TaskID: interrupted.TaskID,
	}); !errors.Is(err, ErrNoInterruptibleExecution) {
		t.Fatalf("Interrupt partially interrupted Task error = %v, want %v", err, ErrNoInterruptibleExecution)
	}
}

func TestCurrentNodeControllerProtocolViolationCapStopsAndInterruptsLiveScope(t *testing.T) {
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh executable unavailable: %v", err)
	}
	queue := newControllerQueueFixture(t, 1)
	reference := queue.tasks[0].reference(t, 0)
	store := queue.store()
	var controller *CurrentNodeController
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runner := &recordingScriptRunner{
		authority: authority,
		command: sessionruntime.ScriptCommand{
			Path: shellPath,
			Args: []string{"-c", "while :; do sleep 1; done"},
		},
		started: make(chan workflow.CurrentNodeReference, 1),
	}
	controller = newCurrentNodeControllerForTest(t, store, runner, authority, 1)
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	if err := queue.approve(context.Background(), controller, reference.TaskID); err != nil {
		t.Fatalf("start current node: %v", err)
	}
	<-runner.started
	waitForRunningCurrentNode(t, authority, reference)
	scopeID := singleLiveScope(t, authority, reference)
	result, err := controller.RecordProtocolViolation(context.Background(), workflowruntime.ViolationRequest{
		ScopeID:  scopeID,
		Kind:     workflowruntime.ViolationKindInvalidCompletion,
		MaxCount: 2,
		Detail:   "invalid completion",
	})
	if err != nil {
		t.Fatalf("record protocol violation: %v", err)
	}
	if result.Interrupted || result.Count != 1 {
		t.Fatalf("first violation result = %+v, want count 1 without interruption", result)
	}
	if err := controller.ResetProtocolViolationBudget(context.Background(), workflowruntime.ViolationResetRequest{
		ScopeID: scopeID,
	}); err != nil {
		t.Fatalf("reset protocol violation budget: %v", err)
	}
	result, err = controller.RecordProtocolViolation(context.Background(), workflowruntime.ViolationRequest{
		ScopeID:  scopeID,
		Kind:     workflowruntime.ViolationKindInvalidCompletion,
		MaxCount: 2,
		Detail:   "invalid completion",
	})
	if err != nil {
		t.Fatalf("record violation after reset: %v", err)
	}
	if result.Interrupted || result.Count != 1 {
		t.Fatalf("post-reset violation result = %+v, want count 1 without interruption", result)
	}
	result, err = controller.RecordProtocolViolation(context.Background(), workflowruntime.ViolationRequest{
		ScopeID:  scopeID,
		Kind:     workflowruntime.ViolationKindInvalidCompletion,
		MaxCount: 2,
		Detail:   "invalid completion",
	})
	if err != nil {
		t.Fatalf("record cap violation: %v", err)
	}
	if !result.Interrupted || result.Count != 2 {
		t.Fatalf("cap violation result = %+v, want count 2 and interrupted", result)
	}
	interruption, ok := store.interruption(reference)
	if !ok || interruption.reason != reasonProtocolViolationCap {
		t.Fatalf("protocol interruption = %+v, want reason %q", interruption, reasonProtocolViolationCap)
	}
	testsetup.RequireUntil(t, time.Now().Add(3*time.Second), 10*time.Millisecond, func() bool {
		_, live := authority.ExecutionByScope(scopeID)
		return !live
	}, "protocol-capped execution did not retire")
	if _, err := controller.RecordProtocolViolation(context.Background(), workflowruntime.ViolationRequest{
		ScopeID:  scopeID,
		Kind:     workflowruntime.ViolationKindInvalidCompletion,
		MaxCount: 2,
	}); !errors.Is(err, sessionruntime.ErrExecutionNoLongerLive) {
		t.Fatalf("retired protocol budget error = %v, want %v", err, sessionruntime.ErrExecutionNoLongerLive)
	}
}
