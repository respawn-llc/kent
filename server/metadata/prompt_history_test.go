package metadata

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestPromptHistoryRecordsAndListsByInsertionSequence(t *testing.T) {
	ctx := context.Background()
	store, cfg, binding := newMetadataTestStore(t)
	sessionID := createMetadataTestSession(t, store, cfg, binding).Meta().SessionID
	now := time.UnixMilli(123).UTC()

	first, inserted, err := store.RecordPromptHistoryEntry(ctx, PromptHistoryEntry{
		SessionID:       sessionID,
		Source:          PromptHistorySourceSubmitUserMessage,
		SourceID:        "req-1",
		ClientRequestID: "req-1",
		Text:            "first",
		CreatedAt:       now,
	})
	if err != nil {
		t.Fatalf("record first: %v", err)
	}
	if !inserted {
		t.Fatal("expected first insert")
	}
	second, inserted, err := store.RecordPromptHistoryEntry(ctx, PromptHistoryEntry{
		SessionID:       sessionID,
		Source:          PromptHistorySourceSubmitUserMessage,
		SourceID:        "req-2",
		ClientRequestID: "req-2",
		Text:            "second",
		CreatedAt:       now,
	})
	if err != nil {
		t.Fatalf("record second: %v", err)
	}
	if !inserted {
		t.Fatal("expected second insert")
	}
	if first.Sequence >= second.Sequence {
		t.Fatalf("sequences not increasing: first=%d second=%d", first.Sequence, second.Sequence)
	}

	history, err := store.ReadPromptHistory(ctx, sessionID)
	if err != nil {
		t.Fatalf("read prompt history: %v", err)
	}
	if !reflect.DeepEqual(history, []string{"first", "second"}) {
		t.Fatalf("history = %+v", history)
	}
}

func TestPromptHistoryConflictRequiresEquivalentPayload(t *testing.T) {
	ctx := context.Background()
	store, cfg, binding := newMetadataTestStore(t)
	sessionID := createMetadataTestSession(t, store, cfg, binding).Meta().SessionID
	entry := PromptHistoryEntry{
		SessionID:       sessionID,
		Source:          PromptHistorySourceRecordPromptHistory,
		SourceID:        "req-1",
		ClientRequestID: "req-1",
		Text:            "/status",
	}

	if _, _, err := store.RecordPromptHistoryEntry(ctx, entry); err != nil {
		t.Fatalf("record initial: %v", err)
	}
	_, inserted, err := store.RecordPromptHistoryEntry(ctx, entry)
	if err != nil {
		t.Fatalf("record equivalent replay: %v", err)
	}
	if inserted {
		t.Fatal("expected equivalent replay to return existing row")
	}

	entry.Text = "/resume"
	_, _, err = store.RecordPromptHistoryEntry(ctx, entry)
	if !errors.Is(err, ErrPromptHistoryConflict) {
		t.Fatalf("mismatched replay error = %v, want ErrPromptHistoryConflict", err)
	}
}

func TestQueuedPromptHistoryStateTransitionsPreserveHistory(t *testing.T) {
	ctx := context.Background()
	store, cfg, binding := newMetadataTestStore(t)
	sessionID := createMetadataTestSession(t, store, cfg, binding).Meta().SessionID

	record, inserted, err := store.RecordPromptHistoryEntry(ctx, PromptHistoryEntry{
		SessionID:       sessionID,
		Source:          PromptHistorySourceQueueUserMessage,
		SourceID:        "queue-1",
		ClientRequestID: "req-queue-1",
		QueueItemID:     "queue-1",
		QueueState:      PromptHistoryQueueStateRecorded,
		Text:            "queued text",
	})
	if err != nil {
		t.Fatalf("record queued: %v", err)
	}
	if !inserted || record.QueueState != PromptHistoryQueueStateRecorded {
		t.Fatalf("queued record inserted=%t state=%q", inserted, record.QueueState)
	}
	pending, changed, err := store.MarkPromptHistoryQueueState(ctx, sessionID, "queue-1", PromptHistoryQueueStatePending)
	if err != nil {
		t.Fatalf("mark pending: %v", err)
	}
	if !changed {
		t.Fatal("expected pending transition to report changed")
	}
	if pending.QueueState != PromptHistoryQueueStatePending {
		t.Fatalf("pending state = %q", pending.QueueState)
	}
	discarded, changed, err := store.MarkPromptHistoryQueueState(ctx, sessionID, "queue-1", PromptHistoryQueueStateDiscarded)
	if err != nil {
		t.Fatalf("mark discarded: %v", err)
	}
	if !changed {
		t.Fatal("expected discard transition to report changed")
	}
	if discarded.QueueState != PromptHistoryQueueStateDiscarded {
		t.Fatalf("discarded state = %q", discarded.QueueState)
	}
	alreadyDiscarded, changed, err := store.MarkPromptHistoryQueueState(ctx, sessionID, "queue-1", PromptHistoryQueueStateDiscarded)
	if err != nil {
		t.Fatalf("mark already discarded: %v", err)
	}
	if changed {
		t.Fatal("expected already-discarded transition to report unchanged")
	}
	if alreadyDiscarded.QueueState != PromptHistoryQueueStateDiscarded {
		t.Fatalf("already-discarded state = %q", alreadyDiscarded.QueueState)
	}

	history, err := store.ReadPromptHistory(ctx, sessionID)
	if err != nil {
		t.Fatalf("read prompt history: %v", err)
	}
	if !reflect.DeepEqual(history, []string{"queued text"}) {
		t.Fatalf("history after discard = %+v", history)
	}
}
