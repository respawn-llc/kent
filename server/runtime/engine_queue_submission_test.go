package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"core/server/llm"
	"core/server/tools"
	"core/shared/runtimeinput"
	"core/shared/textutil"
)

func TestPostTurnQueueDeliversOneItemPerTurnWhileSteersBatch(t *testing.T) {
	testPostTurnQueueDeliversOneItemPerTurn(t, false)
}

func TestPostTurnQueueDoesNotConsumeAnotherItemOnProviderRetry(t *testing.T) {
	testPostTurnQueueDeliversOneItemPerTurn(t, true)
}

func testPostTurnQueueDeliversOneItemPerTurn(t *testing.T, retryQueuedRequest bool) {
	t.Helper()
	started := make(chan int, 4)
	release := make(chan struct{})
	defer close(release)
	var call int
	client := &hookClient{response: finalTextResponse("done")}
	if retryQueuedRequest {
		client.errors = []error{nil, nil, &llm.ProviderAPIError{ProviderID: "openai", StatusCode: 503, Code: llm.UnifiedErrorCodeUnknown}}
	}
	client.beforeReturn = func() error {
		call++
		started <- call
		<-release
		return nil
	}
	submitted := make(chan string, 10)
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{
		Model: "gpt-6-sol",
		OnEvent: func(event Event) {
			if event.QueuedUserMessageStatus != nil && event.QueuedUserMessageStatus.Status == QueuedUserMessageSubmitted {
				submitted <- event.QueuedUserMessageStatus.QueueItemID
			}
		},
	})
	done := make(chan error, 1)
	go func() {
		_, err := engine.SubmitUserMessage(t.Context(), "start")
		done <- err
	}()
	waitCall := func(want int) {
		t.Helper()
		select {
		case got := <-started:
			if got != want {
				t.Fatalf("provider request = %d, want %d", got, want)
			}
		case <-time.After(runtimeTestSynchronizationTimeout):
			t.Fatalf("provider request %d never started", want)
		}
	}
	waitCall(1)
	first, err := engine.QueueUserInput(t.Context(), plainQueuedUserInput("first queued"))
	pendingWorkTestNoError(t, err)
	second, err := engine.QueueUserInput(t.Context(), plainQueuedUserInput("second queued"))
	pendingWorkTestNoError(t, err)
	steerOne, err := engine.Steer(t.Context(), "first steer", nil)
	pendingWorkTestNoError(t, err)
	steerTwo, err := engine.Steer(t.Context(), "second steer", nil)
	pendingWorkTestNoError(t, err)
	release <- struct{}{}
	waitCall(2)
	want := map[string]bool{steerOne.ID: true, steerTwo.ID: true}
	for len(submitted) > 0 {
		id := <-submitted
		if id == first.ID || id == second.ID {
			t.Fatal("Queue item was drained at an in-turn steering boundary")
		}
		delete(want, id)
	}
	if len(want) != 0 {
		t.Fatalf("eligible steers were not delivered together: %+v", want)
	}
	release <- struct{}{}
	waitCall(3)
	select {
	case id := <-submitted:
		if id != first.ID {
			t.Fatalf("first queued turn submitted %s, want %s", id, first.ID)
		}
	default:
		t.Fatal("first queued turn did not submit the first Queue item")
	}
	select {
	case id := <-submitted:
		t.Fatalf("first queued turn drained another item %s", id)
	default:
	}
	pending := pendingWorkTestSnapshot(t, engine)
	if len(pending.Items) != 1 || pending.Items[0].ID.String() != second.ID {
		t.Fatalf("second Queue item must remain pending: %+v", pending.Items)
	}
	release <- struct{}{}
	waitCall(4)
	if retryQueuedRequest {
		select {
		case id := <-submitted:
			t.Fatalf("provider retry consumed another Queue item %s", id)
		default:
		}
		release <- struct{}{}
		waitCall(5)
	}
	select {
	case id := <-submitted:
		if id != second.ID {
			t.Fatalf("next queued turn submitted %s, want %s", id, second.ID)
		}
	default:
		t.Fatal("next queued turn did not submit the remaining Queue item")
	}
	release <- struct{}{}
	pendingWorkTestNoError(t, <-done)
	waitEngineLifecycleTasks(t, engine)
}

