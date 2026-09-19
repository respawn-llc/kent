package workflowexecution

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/session"
	"core/server/sessionruntime"
	"core/server/workflow"
	"core/server/workflowruntime"
)

func TestCurrentNodeControllerAdmitsScriptBeforeDetachedPublication(t *testing.T) {
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh executable unavailable: %v", err)
	}
	queue := newControllerQueueFixture(t, 1)
	reference := queue.tasks[0].reference(t, 0)
	outputPath := t.TempDir() + "/started"
	store := &currentNodeControllerStore{Store: queue.tasks[0].store, queueFixture: queue}
	var controller *CurrentNodeController
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runner := &controlledScriptRunner{
		authority:   authority,
		command:     sessionruntime.ScriptCommand{Path: shellPath, Args: []string{"-c", `printf started > "$1"; trap 'exit 0' TERM; while :; do sleep 1; done`, "sh", outputPath}},
		entered:     make(chan struct{}),
		startRunner: make(chan struct{}),
		registered:  make(chan struct{}),
		returnStart: make(chan struct{}),
		handles:     make(chan sessionruntime.ExecutionHandle, 1),
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

	started := make(chan error, 1)
	go func() {
		queue.automaticIntents(controller, []CurrentNodeAutomaticIntent{{CurrentNode: reference, NodeKind: workflow.NodeKindScript}})
		started <- nil
	}()
	<-runner.entered
	if store.admitCount() != 0 {
		t.Fatalf("admitted current nodes before publication = %d, want 0", store.admitCount())
	}
	close(runner.startRunner)
	<-runner.registered
	if _, err := os.Stat(outputPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("script started before publication validation: stat error = %v", err)
	}
	if store.admitCount() != 0 {
		t.Fatalf("admitted current nodes before publication validation = %d, want 0", store.admitCount())
	}
	close(runner.returnStart)
	handle := <-runner.handles
	if store.admitCount() != 1 {
		t.Fatalf("admitted current nodes after publication validation = %d, want 1", store.admitCount())
	}
	if err := <-started; err != nil {
		t.Fatalf("start current node: %v", err)
	}
	workflowRef, workflowScoped := handle.Scope().Workflow()
	if !workflowScoped || !workflowRef.CurrentNode.Equal(reference) {
		t.Fatalf("published Workflow metadata = %+v, want Current Node %v", workflowRef, reference)
	}
	testsetup.RequireUntil(t, time.Now().Add(3*time.Second), 10*time.Millisecond, func() bool {
		_, err := os.Stat(outputPath)
		return err == nil
	}, "script did not start after controller released lease")
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("stop Script: %v", err)
	}
	if hasLiveCurrentNode(authority, reference) {
		t.Fatal("script execution remained live after retirement")
	}
}

func TestCurrentNodeControllerCloseDoesNotCancelStartedDurableAdmission(t *testing.T) {
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh executable unavailable: %v", err)
	}
	queue := newControllerQueueFixture(t, 1)
	reference := queue.tasks[0].reference(t, 0)
	store := &currentNodeControllerStore{Store: queue.tasks[0].store, queueFixture: queue,
		admitStarted: make(chan struct{}),
		admitRelease: make(chan struct{}),
	}
	var controller *CurrentNodeController
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runner := &recordingScriptRunner{
		authority: authority,
		command:   sessionruntime.ScriptCommand{Path: shellPath, Args: []string{"-c", "exit 0"}},
		started:   make(chan workflow.CurrentNodeReference, 1),
	}
	controller = newCurrentNodeControllerForTest(t, store, runner, authority, 1)
	started := make(chan error, 1)
	go func() {
		queue.automaticIntents(controller, []CurrentNodeAutomaticIntent{{CurrentNode: reference, NodeKind: workflow.NodeKindScript}})
		started <- nil
	}()
	<-store.admitStarted
	closed := make(chan error, 1)
	go func() { closed <- controller.Close() }()
	close(store.admitRelease)
	if err := <-started; err != nil && !errors.Is(err, sessionruntime.ErrExecutionNoLongerLive) {
		t.Fatalf("start Current Node during close: %v", err)
	}
	if err := <-closed; err != nil {
		t.Fatalf("close controller: %v", err)
	}
	store.mu.Lock()
	sawCancellation := store.admitSawCancellation
	store.mu.Unlock()
	if sawCancellation || store.admitCount() != 1 {
		t.Fatalf("started admission = canceled:%t commits:%d, want false/1", sawCancellation, store.admitCount())
	}
	if err := authority.Close(context.Background()); err != nil {
		t.Fatalf("close authority: %v", err)
	}
}

