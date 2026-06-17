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

func TestQueuedUserMessageStatusEventsCoverAcceptedSubmittedAndFailed(t *testing.T) {
	store := mustCreateTestSession(t)
	client := &fakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "after queued steer"},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
	var statuses []QueuedUserMessageStatusEvent
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model: "gpt-5",
		OnEvent: func(evt Event) {
			if evt.QueuedUserMessageStatus != nil {
				statuses = append(statuses, *evt.QueuedUserMessageStatus)
			}
		},
	})

	first := eng.QueueUserMessageWithClientRequestID("steer now", "req-1")
	if _, err := eng.SubmitQueuedUserMessages(context.Background()); err != nil {
		t.Fatalf("SubmitQueuedUserMessages: %v", err)
	}
	second := eng.QueueUserMessageWithClientRequestID("restore me", "req-2")
	failed := eng.FailQueuedUserMessages(QueuedUserMessageFailureClosing)

	if len(failed) != 1 || failed[0].ID != second.ID {
		t.Fatalf("failed queued messages = %+v, want second queue item", failed)
	}
	want := []QueuedUserMessageStatus{
		QueuedUserMessageAccepted,
		QueuedUserMessageSubmitted,
		QueuedUserMessageAccepted,
		QueuedUserMessageFailed,
	}
	if len(statuses) != len(want) {
		t.Fatalf("statuses = %+v, want %d events", statuses, len(want))
	}
	for i, status := range want {
		if statuses[i].Status != status {
			t.Fatalf("status[%d] = %q, want %q in %+v", i, statuses[i].Status, status, statuses)
		}
	}
	if statuses[0].QueueItemID != first.ID || statuses[0].ClientRequestID != "req-1" {
		t.Fatalf("accepted status = %+v, want first id/client request", statuses[0])
	}
	if statuses[1].QueueItemID != first.ID || statuses[1].ClientRequestID != "req-1" {
		t.Fatalf("submitted status = %+v, want first id/client request", statuses[1])
	}
	if statuses[3].QueueItemID != second.ID || statuses[3].ClientRequestID != "req-2" || statuses[3].RestoreText != "restore me" || statuses[3].FailureReason != QueuedUserMessageFailureClosing {
		t.Fatalf("failed status = %+v, want correlated restore", statuses[3])
	}
}

func TestQueuedUserMessagesCoalesceFromStoredSteeringIntents(t *testing.T) {
	pending := []queuedUserSteeringIntent{
		{
			message: QueuedUserMessage{ID: "queue-1", Text: "stale metadata"},
			intent:  steerMessagesWithPersistenceIntent(steeringPriorityUser, steeringMessageEventNone, true, []llm.Message{{Role: llm.RoleUser, Content: "intent text"}}),
		},
		{
			message: QueuedUserMessage{ID: "queue-2"},
			intent:  steerMessagesWithPersistenceIntent(steeringPriorityUser, steeringMessageEventNone, true, []llm.Message{{Role: llm.RoleUser, Content: "second intent"}}),
		},
	}

	messages := normalizeQueuedUserMessages(pending)
	if len(messages) != 2 || messages[0] != "intent text" || messages[1] != "second intent" {
		t.Fatalf("queued messages = %+v, want stored intent content", messages)
	}
	items := queuedUserMessagesForFlush(pending)
	if len(items) != 2 || items[0].ID != "queue-1" || items[1].ID != "queue-2" {
		t.Fatalf("queued message items = %+v, want ids for non-empty stored intents", items)
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

func TestDrainQueuedUserMessagesBeforeCloseProcessesQueuedSteeringAfterFinalAnswer(t *testing.T) {
	store := mustCreateTestSession(t)
	client := &fakeClient{responses: []llm.Response{
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "ack queued steer", Phase: llm.MessagePhaseFinal},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
	}}
	var statuses []QueuedUserMessageStatusEvent
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model: "gpt-5",
		OnEvent: func(evt Event) {
			if evt.QueuedUserMessageStatus != nil {
				statuses = append(statuses, *evt.QueuedUserMessageStatus)
			}
		},
	})
	if _, err := eng.SubmitUserMessage(context.Background(), "initial"); err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}
	queued := eng.QueueUserMessageWithClientRequestID("queued steer", "req-queued")
	if err := eng.DrainQueuedUserMessagesBeforeClose(context.Background()); err != nil {
		t.Fatalf("DrainQueuedUserMessagesBeforeClose: %v", err)
	}
	if len(client.calls) != 2 {
		t.Fatalf("model calls = %d, want initial plus queued drain", len(client.calls))
	}
	hasQueuedUser := false
	for _, message := range requestMessages(client.calls[1]) {
		if message.Role == llm.RoleUser && message.Content == "queued steer" {
			hasQueuedUser = true
			break
		}
	}
	if !hasQueuedUser {
		t.Fatalf("expected drained request to include queued user message, got %+v", requestMessages(client.calls[1]))
	}
	if len(statuses) != 2 || statuses[0].Status != QueuedUserMessageAccepted || statuses[1].Status != QueuedUserMessageSubmitted || statuses[1].QueueItemID != queued.ID || statuses[1].ClientRequestID != "req-queued" {
		t.Fatalf("queued statuses = %+v, want accepted then submitted for %q", statuses, queued.ID)
	}
}

func TestDrainQueuedUserMessagesBeforeCloseFailsRestoredQueueWhenFlushPersistenceFails(t *testing.T) {
	store := mustCreateTestSession(t)
	client := &fakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "unused"},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
	persistErr := errors.New("persist queued flush")
	var statuses []QueuedUserMessageStatusEvent
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model: "gpt-5",
		OnEvent: func(evt Event) {
			if evt.QueuedUserMessageStatus != nil {
				statuses = append(statuses, *evt.QueuedUserMessageStatus)
			}
		},
	})
	eng.beforePersistMessage = func(msg llm.Message) error {
		if msg.Role == llm.RoleUser && msg.Content == "queued steer" {
			return persistErr
		}
		return nil
	}
	queued := eng.QueueUserMessageWithClientRequestID("queued steer", "req-queued")

	err := eng.DrainQueuedUserMessagesBeforeClose(context.Background())
	if !errors.Is(err, persistErr) {
		t.Fatalf("DrainQueuedUserMessagesBeforeClose error = %v, want %v", err, persistErr)
	}
	if eng.HasQueuedUserWork() {
		t.Fatal("queued user work remained after close-drain failure")
	}
	if len(statuses) != 2 || statuses[0].Status != QueuedUserMessageAccepted || statuses[1].Status != QueuedUserMessageFailed {
		t.Fatalf("queued statuses = %+v, want accepted then failed", statuses)
	}
	if statuses[1].QueueItemID != queued.ID || statuses[1].ClientRequestID != "req-queued" || statuses[1].RestoreText != "queued steer" || statuses[1].FailureReason != QueuedUserMessageFailureClosing {
		t.Fatalf("failed status = %+v, want correlated close failure restore", statuses[1])
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