func TestStopRestoresSteerAndPostTurnQueueWithoutContinuation(t *testing.T) {
	client := newBlockingThenQueuedClient()
	var mu sync.Mutex
	var restored []InterruptedHumanInput
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{
		Model: "gpt-6-sol",
		OnEvent: func(event Event) {
			if event.HumanInputInterrupted != nil {
				mu.Lock()
				restored = append(restored, event.HumanInputInterrupted.Items...)
				mu.Unlock()
			}
		},
	})
	done := make(chan error, 1)
	go func() {
		_, err := engine.SubmitUserMessage(t.Context(), "start")
		done <- err
	}()
	pendingWorkTestWait(t, client.started, "held provider")
	steer, err := engine.SteerInput(t.Context(), QueuedUserInput{
		ExecutionText: "expanded steer", CanonicalPresentation: "/review steer",
	}, nil)
	pendingWorkTestNoError(t, err)
	queued, err := engine.QueueUserInput(t.Context(), QueuedUserInput{
		ExecutionText: "expanded queue", CanonicalPresentation: "/review queue",
	})
	pendingWorkTestNoError(t, err)
	stopped, err := engine.TryInterruptActiveRun()
	pendingWorkTestNoError(t, err)
	close(client.releaseC)
	if !stopped {
		t.Fatal("Stop did not stop the held provider")
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("stopped turn = %v", err)
	}
	waitEngineLifecycleTasks(t, engine)
	mu.Lock()
	items := append([]InterruptedHumanInput(nil), restored...)
	mu.Unlock()
	if len(items) != 2 || items[0].QueueItemID != steer.ID || items[1].QueueItemID != queued.ID ||
		items[0].Text != "/review steer" || items[1].Text != "/review queue" {
		t.Fatalf("restored inputs = %+v, want Steer then Queue", items)
	}
	if pending := pendingWorkTestSnapshot(t, engine); len(pending.Items) != 0 {
		t.Fatalf("Stop left Pending Work: %+v", pending.Items)
	}
	client.mu.Lock()
	calls := len(client.calls)
	client.mu.Unlock()
	if calls != 1 {
		t.Fatalf("Stop launched continuation: provider calls = %d", calls)
	}
	_, err = engine.Steer(t.Context(), "subsequent send", nil)
	pendingWorkTestNoError(t, err)
	waitEngineLifecycleTasks(t, engine)
}

func TestPostTurnQueueStartsAfterActiveTurnCompletes(t *testing.T) {
	client := &fakeClient{responses: []llm.Response{{Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("queued work handled"), Phase: textutil.Value(llm.MessagePhaseFinal)}}}}
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{Model: "gpt-6-sol"})
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- engine.stepLifecycle.Run(context.Background(), exclusiveStepOptions{EmitRunState: true, ActiveKind: ActiveKindUserTurn}, func(context.Context, string) error {
			close(started)
			<-release
			return nil
		})
	}()
	select {
	case <-started:
	case <-time.After(runtimeTestSynchronizationTimeout):
		t.Fatal("active turn did not start")
	}
	queuedDone := make(chan struct {
		item QueuedUserMessage
		err  error
	}, 1)
	go func() {
		queued, err := engine.QueueUserInput(t.Context(), plainQueuedUserInput("queued input"))
		queuedDone <- struct {
			item QueuedUserMessage
			err  error
		}{item: queued, err: err}
	}()
	var queued QueuedUserMessage
	select {
	case result := <-queuedDone:
		if result.err != nil || result.item.ID == "" {
			t.Fatalf("accepted queue item = %+v/%v", result.item, result.err)
		}
		queued = result.item
	case <-time.After(runtimeTestSynchronizationTimeout):
		t.Fatal("post-turn Queue acceptance waited for the protected Step boundary")
	}
	if queued.ID == "" {
		t.Fatal("post-turn Queue accepted an empty item")
	}
	if pending := pendingWorkTestSnapshot(t, engine); len(pending.Items) != 1 || pending.Items[0].ID.String() != queued.ID || pending.Items[0].Lane != runtimeinput.PendingWorkLaneQueue {
		t.Fatalf("Pending Work during protected Step = %+v, want accepted Queue item", pending.Items)
	}
	if calls := fakeClientCallCount(client); calls != 0 {
		t.Fatalf("queued work model calls during protected Step = %d, want none", calls)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("active turn completion: %v", err)
	}
	waitEngineLifecycleTasks(t, engine)
	if calls := fakeClientCallCount(client); calls != 1 {
		t.Fatalf("queued work model calls = %d, want 1", calls)
	}
	if engine.HasQueuedUserWork() {
		t.Fatal("post-turn Queue remained pending after the active turn completed")
	}
}