func TestCurrentNodeAdmissionCommitCertainty(t *testing.T) {
	observerErr := errors.New("observer unavailable")
	if err := classifyCurrentNodeAdmission(session.CommitReceipt{Committed: true}, observerErr); err != nil {
		t.Fatalf("committed admission observer error = %v, want publication to continue", err)
	}
	definite := session.DefinitelyUncommittedMutation(errors.New("write rejected"))
	if err := classifyCurrentNodeAdmission(session.CommitReceipt{}, definite); !errors.Is(err, session.ErrMutationDefinitelyUncommitted) {
		t.Fatalf("definitely-uncommitted admission = %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("indeterminate admission did not fail fast")
		}
	}()
	_ = classifyCurrentNodeAdmission(session.CommitReceipt{}, errors.New("commit result unavailable"))
}

func TestAutomaticCurrentNodeStartFailureIsProcessFatalWhenInterruptionCannotPersist(t *testing.T) {
	reference := currentNodeReferenceForControllerTest(t, "task-automatic-fatal", "node-successor")
	startFailure := errors.New("automatic successor assignment failed")
	interruptionFailure := errors.New("current node interruption persistence failed")
	controller := &CurrentNodeController{
		store:     &currentNodeControllerStore{interruptionErr: interruptionFailure},
		mutations: NewTaskMutationCoordinator(),
	}

	defer func() {
		recovered := recover()
		fatal, ok := recovered.(*CurrentNodeAutomaticInterruptionPersistencePanic)
		if !ok {
			t.Fatalf("recovered panic = %#v, want automatic interruption persistence panic", recovered)
		}
		if fatal.Operation != "ready_start" ||
			!fatal.Reference.Equal(reference) ||
			fatal.ExpectedScheduling != workflow.CurrentNodeSchedulingReady ||
			!errors.Is(fatal.OriginalFailure, startFailure) ||
			!errors.Is(fatal.InterruptionFailure, interruptionFailure) {
			t.Fatalf("fatal panic = %+v, want exact automatic successor failure", fatal)
		}
		var processFatal interface{ ProcessFatalPanic() } = fatal
		processFatal.ProcessFatalPanic()
	}()

	controller.handleCurrentNodeStartFailures([]currentNodeQueuedStart{{
		reference: reference,
		policy:    currentNodeAdmissionAutomaticAgent,
	}}, false, startFailure)
}

