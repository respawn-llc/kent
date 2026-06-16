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
		SessionID: sessionID,
		SourceID:  "req-1",
		Text:      "first",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("record first: %v", err)
	}
	if !inserted {
		t.Fatal("expected first insert")
	}
	second, inserted, err := store.RecordPromptHistoryEntry(ctx, PromptHistoryEntry{
		SessionID: sessionID,
		SourceID:  "req-2",
		Text:      "second",
		CreatedAt: now,
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
		SessionID: sessionID,
		SourceID:  "req-1",
		Text:      "/status",
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

func TestQueuedPromptHistoryRecordsPlainPromptRow(t *testing.T) {
	ctx := context.Background()
	store, cfg, binding := newMetadataTestStore(t)
	sessionID := createMetadataTestSession(t, store, cfg, binding).Meta().SessionID

	record, inserted, err := store.RecordPromptHistoryEntry(ctx, PromptHistoryEntry{
		SessionID: sessionID,
		SourceID:  "req-queue-1",
		Text:      "queued text",
	})
	if err != nil {
		t.Fatalf("record queued: %v", err)
	}
	if !inserted {
		t.Fatal("expected queued prompt insert")
	}
	if record.SessionID != sessionID || record.SourceID != "req-queue-1" || record.Text != "queued text" {
		t.Fatalf("queued record = %+v", record)
	}

	history, err := store.ReadPromptHistory(ctx, sessionID)
	if err != nil {
		t.Fatalf("read prompt history: %v", err)
	}
	if !reflect.DeepEqual(history, []string{"queued text"}) {
		t.Fatalf("history = %+v", history)
	}
}