func TestPostTurnQueueStartsImmediatelyWhenRuntimeIsIdle(t *testing.T) {
	client := &fakeClient{responses: []llm.Response{{
		Assistant: llm.Message{
			Role:    llm.RoleAssistant,
			Content: textutil.Value("queued work handled"),
			Phase:   textutil.Value(llm.MessagePhaseFinal),
		},
	}}}
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{Model: "gpt-6-sol"})

	queued, err := engine.QueueUserInput(t.Context(), plainQueuedUserInput("queued input"))
	if err != nil {
		t.Fatalf("QueueUserMessage: %v", err)
	}
	if queued.ID == "" {
		t.Fatal("QueueUserMessage returned no Queue Item identity")
	}
	waitEngineLifecycleTasks(t, engine)
	if calls := fakeClientCallCount(client); calls != 1 {
		t.Fatalf("queued work model calls = %d, want 1", calls)
	}
	if engine.HasQueuedUserWork() {
		t.Fatal("idle Queue item remained pending after its immediate turn")
	}
}

func TestPostTurnQueueDoesNotHoldCompletedLiveRun(t *testing.T) {
	for _, test := range []struct {
		name      string
		autoStart bool
	}{
		{name: "staged input"},
		{name: "public Queue", autoStart: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, started, releaseProvider := newGatedHookClient(finalTextResponse("original answer"), finalTextResponse("queued answer"))
			defer releaseProvider()
			engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{Model: "gpt-6-sol"})
			done := make(chan error, 1)
			go func() {
				_, err := engine.SubmitUserMessage(t.Context(), "start")
				done <- err
			}()
			pendingWorkTestWait(t, started, "held provider")
			waitCtx, cancelWait := context.WithTimeout(t.Context(), runtimeTestSynchronizationTimeout)
			defer cancelWait()
			handle, err := engine.CaptureActiveRunResult(waitCtx)
			pendingWorkTestNoError(t, err)
			var queued QueuedUserMessage
			if test.autoStart {
				queued, err = engine.QueueUserInput(t.Context(), plainQueuedUserInput("later"))
			} else {
				queued, err = engine.QueueUserMessage(t.Context(), "later")
			}
			pendingWorkTestNoError(t, err)
			releaseProvider()
			pendingWorkTestNoError(t, <-done)
			waitEngineLifecycleTasks(t, engine)
			result, err := handle.Wait()
			pendingWorkTestNoError(t, err)
			if result.Status != RunStatusCompleted || result.ResultKind != LiveRunResultAssistantFinalAnswer ||
				messageContent(result.AssistantMessage) != "original answer" {
				t.Fatalf("original live result = %+v", result)
			}
			if engine.HasActiveLiveRunGroup() {
				t.Fatal("post-turn Queue retained a completed execution")
			}
			pending := pendingWorkTestSnapshot(t, engine)
			wantCalls := 1
			if test.autoStart {
				wantCalls = 2
				if len(pending.Items) != 0 {
					t.Fatalf("public Queue remained pending: %+v", pending.Items)
				}
			} else if len(pending.Items) != 1 || pending.Items[0].ID.String() != queued.ID {
				t.Fatalf("staged input = %+v, want accepted item still pending", pending.Items)
			}
			if calls := hookClientCallCount(client); calls != wantCalls {
				t.Fatalf("provider calls = %d, want %d", calls, wantCalls)
			}
		})
	}
}

