package session_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"core/internal/testharness/databaseseed"
	"core/server/metadata"
	"core/server/session"
	"core/shared/sessioncontract"
)

type failingSessionCommitObserver struct {
	store                *metadata.Store
	failure              error
	afterCommitFailure   error
	unknownCommitFailure error
	calls                int
}

func (o *failingSessionCommitObserver) ObservePersistedStore(ctx context.Context, snapshot session.PersistedStoreSnapshot) (session.CommitReceipt, error) {
	o.calls++
	if o.failure != nil {
		return session.CommitReceipt{}, session.DefinitelyUncommittedMutation(o.failure)
	}
	if err := o.store.ImportSessionSnapshot(ctx, snapshot); err != nil {
		return session.CommitReceipt{}, err
	}
	if o.unknownCommitFailure != nil {
		return session.CommitReceipt{}, o.unknownCommitFailure
	}
	return session.CommitReceipt{Committed: true}, o.afterCommitFailure
}

func TestUnknownCommitStopsFurtherSessionMutations(t *testing.T) {
	t.Setenv("KENT_DEBUG", "0")
	t.Setenv("KENT_INVARIANT_MODE", "diagnostic")
	root, workspace := t.TempDir(), t.TempDir()
	db, err := metadata.NewStore(root, databaseseed.OpenCurrentMetadataDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := db.RegisterWorkspaceBinding(t.Context(), workspace); err != nil {
		t.Fatal(err)
	}
	observer := &failingSessionCommitObserver{store: db}
	options := append(db.AuthoritativeSessionStoreOptions(), session.WithPersistenceObserver(observer))
	store, err := session.Create(filepath.Join(root, "sessions"), "sessions", workspace, sessioncontract.SessionCategoryMain, options...)
	if err != nil {
		t.Fatal(err)
	}
	log, err := store.MaterializeEventLog()
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("commit acknowledgement unavailable")
	observer.unknownCommitFailure = cause
	name := "committed despite uncertain acknowledgement"
	if err := store.SetName(&name); !errors.Is(err, cause) {
		t.Fatalf("unknown commit error: %v", err)
	} else {
		var terminal *session.IndeterminatePersistenceError
		if !errors.As(err, &terminal) {
			t.Fatalf("unknown outcome lacks terminal persistence classification: %v", err)
		}
	}
	calls := observer.calls
	next := "must not overwrite"
	if err := store.SetName(&next); !errors.Is(err, cause) {
		t.Fatalf("metadata mutation after unknown commit: %v", err)
	}
	if err := store.SetSessionContextFacts(1, true); !errors.Is(err, cause) {
		t.Fatalf("context mutation after unknown commit: %v", err)
	}
	if err := store.SetManualCompactEligibility(true); !errors.Is(err, cause) {
		t.Fatalf("eligibility mutation after unknown commit: %v", err)
	}
	content := "must not append"
	if _, receipt, err := log.AppendRecord(nil, session.MessageRecord{Role: session.MessageRoleUser, Content: &content}); !errors.Is(err, cause) || receipt.Committed {
		t.Fatalf("append after unknown commit: receipt=%+v error=%v", receipt, err)
	}
	if observer.calls != calls {
		t.Fatal("terminal Store attempted another persistence operation")
	}
	reopened, err := session.Open(store.Dir(), db.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Meta().Name == nil || *reopened.Meta().Name != name || reopened.Meta().LastSequence != 0 {
		t.Fatalf("fresh Store did not retain authoritative committed metadata: %+v", reopened.Meta())
	}
}

func TestReopenDoesNotCommitFailedResultGroup(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	metadataStore, err := metadata.NewStore(root, databaseseed.OpenCurrentMetadataDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := metadataStore.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := metadataStore.RegisterWorkspaceBinding(t.Context(), workspace); err != nil {
		t.Fatal(err)
	}
	observer := &failingSessionCommitObserver{store: metadataStore}
	options := append(metadataStore.AuthoritativeSessionStoreOptions(), session.WithPersistenceObserver(observer))
	store, err := session.Create(filepath.Join(root, "sessions"), "sessions", workspace, sessioncontract.SessionCategoryMain, options...)
	if err != nil {
		t.Fatal(err)
	}
	log, err := store.MaterializeEventLog()
	if err != nil {
		t.Fatal(err)
	}
	content := "committed"
	first, receipt, err := log.AppendRecord(nil, session.MessageRecord{Role: session.MessageRoleUser, Content: &content})
	if err != nil || !receipt.Committed {
		t.Fatalf("commit first record: receipt=%+v error=%v", receipt, err)
	}
	store, err = session.Open(store.Dir(), options...)
	if err != nil {
		t.Fatal(err)
	}
	log, err = store.MaterializeEventLog()
	if err != nil {
		t.Fatal(err)
	}
	observer.failure = errors.New("metadata transaction rejected")
	staleView, err := session.ResolvePersistedSessionView(t.Context(), metadataStore, store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	_, failedReceipt, appendErr := log.AppendRecordBatchAtomic([]session.EventRecordAppendInput{
		{Payload: session.MessageRecord{Role: session.MessageRoleAssistant, Content: &content}},
		{Payload: session.MessageRecord{Role: session.MessageRoleUser, Content: &content}},
	})
	if !errors.Is(appendErr, observer.failure) {
		t.Fatalf("append error=%v, want rejected metadata transaction", appendErr)
	}
	if failedReceipt.Committed {
		t.Error("result group committed before its metadata transaction")
	}
	reopened, err := session.Open(store.Dir(), metadataStore.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	reopenedLog, err := reopened.MaterializeEventLog()
	if err != nil {
		t.Fatal(err)
	}
	records, err := reopenedLog.ReadRecentRecords(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records.Records) != 1 || records.Records[0].Seq() != first.Seq() || reopened.Meta().LastSequence != first.Seq() {
		t.Fatalf("reopen promoted a rejected group: records=%d last_sequence=%d, want committed sequence=%d", len(records.Records), reopened.Meta().LastSequence, first.Seq())
	}
	observer.failure = nil
	observer.afterCommitFailure = errors.New("delivery failed after metadata commit")
	committed, receipt, err := log.AppendRecord(nil, session.MessageRecord{Role: session.MessageRoleAssistant, Content: &content})
	if !receipt.Committed || !errors.Is(err, observer.afterCommitFailure) {
		t.Fatalf("postcommit failure: receipt=%+v error=%v", receipt, err)
	}
	reopened, err = session.Open(store.Dir(), metadataStore.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	reopenedLog, err = reopened.MaterializeEventLog()
	if err != nil {
		t.Fatal(err)
	}
	records, err = reopenedLog.ReadRecentRecords(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records.Records) != 2 || records.Records[1].Seq() != committed.Seq() {
		t.Fatalf("postcommit failure lost or duplicated history: %+v", records)
	}
	stale, err := staleView.ReadNewestSegmentBackward(nil)
	if err != nil || len(stale.Records) != 1 || stale.Records[0].Seq() != first.Seq() {
		t.Fatalf("captured persisted view exposed a later group: %+v, %v", stale, err)
	}
	eventsPath := filepath.Join(store.Dir(), "events.jsonl")
	prefix, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	fp, err := os.OpenFile(eventsPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := fp.Write([]byte("{"))
	if err := errors.Join(writeErr, fp.Close()); err != nil {
		t.Fatal(err)
	}
	tornView, err := session.ResolvePersistedSessionView(t.Context(), metadataStore, store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	visible, err := tornView.ReadNewestSegmentBackward(nil)
	if err != nil || len(visible.Records) != 2 {
		t.Fatalf("uncommitted torn suffix affected committed history: %+v, %v", visible, err)
	}
	observer.afterCommitFailure = nil
	if _, receipt, err := log.AppendRecord(nil, session.MessageRecord{Role: session.MessageRoleUser, Content: &content}); err != nil || !receipt.Committed {
		t.Fatalf("append after torn suffix: receipt=%+v error=%v", receipt, err)
	}
	appended, err := os.ReadFile(eventsPath)
	if err != nil || len(appended) < len(prefix) || !bytes.Equal(appended[:len(prefix)], prefix) {
		t.Fatalf("discarding uncommitted suffix changed committed prefix: %v", err)
	}
	// Missing committed bytes are corruption, not permission to roll the
	// database backward or discard older history.
	if err := os.Truncate(eventsPath, int64(len(prefix))); err != nil {
		t.Fatal(err)
	}
	damaged, err := session.Open(store.Dir(), metadataStore.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := damaged.MaterializeEventLog(); err == nil {
		t.Fatal("missing committed boundary was silently accepted")
	}
	retained, err := os.ReadFile(eventsPath)
	if err != nil || !bytes.Equal(retained, prefix) {
		t.Fatalf("failed boundary validation changed prior history: %v", err)
	}
	if damaged.Meta().LastSequence != committed.Seq()+1 {
		t.Fatal("missing boundary caused committed database state to roll backward")
	}
}
