package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"core/server/llm"
	"core/server/tools"
)

func TestThinkingInputCommitFailureBoundaries(t *testing.T) {
	for _, queued := range []bool{false, true} {
		for _, providerFailure := range []bool{false, true} {
			name := "direct"
			if queued {
				name = "queued"
			}
			if providerFailure {
				name += "/provider"
			} else {
				name += "/append"
			}
			t.Run(name, func(t *testing.T) {
				store := mustCreateTestSession(t)
				client := &fakeClient{
					caps:      llm.ProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true, SupportsNativeThinkingUpdates: true},
					responses: []llm.Response{finalOutputItemResponse("seed")},
				}
				var flushed atomic.Int32
				var thinkingRows atomic.Int32
				var restorationMu sync.Mutex
				var restored []string
				engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
					Model: "gpt-6-astra", ThinkingLevel: "high",
					OnEvent: func(event Event) {
						if event.Kind == EventUserMessageFlushed {
							flushed.Add(1)
						}
						if event.LocalEntry != nil && event.LocalEntry.ThinkingEffort != nil {
							thinkingRows.Add(1)
						}
						if event.PendingWorkRestoration != nil {
							restorationMu.Lock()
							restored = append(restored, event.PendingWorkRestoration.CanonicalInput)
							restorationMu.Unlock()
						}
					},
				})
				if _, err := engine.SubmitUserMessage(t.Context(), "seed"); err != nil {
					t.Fatal(err)
				}
				if err := engine.SetThinkingLevel(t.Context(), "low"); err != nil {
					t.Fatal(err)
				}
				if providerFailure {
					client.errors = []error{&llm.ProviderAPIError{ProviderID: "openai", StatusCode: 400, Code: llm.UnifiedErrorCodeUnknown, ProviderCode: "request_failed"}}
				} else {
					mustBlockTestEventLogAppends(t, store)
				}
				var err error
				if queued {
					_, err = engine.Steer(t.Context(), "next", nil)
					if err == nil {
						err = engine.WaitForScheduledQueuedUserWork(t.Context())
					}
					waitEngineLifecycleTasks(t, engine)
				} else {
					_, err = engine.SubmitUserMessage(t.Context(), "next")
				}
				_, streamingError, _ := engine.transcriptRuntimeState().StreamingSnapshot()
				if err == nil && streamingError == "" {
					t.Fatal("failure was not surfaced")
				}
				updates, next := 0, 0
				for _, item := range engine.transcriptRuntimeState().SnapshotItems() {
					if item.Type == llm.ResponseItemTypeConfigurationUpdate {
						updates++
					}
					if item.Content != nil && *item.Content == "next" {
						next++
					}
				}
				want := 0
				if providerFailure {
					want = 1
				}
				if updates != want || next != want || int(flushed.Load()) != 1+want || fakeClientCallCount(client) != 1+want {
					t.Fatalf("updates=%d input=%d flushed=%d requests=%d", updates, next, flushed.Load(), fakeClientCallCount(client))
				}
				if int(thinkingRows.Load()) != want {
					t.Fatalf("Thinking rows = %d, want %d committed updates", thinkingRows.Load(), want)
				}
				if !providerFailure {
					restorationMu.Lock()
					got := append([]string(nil), restored...)
					restorationMu.Unlock()
					if len(got) != 1 || got[0] != "next" {
						t.Errorf("uncommitted input restoration = %v, want next exactly once", got)
					}
					if pending := pendingWorkTestSnapshot(t, engine); len(pending.Items) != 0 {
						t.Error("uncommitted required input remained pending")
					}
					if _, err := engine.SubmitUserMessage(t.Context(), "must not replay"); !errors.Is(err, ErrEngineClosed) {
						t.Errorf("Runtime admission after required write failure = %v, want closed", err)
					}
				}
			})
		}
	}
}

func TestStopAfterInputCommitPreservesSubmittedInput(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	client := &hookClient{response: finalOutputItemResponse("done"), beforeReturn: func() error {
		close(started)
		<-release
		return context.Canceled
	}}
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{})
	var flushed atomic.Int32
	done := make(chan error, 1)
	go func() {
		_, err := engine.SubmitUserMessageWithFlushHook(t.Context(), "saved input", func() { flushed.Add(1) })
		done <- err
	}()
	pendingWorkTestWait(t, started, "provider request after commitment")
	if flushed.Load() != 1 {
		t.Fatal("flush hook did not follow input commitment")
	}
	interrupted, err := engine.TryInterruptActiveRun()
	if err != nil || !interrupted {
		t.Fatalf("Stop = %v, %v", interrupted, err)
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("execution outcome = %v", err)
	}
	count := 0
	for _, message := range engine.transcriptRuntimeState().SnapshotMessages() {
		if message.Role == llm.RoleUser && message.Content != nil && *message.Content == "saved input" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("saved input after Stop = %d", count)
	}
}
