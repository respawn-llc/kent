package runtime

import (
	"context"
	"sync"
	"testing"

	"core/shared/clientui"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
)

type pendingRemovalSteps struct {
	exclusiveStepLifecycle
	runNext func(context.Context, exclusiveStepOptions, func(context.Context, string) error) error
}

func (s pendingRemovalSteps) RunNext(ctx context.Context, options exclusiveStepOptions, fn func(context.Context, string) error) error {
	return s.runNext(ctx, options, fn)
}

type pendingRemovalObservedContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *pendingRemovalObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func TestRemovedPendingOperationProducesNoFailureFeedback(t *testing.T) {
	for _, timing := range []string{"waiting", "before entry"} {
		t.Run(timing, func(t *testing.T) {
			client := &fakeCompactionClient{}
			var mu sync.Mutex
			var events []Event
			engine := mustNewTestEngine(t, mustCreateTestSession(t), client, newTestToolRegistry(t), Config{
				Model: "gpt-6-sol", CompactionMode: "local",
				OnEvent: func(event Event) {
					mu.Lock()
					defer mu.Unlock()
					events = append(events, event)
				},
			})
			completeManualEligibilityAgentStep(t, engine)
			release := sync.OnceFunc(pendingWorkTestHoldStep(t, engine, ActiveKindUserTurn))
			t.Cleanup(release)
			reached, proceed := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(proceed) })
			t.Cleanup(unblock)
			steps := engine.stepLifecycle
			engine.compactionFlow.(*defaultContextCompactor).steps = pendingRemovalSteps{
				exclusiveStepLifecycle: steps,
				runNext: func(ctx context.Context, options exclusiveStepOptions, fn func(context.Context, string) error) error {
					if timing == "before entry" {
						close(reached)
						<-proceed
					} else {
						ctx = &pendingRemovalObservedContext{Context: ctx, observed: reached}
					}
					return steps.RunNext(ctx, options, fn)
				},
			}
			before := engine.ChatSnapshot()
			id := schedulePendingManualCompaction(t, engine)
			pendingWorkTestWait(t, reached, timing)
			restored, err := engine.RemovePendingWork(t.Context(), id)
			pendingWorkTestNoError(t, err)
			if restored.Kind != runtimeinput.PendingWorkItemKindManualCompaction || restored.CanonicalInput != "/compact" {
				t.Fatalf("removal response = %+v", restored)
			}
			if pendingWorkTestContains(pendingWorkTestSnapshot(t, engine), id) {
				t.Fatal("removed operation remains pending")
			}
			unblock()
			release()
			waitEngineLifecycleTasks(t, engine)
			client.mu.Lock()
			defer client.mu.Unlock()
			if len(client.calls) != 0 || len(client.compactionCalls) != 0 {
				t.Fatal("removed compaction reached provider execution")
			}
			after := engine.ChatSnapshot()
			if len(after.Entries) != len(before.Entries) || after.StreamingError != "" {
				t.Fatalf("removed operation produced chat feedback: %+v", after)
			}
			mu.Lock()
			defer mu.Unlock()
			for _, event := range events {
				switch event.Kind {
				case EventCompactionStarted, EventCompactionCompleted, EventCompactionFailed,
					EventStreamingErrorUpdated, EventLocalEntryAdded, EventPendingWorkRestored:
					t.Errorf("removed operation emitted %s", event.Kind)
				}
			}
		})
	}
}

func TestRemovedPendingWorktreeDoesNotExecute(t *testing.T) {
	engine := pendingWorkTestEngine(t, Config{Model: "gpt-6-sol"})
	release := sync.OnceFunc(pendingWorkTestHoldStep(t, engine, ActiveKindUserTurn))
	t.Cleanup(release)
	before := engine.ChatSnapshot()
	var mu sync.Mutex
	var events []Event
	engine.cfg.OnEvent = func(event Event) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	}
	executed := make(chan struct{}, 1)
	operationID := clientui.NewWorktreeTransitionID()
	_, err := engine.ScheduleWorktreeTransition(t.Context(), operationID,
		runtimeinput.PendingWorkWorktreeTransition{Transition: runtimeinput.PendingWorkWorktreeTransitionLeave},
		func(context.Context) error { executed <- struct{}{}; return nil })
	pendingWorkTestNoError(t, err)
	id, err := runtimeids.ParseQueueItemID(operationID.String())
	pendingWorkTestNoError(t, err)
	restored, err := engine.RemovePendingWork(t.Context(), id)
	pendingWorkTestNoError(t, err)
	if restored.Kind != runtimeinput.PendingWorkItemKindWorktreeTransition || restored.CanonicalInput != "/wt leave" {
		t.Fatalf("removal response = %+v", restored)
	}
	if pendingWorkTestContains(pendingWorkTestSnapshot(t, engine), id) {
		t.Fatal("removed Worktree remains pending")
	}
	release()
	waitEngineLifecycleTasks(t, engine)
	if len(executed) != 0 {
		t.Fatal("removed Worktree transition executed")
	}
	after := engine.ChatSnapshot()
	if len(after.Entries) != len(before.Entries) || after.StreamingError != "" {
		t.Fatalf("removed Worktree produced chat feedback: %+v", after)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, event := range events {
		switch event.Kind {
		case EventStreamingErrorUpdated, EventLocalEntryAdded, EventPendingWorkRestored:
			t.Errorf("removed Worktree emitted %s", event.Kind)
		}
	}
}
