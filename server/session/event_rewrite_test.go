package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAnalyzeAndRewriteEventsDropsEditsAndAppendsExtraEvent(t *testing.T) {
	store := newSessionTestStore(t)
	if _, _, err := store.AppendEvent("s1", "message", map[string]any{"role": "user", "content": "hello"}); err != nil {
		t.Fatalf("append event 1: %v", err)
	}
	if _, _, err := store.AppendEvent("s2", "drop_me", map[string]any{"drop": true}); err != nil {
		t.Fatalf("append event 2: %v", err)
	}
	if _, _, err := store.AppendEvent("s3", "message", map[string]any{"role": "assistant", "content": "old"}); err != nil {
		t.Fatalf("append event 3: %v", err)
	}
	store.pendingFsyncWrites = 7
	store.writesSinceCompaction = 9

	seen := make([]string, 0)
	result, committed, err := store.AnalyzeAndRewriteEvents(
		"repair-step",
		func(evt Event) error {
			seen = append(seen, evt.Kind)
			return nil
		},
		func(evt Event) (EventRewriteDecision, error) {
			if evt.Kind == "drop_me" {
				return EventRewriteDecision{Drop: true}, nil
			}
			if evt.Seq == 3 {
				payload := mustFixtureJSON(t, map[string]any{"role": "assistant", "content": "edited"})
				evt.Payload = payload
			}
			return EventRewriteDecision{Event: evt}, nil
		},
		func() ([]EventInput, error) {
			return []EventInput{{Kind: "local_entry", Payload: map[string]any{"message": "rewritten"}}}, nil
		},
	)
	if err != nil {
		t.Fatalf("rewrite events: %v", err)
	}
	if !committed {
		t.Fatal("expected committed rewrite")
	}
	if !result.Changed {
		t.Fatal("expected changed result")
	}
	if result.OldLastSequence != 3 {
		t.Fatalf("old last sequence = %d, want 3", result.OldLastSequence)
	}
	if result.LastSequence != 4 {
		t.Fatalf("last sequence = %d, want 4", result.LastSequence)
	}
	if len(result.AppendedEvents) != 1 || result.AppendedEvents[0].Seq != 4 || result.AppendedEvents[0].StepID != "repair-step" {
		t.Fatalf("unexpected appended events: %+v", result.AppendedEvents)
	}
	if got := store.Meta().LastSequence; got != 4 {
		t.Fatalf("meta last sequence = %d, want 4", got)
	}
	if store.pendingFsyncWrites != 0 || store.writesSinceCompaction != 0 {
		t.Fatalf("rewrite should reset counters, got pending=%d writes=%d", store.pendingFsyncWrites, store.writesSinceCompaction)
	}
	if store.ConversationFreshness().IsFresh() {
		t.Fatal("expected freshness to recompute from remaining visible user message as established")
	}
	if len(seen) != 3 || seen[0] != "message" || seen[1] != "drop_me" || seen[2] != "message" {
		t.Fatalf("unexpected analysis order: %+v", seen)
	}

	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events len = %d, want 3", len(events))
	}
	if events[0].Seq != 1 || events[1].Seq != 3 || events[2].Seq != 4 {
		t.Fatalf("expected sequence gap and appended high-water event, got %+v", events)
	}
	var edited map[string]any
	if err := json.Unmarshal(events[1].Payload, &edited); err != nil {
		t.Fatalf("decode edited payload: %v", err)
	}
	if edited["content"] != "edited" {
		t.Fatalf("edited content = %v, want edited", edited["content"])
	}
}

