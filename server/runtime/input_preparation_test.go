package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"core/server/llm"
	"core/server/tools"
	"core/shared/runtimeids"
)

type heldPromptPreparation struct {
	once    sync.Once
	started chan struct{}
	release chan struct{}
	err     error
}

func (p *heldPromptPreparation) ReloadPromptFacingSnapshotConfig(context.Context, string) (PromptFacingSnapshotConfig, error) {
	p.once.Do(func() {
		close(p.started)
		<-p.release
	})
	return PromptFacingSnapshotConfig{}, p.err
}

func TestInputRemainsPendingUntilPreparationCompletes(t *testing.T) {
	testInputPreparationOutcome(t, false)
}

func TestStopBeforeInputCommitReleasesClaim(t *testing.T) {
	testInputPreparationOutcome(t, true)
}

func testInputPreparationOutcome(t *testing.T, stop bool) {
	for _, submission := range []string{"synchronous", "queued", "agent"} {
		t.Run(submission, func(t *testing.T) {
			queued := submission == "queued"
			preparationErr := errors.New("preparation failed")
			if stop {
				preparationErr = nil
			}
			preparation := &heldPromptPreparation{started: make(chan struct{}), release: make(chan struct{}), err: preparationErr}
			client := &fakeClient{responses: []llm.Response{finalOutputItemResponse("done")}}
			var flushed atomic.Int32
			store := mustCreateTestSession(t)
			seed := mustNewTestEngine(t, store, &fakeClient{responses: []llm.Response{finalOutputItemResponse("seed")}}, tools.NewRegistry(), Config{})
			if _, err := seed.SubmitUserMessage(t.Context(), "seed"); err != nil {
				t.Fatal(err)
			}
			if err := seed.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := store.MarkLockedPromptFacingSnapshotsStale(); err != nil {
				t.Fatal(err)
			}
			engine := mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), client, tools.NewRegistry(), Config{
				PromptFacingSnapshotReloader: preparation,
				OnEvent: func(event Event) {
					if event.Kind == EventUserMessageFlushed {
						flushed.Add(1)
					}
				},
			})
			done := make(chan error, 1)
			go func() {
				if queued {
					_, err := engine.Steer(t.Context(), "pending input", nil)
					done <- err
				} else if submission == "agent" {
					steer, err := NewAgentSteer(runtimeids.NewSessionID(), "pending input")
					if err != nil {
						done <- err
						return
					}
					_, err = engine.SubmitAgentSteerWithHooks(t.Context(), steer, nil, func() { flushed.Add(1) })
					done <- err
				} else {
					_, err := engine.SubmitUserMessageWithHooks(t.Context(), "pending input", nil, func() { flushed.Add(1) })
					done <- err
				}
			}()
			pendingWorkTestWait(t, preparation.started, "request preparation")
			pending := pendingWorkTestSnapshot(t, engine)
			if len(pending.Items) != 1 || flushed.Load() != 0 {
				t.Errorf("during preparation: pending=%d flushed=%d", len(pending.Items), flushed.Load())
			}
			if stop {
				interrupted, err := engine.TryInterruptActiveRun()
				if err != nil || !interrupted {
					t.Fatalf("Stop = %v, %v", interrupted, err)
				}
				preparationErr = context.Canceled
			}
			close(preparation.release)
			err := <-done
			if !queued && !errors.Is(err, preparationErr) {
				t.Fatalf("submit error = %v", err)
			}
			waitEngineLifecycleTasks(t, engine)
			if flushed.Load() != 0 || fakeClientCallCount(client) != 0 {
				t.Fatal("failed preparation submitted input or called provider")
			}
			if stop {
				if pending := pendingWorkTestSnapshot(t, engine); len(pending.Items) != 0 {
					t.Fatalf("stopped claim remained pending: %+v", pending.Items)
				}
				if _, err := engine.SubmitUserMessage(t.Context(), "later independent input"); err != nil {
					t.Fatal(err)
				}
				if fakeClientCallCount(client) != 1 || flushed.Load() != 1 {
					t.Fatal("later input did not complete independently")
				}
			}
		})
	}
}
