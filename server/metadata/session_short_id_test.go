package metadata

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"core/server/session"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"
	"core/shared/textutil"
)

func TestSessionShortIDsAreStableAcrossUpdatesAndProjects(t *testing.T) {
	store, cfg, binding := newMetadataTestStore(t)
	first := createMetadataTestSession(t, store, cfg, binding)
	otherConfig := cfg
	otherConfig.WorkspaceRoot = t.TempDir()
	otherBinding, err := store.RegisterWorkspaceBinding(t.Context(), otherConfig.WorkspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	second := createMetadataTestSession(t, store, otherConfig, otherBinding)
	for index, sess := range []*session.Store{first, second} {
		record, err := store.ResolvePersistedSession(t.Context(), sess.Meta().SessionID)
		if err != nil {
			t.Fatal(err)
		}
		if record.ShortID == nil || record.ShortID.Int64() != int64(index+1) {
			t.Fatalf("assigned number = %v, want %d", record.ShortID, index+1)
		}
		if sess.Identity().String() != record.ShortID.String() {
			t.Fatalf("created Store identity = %s, want %s", sess.Identity(), record.ShortID)
		}
		projectID := binding.ProjectID
		if index == 1 {
			projectID = otherBinding.ProjectID
		}
		if err := sess.SetName(textutil.Value("listed session")); err != nil {
			t.Fatal(err)
		}
		page, err := store.ListSessionPage(t.Context(), projectID, sessioncontract.SessionCategoryMain, 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Sessions) != 1 || page.Sessions[0].SessionID != sess.Identity().ID ||
			page.Sessions[0].ShortID == nil || *page.Sessions[0].ShortID != *record.ShortID {
			t.Fatalf("listed identity = %+v, want %v", page.Sessions, sess.Identity())
		}
		for _, raw := range []string{record.ShortID.String(), "000" + record.ShortID.String(), sess.Meta().SessionID} {
			selector, err := runtimeids.ParseSessionSelector(raw)
			if err != nil {
				t.Fatal(err)
			}
			id, err := store.ResolveSessionSelector(t.Context(), selector)
			if err != nil || id.String() != sess.Meta().SessionID {
				t.Fatalf("resolve %s = %v, %v", raw, id, err)
			}
		}
		if err := sess.SetInputDraft("retained content", nil); err != nil {
			t.Fatal(err)
		}
		updated, err := store.ResolvePersistedSession(t.Context(), sess.Meta().SessionID)
		if err != nil {
			t.Fatal(err)
		}
		if updated.ShortID == nil || *updated.ShortID != *record.ShortID || updated.SessionDir != record.SessionDir || updated.Meta.InputDraft != "retained content" {
			t.Fatalf("update changed identity or lost content: %+v", updated)
		}
	}
}

func TestSessionShortIDsSurviveRestartAndAreNotReusedAfterDeletion(t *testing.T) {
	store, cfg, binding := newFileBackedMetadataTestStore(t)
	first := createMetadataTestSession(t, store, cfg, binding)
	record, err := store.ResolvePersistedSession(t.Context(), first.Meta().SessionID)
	if err != nil || record.ShortID == nil {
		t.Fatalf("created Session = %+v, %v", record, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(cfg.PersistenceRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	id, err := reopened.ResolveSessionSelector(t.Context(), *record.ShortID)
	if err != nil || id.String() != first.Meta().SessionID {
		t.Fatalf("restarted lookup = %v, %v", id, err)
	}
	if err := reopened.DeleteFailedSessionCreationRecordByID(t.Context(), id.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ResolveSessionSelector(t.Context(), *record.ShortID); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("deleted selector lookup = %v", err)
	}
	deleted, err := reopened.SessionIdentity(t.Context(), id)
	if err != nil || deleted.ID != id || deleted.ShortID != nil {
		t.Fatalf("deleted reference = %v, %v", deleted, err)
	}
	next := createMetadataTestSession(t, reopened, cfg, binding)
	nextRecord, err := reopened.ResolvePersistedSession(t.Context(), next.Meta().SessionID)
	if err != nil || nextRecord.ShortID == nil || nextRecord.ShortID.Int64() <= record.ShortID.Int64() {
		t.Fatalf("next Session reused deleted number: %+v, %v", nextRecord, err)
	}
}

func TestConcurrentSessionCreationAssignsUniqueNumbers(t *testing.T) {
	store, cfg, binding := newFileBackedMetadataTestStore(t)
	const count = 10
	type result struct {
		store *session.Store
		err   error
	}
	results := make(chan result, count)
	var group sync.WaitGroup
	for range count {
		group.Go(func() {
			sess, err := session.Create(
				filepath.Join(cfg.PersistenceRoot, "projects", binding.ProjectID, "sessions"),
				binding.WorkspaceName, cfg.WorkspaceRoot, sessioncontract.SessionCategoryMain,
				store.AuthoritativeSessionStoreOptions()...,
			)
			results <- result{sess, err}
		})
	}
	group.Wait()
	close(results)
	assigned := make(map[runtimeids.SessionShortID]bool, count)
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		record, err := store.ResolvePersistedSession(t.Context(), result.store.Meta().SessionID)
		if err != nil || record.ShortID == nil {
			t.Fatalf("created Session = %+v, %v", record, err)
		}
		if assigned[*record.ShortID] {
			t.Fatalf("duplicate number %s", record.ShortID)
		}
		assigned[*record.ShortID] = true
	}
}
