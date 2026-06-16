package runtime

import (
	"context"
	"errors"
	"testing"

	"core/server/llm"
	"core/server/tools"
)

func TestSubmitQueuedUserMessagesStartsTurnFromQueuedInjection(t *testing.T) {
	store := mustCreateTestSession(t)

	client := &fakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "after queued steer"},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}

	var flushed Event
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model: "gpt-5",
		OnEvent: func(evt Event) {
			if evt.Kind == EventUserMessageFlushed {
				flushed = evt
			}
		},
	})

	queued := eng.QueueUserMessage("steer now")

	msg, err := eng.SubmitQueuedUserMessages(context.Background())
	if err != nil {
		t.Fatalf("submit queued user messages: %v", err)
	}
	if msg.Content != "after queued steer" {
		t.Fatalf("assistant content = %q, want after queued steer", msg.Content)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected one model call for queued submission, got %d", len(client.calls))
	}
	if flushed.UserMessage != "steer now" {
		t.Fatalf("unexpected flushed user message %q", flushed.UserMessage)
	}
	if len(flushed.UserMessageBatchQueueItemIDs) != 1 || flushed.UserMessageBatchQueueItemIDs[0] != queued.ID {
		t.Fatalf("flushed queue ids = %+v, want [%q]", flushed.UserMessageBatchQueueItemIDs, queued.ID)
	}

	hasQueuedUser := false
	for _, message := range requestMessages(client.calls[0]) {
		if message.Role == llm.RoleUser && message.Content == "steer now" {
			hasQueuedUser = true
			break
		}
	}
	if !hasQueuedUser {
		t.Fatalf("expected first request to include queued user message, got %+v", requestMessages(client.calls[0]))
	}
}

func TestQueuedUserMessagesCoalesceFromStoredSteeringIntents(t *testing.T) {
	pending := []queuedUserSteeringIntent{
		{
			message: QueuedUserMessage{ID: "queue-1", Text: "stale metadata"},
			intent:  steerUserMessageWithoutDerivedEventIntent(llm.Message{Role: llm.RoleUser, Content: "intent text"}),
		},
		{
			message: QueuedUserMessage{ID: "queue-2"},
			intent:  steerUserMessageWithoutDerivedEventIntent(llm.Message{Role: llm.RoleUser, Content: "second intent"}),
		},
	}

	messages := normalizeQueuedUserMessages(pending)
	if len(messages) != 2 || messages[0] != "intent text" || messages[1] != "second intent" {
		t.Fatalf("queued messages = %+v, want stored intent content", messages)
	}
	ids := queuedUserMessageIDs(pending)
	if len(ids) != 2 || ids[0] != "queue-1" || ids[1] != "queue-2" {
		t.Fatalf("queued message ids = %+v, want ids for non-empty stored intents", ids)
	}
}

func TestSubmitQueuedUserMessagesRetriesTransientBusyErrors(t *testing.T) {
	store := mustCreateTestSession(t)

	client := &fakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "after queued steer"},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-5"})

	attempts := 0
	eng.stepLifecycle = &stubExclusiveStepLifecycle{runFn: func(ctx context.Context, options exclusiveStepOptions, fn func(stepCtx context.Context, stepID string) error) error {
		attempts++
		if attempts == 1 {
			return errExclusiveStepBusy
		}
		return fn(ctx, "stub-step")
	}}
	eng.QueueUserMessage("steer now")

	msg, err := eng.SubmitQueuedUserMessages(context.Background())
	if err != nil {
		t.Fatalf("submit queued user messages: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected busy retry before success, got %d attempts", attempts)
	}
	if msg.Content != "after queued steer" {
		t.Fatalf("assistant content = %q, want after queued steer", msg.Content)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected one model call after retry, got %d", len(client.calls))
	}
	hasQueuedUser := false
	for _, message := range requestMessages(client.calls[0]) {
		if message.Role == llm.RoleUser && message.Content == "steer now" {
			hasQueuedUser = true
			break
		}
	}
	if !hasQueuedUser {
		t.Fatalf("expected retried request to include queued user message, got %+v", requestMessages(client.calls[0]))
	}
}

func TestSubmitQueuedUserMessagesStopsRetryingWhenContextIsCanceled(t *testing.T) {
	store := mustCreateTestSession(t)

	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(), Config{Model: "gpt-5"})

	attempts := 0
	eng.stepLifecycle = &stubExclusiveStepLifecycle{runFn: func(ctx context.Context, options exclusiveStepOptions, fn func(stepCtx context.Context, stepID string) error) error {
		attempts++
		return errExclusiveStepBusy
	}}
	eng.QueueUserMessage("steer now")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	msg, err := eng.SubmitQueuedUserMessages(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got msg=%+v err=%v", msg, err)
	}
	if attempts != 1 {
		t.Fatalf("expected one busy attempt before cancellation, got %d", attempts)
	}
}

func TestHasQueuedUserWorkDetectsBackgroundNotices(t *testing.T) {
	store := mustCreateTestSession(t)

	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(), Config{Model: "gpt-5"})
	steps := &stubExclusiveStepLifecycle{busy: true}
	eng.stepLifecycle = steps
	eng.backgroundFlow = &defaultBackgroundNoticeScheduler{engine: eng, steps: steps}
	if eng.HasQueuedUserWork() {
		t.Fatal("did not expect queued work in fresh engine")
	}

	eng.HandleBackgroundShellUpdate(BackgroundShellEvent{ID: "42", Type: "completed", State: "done"}, true)
	if !eng.HasQueuedUserWork() {
		t.Fatal("expected queued work after background notice was queued")
	}
}