func TestCurrentNodeControllerRunnerFailuresInterruptAdmittedCurrentNode(t *testing.T) {
	for name, cause := range map[string]error{
		"ordinary failure":         errors.New("provider unavailable"),
		"execution no longer live": sessionruntime.ErrExecutionNoLongerLive,
	} {
		t.Run(name, func(t *testing.T) {
			queue := newControllerQueueFixture(t, 1)
			reference := queue.tasks[0].reference(t, 0)
			store := &currentNodeControllerStore{Store: queue.tasks[0].store, queueFixture: queue}
			attention := &currentNodeAttentionRecorder{}
			authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
			controller := newCurrentNodeControllerWithAttentionForTest(t, store, failingCurrentNodeRunner{cause: cause}, authority, 1, attention)
			t.Cleanup(func() {
				if err := controller.Close(); err != nil {
					t.Errorf("close controller: %v", err)
				}
				if err := authority.Close(context.Background()); err != nil {
					t.Errorf("close authority: %v", err)
				}
			})

			if err := queue.approve(context.Background(), controller, reference.TaskID); err != nil {
				t.Fatalf("queue current node start: %v", err)
			}
			var interruption currentNodeInterruptionRecord
			testsetup.RequireUntil(t, time.Now().Add(3*time.Second), 10*time.Millisecond, func() bool {
				var interrupted bool
				interruption, interrupted = store.interruption(reference)
				return interrupted
			}, "runner failure did not interrupt the admitted current node")
			if interruption.reason != "workflow_runtime_start_failed" {
				t.Fatalf("interruption reason = %q, want workflow_runtime_start_failed", interruption.reason)
			}
			if calls := store.interruptionCount(reference); calls != 1 {
				t.Fatalf("runner failure interruption writes = %d, want 1", calls)
			}
			testsetup.RequireUntil(t, time.Now().Add(3*time.Second), 10*time.Millisecond, func() bool {
				return attention.pendingCount() == 1
			}, "runner failure did not publish interrupted Current Node attention")
		})
	}
}

func TestCurrentNodeControllerExecutionLossAfterCommittedAssignmentInterruptsReadyCurrentNode(t *testing.T) {
	queue := newControllerQueueFixture(t, 1)
	reference := queue.tasks[0].reference(t, 0)
	store := &currentNodeControllerStore{Store: queue.tasks[0].store, queueFixture: queue}
	attention := &currentNodeAttentionRecorder{}
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runner := &countingCurrentNodeRunner{}
	store.assignment = &recordingCurrentNodeAssignmentSteerer{
		waitReceipt: session.CommitReceipt{Committed: true},
		waitErr:     sessionruntime.ErrExecutionNoLongerLive,
	}
	controller := newCurrentNodeControllerWithConfigForTest(t, store, runner, authority, NewTaskMutationCoordinator(), CurrentNodeControllerConfig{
		AgentConcurrency: 1,
		Attention:        attention,
	})
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	if err := queue.approve(context.Background(), controller, reference.TaskID); !errors.Is(err, sessionruntime.ErrExecutionNoLongerLive) {
		t.Fatalf("approval assignment error = %v, want exact execution loss", err)
	}
	var interruption currentNodeInterruptionRecord
	testsetup.RequireUntil(t, time.Now().Add(3*time.Second), 10*time.Millisecond, func() bool {
		var interrupted bool
		interruption, interrupted = store.interruption(reference)
		return interrupted
	}, "pre-admission execution loss did not interrupt the ready current node")
	if admitted := store.admitCount(); admitted != 0 {
		t.Fatalf("admitted current nodes = %d, want 0", admitted)
	}
	if deliveries := runner.promptDeliveries(); len(deliveries) != 0 {
		t.Fatalf("runner prompt deliveries = %+v, want none", deliveries)
	}
	if interruption.reason != reasonCurrentNodeRuntimeStartFailed {
		t.Fatalf("interruption reason = %q, want %q", interruption.reason, reasonCurrentNodeRuntimeStartFailed)
	}
	if interruption.detail.Code != string(reasonCurrentNodeRuntimeStartFailed) ||
		interruption.detail.Diagnostic() == nil {
		t.Fatalf("interruption detail = %+v, want runtime-start diagnostic", interruption.detail)
	}
	testsetup.RequireUntil(t, time.Now().Add(3*time.Second), 10*time.Millisecond, func() bool {
		return attention.pendingCount() == 1
	}, "pre-admission execution loss did not publish interrupted Current Node attention")
}