func TestAnalyzeAndRewriteEventsNoopDoesNotReplaceLog(t *testing.T) {
	store := newSessionTestStore(t)
	if _, _, err := store.AppendEvent("s1", "message", map[string]any{"role": "assistant", "content": "unchanged"}); err != nil {
		t.Fatalf("append event: %v", err)
	}
	before, err := os.Stat(filepath.Join(store.Dir(), eventsFile))
	if err != nil {
		t.Fatalf("stat events file before rewrite: %v", err)
	}

	result, committed, err := store.AnalyzeAndRewriteEvents(
		"noop",
		nil,
		func(evt Event) (EventRewriteDecision, error) {
			return EventRewriteDecision{Event: evt}, nil
		},
		nil,
	)
	if err != nil {
		t.Fatalf("rewrite events: %v", err)
	}
	if committed {
		t.Fatal("expected noop rewrite to avoid commit")
	}
	if result.Changed {
		t.Fatal("expected noop rewrite to report unchanged")
	}
	after, err := os.Stat(filepath.Join(store.Dir(), eventsFile))
	if err != nil {
		t.Fatalf("stat events file after rewrite: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
		t.Fatalf("expected noop rewrite not to replace events file")
	}
}

func TestAnalyzeAndRewriteEventsReturnsCommittedWhenObserverFailsAfterReplacement(t *testing.T) {
	observerErr := errors.New("observer failed")
	observer := &recordingPersistenceObserver{}
	store, err := Create(t.TempDir(), "workspace-x", "/tmp/work", WithPersistenceObserver(observer))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if _, _, err := store.AppendEvent("s1", "message", map[string]any{"role": "assistant", "content": "old"}); err != nil {
		t.Fatalf("append event: %v", err)
	}
	observer.err = observerErr

	result, committed, err := store.AnalyzeAndRewriteEvents(
		"repair",
		nil,
		func(evt Event) (EventRewriteDecision, error) {
			evt.Kind = "edited"
			return EventRewriteDecision{Event: evt}, nil
		},
		nil,
	)
	if !committed {
		t.Fatal("expected committed=true after events file replacement")
	}
	if !errors.Is(err, observerErr) {
		t.Fatalf("expected observer error, got %v", err)
	}
	if !result.Changed || result.LastSequence != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	events, readErr := store.ReadEvents()
	if readErr != nil {
		t.Fatalf("read events after observer failure: %v", readErr)
	}
	if len(events) != 1 || events[0].Kind != "edited" {
		t.Fatalf("expected committed edited event despite observer failure, got %+v", events)
	}
}

func TestAnalyzeAndRewriteEventsPreservesHighWaterSequenceWhenDroppingTail(t *testing.T) {
	store := newSessionTestStore(t)
	for i := 0; i < 3; i++ {
		if _, _, err := store.AppendEvent("s", "message", map[string]any{"role": "assistant", "content": "event"}); err != nil {
			t.Fatalf("append event %d: %v", i+1, err)
		}
	}

	result, committed, err := store.AnalyzeAndRewriteEvents(
		"repair",
		nil,
		func(evt Event) (EventRewriteDecision, error) {
			if evt.Seq == 3 {
				return EventRewriteDecision{Drop: true}, nil
			}
			return EventRewriteDecision{Event: evt}, nil
		},
		nil,
	)
	if err != nil {
		t.Fatalf("rewrite events: %v", err)
	}
	if !committed || !result.Changed {
		t.Fatalf("expected committed change, got result=%+v committed=%v", result, committed)
	}
	if result.LastSequence != 3 || store.Meta().LastSequence != 3 {
		t.Fatalf("expected high-water sequence 3, got result=%d meta=%d", result.LastSequence, store.Meta().LastSequence)
	}
	next, _, err := store.AppendEvent("s", "message", map[string]any{"role": "assistant", "content": "next"})
	if err != nil {
		t.Fatalf("append after rewrite: %v", err)
	}
	if next.Seq != 4 {
		t.Fatalf("next sequence = %d, want 4", next.Seq)
	}
}

func TestAnalyzeAndRewriteEventsHoldsStoreLockAcrossAnalysisAndRewrite(t *testing.T) {
	store := newSessionTestStore(t)
	if _, _, err := store.AppendEvent("s1", "message", map[string]any{"role": "assistant", "content": "old"}); err != nil {
		t.Fatalf("append event: %v", err)
	}

	appendDone := make(chan error, 1)
	var once sync.Once
	result, committed, err := store.AnalyzeAndRewriteEvents(
		"repair",
		func(Event) error {
			once.Do(func() {
				go func() {
					_, _, appendErr := store.AppendEvent("blocked", "message", map[string]any{"role": "user", "content": "later"})
					appendDone <- appendErr
				}()
				select {
				case err := <-appendDone:
					t.Fatalf("append acquired store lock during rewrite callback: %v", err)
				case <-time.After(25 * time.Millisecond):
				}
			})
			return nil
		},
		func(evt Event) (EventRewriteDecision, error) {
			evt.Kind = "edited"
			return EventRewriteDecision{Event: evt}, nil
		},
		nil,
	)
	if err != nil {
		t.Fatalf("rewrite events: %v", err)
	}
	if !committed || !result.Changed {
		t.Fatalf("expected committed change, got result=%+v committed=%v", result, committed)
	}
	select {
	case err := <-appendDone:
		if err != nil {
			t.Fatalf("append after rewrite: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("append did not complete after rewrite released store lock")
	}
}

func TestAnalyzeAndRewriteEventsReturnsAnalysisAndExtraEventErrorsBeforeCommit(t *testing.T) {
	store := newSessionTestStore(t)
	if _, _, err := store.AppendEvent("s1", "message", map[string]any{"role": "assistant", "content": "old"}); err != nil {
		t.Fatalf("append event: %v", err)
	}
	analysisErr := errors.New("analysis failed")
	if _, committed, err := store.AnalyzeAndRewriteEvents("repair", func(Event) error {
		return analysisErr
	}, nil, nil); !errors.Is(err, analysisErr) || committed {
		t.Fatalf("expected analysis error before commit, got committed=%v err=%v", committed, err)
	}
	extraErr := errors.New("extra failed")
	if _, committed, err := store.AnalyzeAndRewriteEvents("repair", nil, func(evt Event) (EventRewriteDecision, error) {
		evt.Kind = "edited"
		return EventRewriteDecision{Event: evt}, nil
	}, func() ([]EventInput, error) {
		return nil, extraErr
	}); !errors.Is(err, extraErr) || committed {
		t.Fatalf("expected extra-event error before commit, got committed=%v err=%v", committed, err)
	}
	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 1 || events[0].Kind != "message" {
		t.Fatalf("expected no committed changes, got %+v", events)
	}
}

type failingRewriteObserver struct {
	err error
}

func (f failingRewriteObserver) ObservePersistedStore(context.Context, PersistedStoreSnapshot) error {
	return f.err
}