func TestQueuedUserMessageCallerCancellationStopsWaitAndPreventsLaterAcceptance(t *testing.T) {
	engine := mustNewExecTestEngine(t, mustCreateTestSession(t), &fakeClient{}, Config{Model: "gpt-6-sol"})
	caller, cancel := context.WithCancel(t.Context())
	defer cancel()
	accepting := make(chan struct{})
	accept := func(commit func() (bool, error)) (bool, error) {
		close(accepting)
		<-caller.Done()
		return false, context.Cause(caller)
	}
	done := make(chan error, 1)
	go func() {
		_, err := engine.QueueUserInputWithAcceptance(caller, plainQueuedUserInput("canceled input"), accept)
		done <- err
	}()
	pendingWorkTestWait(t, accepting, "Queue acceptance")

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled Queue wait = %v, want canceled", err)
		}
	case <-time.After(runtimeTestSynchronizationTimeout):
		t.Fatal("canceled Queue caller remained blocked")
	}
	if engine.HasQueuedUserWork() {
		t.Fatal("canceled caller created Pending Work")
	}
}

func TestConcurrentQueueRemovalDoesNotKeepLaterSendRunning(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("fail=%t", fail), func(t *testing.T) {
			client, started, release := newGatedHookClient(finalTextResponse("initial"), finalTextResponse("later"))
			defer release()
			engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{Model: "gpt-6-sol"})
			initialDone := make(chan error, 1)
			go func() {
				_, err := engine.SubmitUserMessage(t.Context(), "start")
				initialDone <- err
			}()
			pendingWorkTestWait(t, started, "held provider")
			inserted := make(chan QueuedUserMessage, 1)
			releaseAdmission := make(chan struct{})
			finishAdmission := sync.OnceFunc(func() { close(releaseAdmission) })
			defer finishAdmission()
			engine.messageFlow = &heldQueueInsertion{
				messageLifecycle: engine.messageFlow,
				inserted:         inserted,
				release:          releaseAdmission,
			}
			admitted := make(chan error, 1)
			go func() {
				_, err := engine.QueueUserInput(t.Context(), plainQueuedUserInput("queued"))
				admitted <- err
			}()
			var queued QueuedUserMessage
			select {
			case queued = <-inserted:
			case <-time.After(runtimeTestSynchronizationTimeout):
				t.Fatal("Queue was not inserted")
			}
			if fail {
				failed := engine.FailQueuedUserMessages(QueuedUserMessageFailureTerminalWorkflowCompletion)
				if len(failed) != 1 || failed[0].ID != queued.ID {
					t.Fatalf("failed inputs = %+v, want inserted Queue", failed)
				}
			} else {
				_, err := engine.RemovePendingWork(t.Context(), mustQueueItemID(queued.ID))
				pendingWorkTestNoError(t, err)
			}
			finishAdmission()
			pendingWorkTestNoError(t, <-admitted)
			release()
			pendingWorkTestNoError(t, <-initialDone)
			waitEngineLifecycleTasks(t, engine)
			_, err := engine.Steer(t.Context(), "subsequent send", nil)
			pendingWorkTestNoError(t, err)
			waitEngineLifecycleTasks(t, engine)
			if engine.HasQueuedUserWork() || engine.HasScheduledQueuedUserWork() {
				t.Fatal("removed Queue kept the subsequent Send running")
			}
		})
	}
}

type heldQueueInsertion struct {
	messageLifecycle
	once     sync.Once
	inserted chan<- QueuedUserMessage
	release  <-chan struct{}
}

func (m *heldQueueInsertion) QueueUserMessageWithID(item QueuedUserMessage, association ...queuedUserMessageAssociation) (QueuedUserMessage, error) {
	queued, err := m.messageLifecycle.QueueUserMessageWithID(item, association...)
	if err == nil {
		m.once.Do(func() {
			m.inserted <- queued
			<-m.release
		})
	}
	return queued, err
}