func TestCurrentNodeControllerExplicitAdmissionStartsParallelBranchesIndependently(t *testing.T) {
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh executable unavailable: %v", err)
	}
	queue := newControllerQueueFixture(t, 2)
	first := queue.tasks[0].reference(t, 0)
	second := queue.tasks[0].reference(t, 1)
	store := queue.store()
	var controller *CurrentNodeController
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runner := &parallelExplicitRunner{
		authority:      authority,
		shellPath:      shellPath,
		blocked:        first,
		blockedEntered: make(chan struct{}),
		releaseBlocked: make(chan struct{}),
		siblingStarted: make(chan workflow.CurrentNodeReference, 1),
	}
	var releaseOnce sync.Once
	releaseBlocked := func() {
		releaseOnce.Do(func() {
			close(runner.releaseBlocked)
		})
	}
	attention := &currentNodeAttentionRecorder{}
	controller = newCurrentNodeControllerWithAttentionForTest(t, store, runner, authority, 1, attention)
	t.Cleanup(func() {
		releaseBlocked()
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
	case <-runner.blockedEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("first resumed branch did not begin setup")
	}
	select {
	case started := <-runner.siblingStarted:
		if !started.Equal(second) {
			t.Fatalf("independent resumed branch = %v, want %v", started, second)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("blocked first branch prevented sibling admission")
	}
	releaseBlocked()
	testsetup.RequireUntil(t, time.Now().Add(3*time.Second), 10*time.Millisecond, func() bool {
		_, interrupted := store.interruption(first)
		return interrupted
	}, "failed resumed branch was not durably interrupted")
}

func TestCurrentNodeControllerSteersUnclassifiedAutomaticAgentBeforeStartingIt(t *testing.T) {
	queue := newControllerQueueFixture(t, 1)
	reference := queue.tasks[0].reference(t, 0)
	store := &currentNodeControllerStore{Store: queue.tasks[0].store, queueFixture: queue}
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runner := &countingCurrentNodeRunner{}
	steerer := &recordingCurrentNodeAssignmentSteerer{}
	store.assignment = steerer
	controller, err := NewCurrentNodeController(
		store,
		currentNodeTestPublicationRunner{runner: runner, authority: authority, store: store},
		authority,
		NewTaskMutationCoordinator(),
		CurrentNodeControllerConfig{
			AgentConcurrency: 1,
		},
	)
	if err != nil {
		t.Fatalf("new current node controller: %v", err)
	}
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	queue.automaticIntents(controller, []CurrentNodeAutomaticIntent{{
		CurrentNode: reference,
		NodeKind:    workflow.NodeKindAgent,
	}})

	testsetup.RequireUntil(t, time.Now().Add(3*time.Second), 10*time.Millisecond, func() bool {
		return runner.starts() == 1
	}, "automatic Agent did not reach runner")
	if got := steerer.references(); len(got) != 1 || !got[0].Equal(reference) {
		t.Fatalf("steered assignments = %+v, want %v", got, reference)
	}
	if deliveries := runner.promptDeliveries(); len(deliveries) != 1 ||
		deliveries[0] != workflowruntime.TaskPromptDeliveryResume {
		t.Fatalf("automatic target prompt deliveries = %+v, want Resume after assignment", deliveries)
	}
}

func TestCurrentNodeControllerBoundsExplicitAdmissionSetupWithoutBlockingSiblings(t *testing.T) {
	const branchCount = explicitAdmissionConcurrency + 2
	queue := newControllerQueueFixture(t, branchCount)
	store := queue.store()
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runner := &boundedExplicitAdmissionRunner{
		entered: make(chan workflow.CurrentNodeReference, branchCount),
		release: make(chan struct{}),
	}
	controller := newCurrentNodeControllerForTest(t, store, runner, authority, 1)
	var releaseOnce sync.Once
	releaseAll := func() {
		releaseOnce.Do(func() {
			close(runner.release)
		})
	}
	t.Cleanup(func() {
		releaseAll()
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
	for index := 0; index < explicitAdmissionConcurrency; index++ {
		select {
		case <-runner.entered:
		case <-time.After(3 * time.Second):
			t.Fatalf("explicit admission %d did not begin", index+1)
		}
	}
	runner.release <- struct{}{}
	select {
	case <-runner.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("releasing explicit admission capacity did not admit a queued sibling")
	}
	releaseAll()
}

func TestCurrentNodeControllerReservesAutomaticCapacityBeforeLaunchingAdmission(t *testing.T) {
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh executable unavailable: %v", err)
	}
	queue := newControllerQueueFixture(t, 1, 1)
	first := queue.tasks[0].reference(t, 0)
	second := queue.tasks[1].reference(t, 0)
	store := &currentNodeControllerStore{Store: queue.tasks[0].store, queueFixture: queue}
	var controller *CurrentNodeController
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runner := &firstAdmissionBlockingScriptRunner{
		authority: authority,
		shellPath: shellPath,
		entered:   make(chan workflow.CurrentNodeReference, 2),
		release:   make(chan struct{}),
	}
	var releaseOnce sync.Once
	releaseRunner := func() {
		releaseOnce.Do(func() {
			close(runner.release)
		})
	}
	defer releaseRunner()
	controller = newCurrentNodeControllerForTest(t, store, runner, authority, 1)
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	queue.automaticIntents(controller, []CurrentNodeAutomaticIntent{
		{CurrentNode: first, NodeKind: workflow.NodeKindAgent},
		{CurrentNode: second, NodeKind: workflow.NodeKindAgent},
	})
	select {
	case entered := <-runner.entered:
		first = entered
	case <-time.After(3 * time.Second):
		t.Fatal("first automatic admission did not begin")
	}
	select {
	case entered := <-runner.entered:
		t.Fatalf("automatic admission %v began without available capacity", entered)
	case <-time.After(100 * time.Millisecond):
	}

	releaseRunner()
	testsetup.RequireUntil(t, time.Now().Add(3*time.Second), 10*time.Millisecond, func() bool {
		return hasLiveCurrentNode(authority, first)
	}, "first automatic current node did not become live")
	firstHandle, ok := authority.ExecutionByScope(singleLiveScope(t, authority, first))
	if !ok {
		t.Fatal("first automatic current node has no exact execution")
	}
	if err := firstHandle.Stop(context.Background()); err != nil {
		t.Fatalf("stop first automatic current node: %v", err)
	}
	select {
	case entered := <-runner.entered:
		if !entered.Equal(second) {
			t.Fatalf("second automatic admission = %v, want %v", entered, second)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued automatic admission did not begin after capacity released")
	}
}

func TestCurrentNodeControllerPromotesConcurrencyQueuedTaskToExplicitAdmission(t *testing.T) {
	queue := newControllerQueueFixture(t, 1, 1)
	first := queue.tasks[0].reference(t, 0)
	queued := queue.tasks[1].reference(t, 0)
	store := &currentNodeControllerStore{Store: queue.tasks[0].store, queueFixture: queue}
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runner := &boundedExplicitAdmissionRunner{
		entered: make(chan workflow.CurrentNodeReference, 2),
		release: make(chan struct{}),
	}
	controller := newCurrentNodeControllerForTest(t, store, runner, authority, 1)
	var releaseOnce sync.Once
	releaseAll := func() {
		releaseOnce.Do(func() {
			close(runner.release)
		})
	}
	t.Cleanup(func() {
		releaseAll()
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	queue.automaticIntents(controller, []CurrentNodeAutomaticIntent{
		{CurrentNode: first, NodeKind: workflow.NodeKindAgent},
		{CurrentNode: queued, NodeKind: workflow.NodeKindAgent},
	})
	select {
	case entered := <-runner.entered:
		if !entered.Equal(first) {
			t.Fatalf("first automatic admission = %v, want %v", entered, first)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first automatic admission did not begin")
	}
	select {
	case entered := <-runner.entered:
		t.Fatalf("automatic admission %v exceeded Agent capacity", entered)
	case <-time.After(100 * time.Millisecond):
	}

	observation, err := controller.ObserveWorkflowTaskExecutions([]workflow.TaskID{queued.TaskID})
	if err != nil {
		t.Fatalf("ObserveWorkflowTaskExecutions: %v", err)
	}
	if references := observation.ConcurrencyQueued[queued.TaskID]; len(references) != 1 ||
		!references[0].Equal(queued) {
		t.Fatalf("concurrency-queued Current Nodes = %+v, want %v", references, queued)
	}
	promoted, handled, err := controller.PromoteConcurrencyQueuedTask(
		context.Background(),
		queued.TaskID,
	)
	if err != nil {
		t.Fatalf("PromoteConcurrencyQueuedTask: %v", err)
	}
	if !handled || len(promoted) != 1 || !promoted[0].Reference.Equal(queued) {
		t.Fatalf("promoted Current Nodes = %+v handled=%v, want %v", promoted, handled, queued)
	}
	select {
	case entered := <-runner.entered:
		if !entered.Equal(queued) {
			t.Fatalf("explicit promoted admission = %v, want %v", entered, queued)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("promoted Current Node did not bypass Agent concurrency")
	}
}

func TestCurrentNodeControllerStartsScriptsWhileAgentCapacityIsSaturated(t *testing.T) {
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh executable unavailable: %v", err)
	}
	queue := newControllerQueueFixture(t, 1, 1, 1, 1)
	agent := queue.tasks[0].reference(t, 0)
	queuedAgent := queue.tasks[1].reference(t, 0)
	firstScript := queue.tasks[2].reference(t, 0)
	secondScript := queue.tasks[3].reference(t, 0)
	store := &currentNodeControllerStore{Store: queue.tasks[0].store, queueFixture: queue}
	var controller *CurrentNodeController
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runner := &recordingScriptRunner{
		authority: authority,
		command: sessionruntime.ScriptCommand{
			Path: shellPath,
			Args: []string{"-c", "trap 'exit 0' TERM; while :; do sleep 1; done"},
		},
		started: make(chan workflow.CurrentNodeReference, 4),
		scripts: map[workflow.CurrentNodeReference]struct{}{
			firstScript:  {},
			secondScript: {},
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
		CurrentNode: agent,
		NodeKind:    workflow.NodeKindAgent,
	}})
	select {
	case started := <-runner.started:
		if !started.Equal(agent) {
			t.Fatalf("first automatic start = %v, want %v", started, agent)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first automatic Agent Node did not start")
	}
	waitForRunningCurrentNode(t, authority, agent)

	queue.automaticIntents(controller, []CurrentNodeAutomaticIntent{
		{CurrentNode: queuedAgent, NodeKind: workflow.NodeKindAgent},
		{CurrentNode: firstScript, NodeKind: workflow.NodeKindScript},
		{CurrentNode: secondScript, NodeKind: workflow.NodeKindScript},
	})
	seenScripts := map[workflow.CurrentNodeReference]bool{}
	for len(seenScripts) < 2 {
		select {
		case started := <-runner.started:
			switch {
			case started.Equal(firstScript), started.Equal(secondScript):
				seenScripts[started] = true
			case started.Equal(queuedAgent):
				t.Fatalf("queued automatic Agent Node started before the occupying Agent was released")
			default:
				t.Fatalf("unexpected automatic start %v", started)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("Scripts did not start concurrently while Agent capacity was saturated: %v", seenScripts)
		}
	}
	testsetup.RequireUntil(t, time.Now().Add(3*time.Second), 10*time.Millisecond, func() bool {
		return hasLiveCurrentNode(authority, firstScript) && hasLiveCurrentNode(authority, secondScript)
	}, "both Script Nodes did not become live before Agent release")

	agentHandle, ok := authority.ExecutionByScope(singleLiveScope(t, authority, agent))
	if !ok {
		t.Fatal("occupying Agent has no exact execution")
	}
	if err := agentHandle.Stop(context.Background()); err != nil {
		t.Fatalf("stop occupying Agent: %v", err)
	}
	select {
	case started := <-runner.started:
		if !started.Equal(queuedAgent) {
			t.Fatalf("queued Agent start = %v, want %v", started, queuedAgent)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued Agent did not start after occupying Agent released")
	}
}

func TestCurrentNodeControllerCloseBroadcastsScriptStopsBeforeJoining(t *testing.T) {
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh executable unavailable: %v", err)
	}
	const scriptCount = 3
	grace := 250 * time.Millisecond
	script := `trap '' TERM; while :; do sleep 1; done`
	queue := newControllerQueueFixture(t, 1, 1, 1)
	store := queue.store()
	var controller *CurrentNodeController
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runner := &recordingScriptRunner{
		authority: authority,
		command: sessionruntime.ScriptCommand{
			Path:              shellPath,
			Args:              []string{"-c", script},
			CancellationGrace: &grace,
		},
		started: make(chan workflow.CurrentNodeReference, scriptCount),
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
	references := make([]workflow.CurrentNodeReference, 0, scriptCount)
	intents := make([]CurrentNodeAutomaticIntent, 0, scriptCount)
	for index := 0; index < scriptCount; index++ {
		reference := queue.tasks[index].reference(t, 0)
		references = append(references, reference)
		intents = append(intents, CurrentNodeAutomaticIntent{
			CurrentNode: reference,
			NodeKind:    workflow.NodeKindScript,
		})
	}
	queue.automaticIntents(controller, intents)
	started := make(map[workflow.CurrentNodeReference]struct{}, scriptCount)
	for len(started) < scriptCount {
		select {
		case reference := <-runner.started:
			started[reference] = struct{}{}
			if len(started) == scriptCount {
				break
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("Scripts did not start: %+v", started)
		}
	}
	for _, reference := range references {
		waitForRunningCurrentNode(t, authority, reference)
	}

	closeStarted := time.Now()
	if err := controller.Close(); err != nil {
		t.Fatalf("controller Close: %v", err)
	}
	if elapsed := time.Since(closeStarted); elapsed >= 2*grace {
		t.Fatalf("controller Close took %s for %d Script grace windows, want overlapping shutdown", elapsed, scriptCount)
	}
}

func TestCurrentNodeControllerReservationBlocksTaskQuiescence(t *testing.T) {
	reference := currentNodeReferenceForControllerTest(t, "task-reservation-quiescence", "node-agent")
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

	if err := controller.EnsureTaskQuiescent(reference.TaskID); !errors.Is(err, ErrTaskExecutionNotQuiescent) {
		t.Fatalf("EnsureTaskQuiescent error = %v, want %v", err, ErrTaskExecutionNotQuiescent)
	}
}

func TestCurrentNodeControllerTaskQuiescenceRejectsEveryControllerOwnedWorkState(t *testing.T) {
	reference := currentNodeReferenceForControllerTest(t, "task-quiescence-states", "node-agent")
	tests := []struct {
		name  string
		apply func(*CurrentNodeController)
	}{
		{
			name: "automatic queue",
			apply: func(controller *CurrentNodeController) {
				controller.automaticQueue.append(currentNodeQueuedStart{
					reference: reference,
					policy:    currentNodeAdmissionAutomaticAgent,
				})
			},
		},
		{
			name: "automatic reservation",
			apply: func(controller *CurrentNodeController) {
				key, err := reference.Key()
				if err != nil {
					t.Fatalf("reference key: %v", err)
				}
				controller.automaticReservations[key] = currentNodeQueuedStart{reference: reference, policy: currentNodeAdmissionAutomaticAgent}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
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
			controller.mu.Lock()
			test.apply(controller)
			controller.mu.Unlock()
			if err := controller.EnsureTaskQuiescent(reference.TaskID); !errors.Is(err, ErrTaskExecutionNotQuiescent) {
				t.Fatalf("EnsureTaskQuiescent error = %v, want %v", err, ErrTaskExecutionNotQuiescent)
			}
		})
	}
}
