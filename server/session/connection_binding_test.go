package session_test

import (
	"path/filepath"
	"testing"

	"core/server/metadata"
	"core/server/session"
	"core/shared/sessioncontract"
)

func TestSessionConnectionBindingSurvivesReopen(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	persistence, err := metadata.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = persistence.Close() })
	binding, err := persistence.RegisterWorkspaceBinding(t.Context(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	store, err := session.Create(
		filepath.Join(root, "projects", binding.ProjectID, "sessions"),
		"sessions", workspace, sessioncontract.SessionCategoryMain,
		persistence.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetConnectionID("work"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkModelDispatchLocked(session.LockedContract{Model: "original-model"}); err != nil {
		t.Fatal(err)
	}
	if err := persistence.Close(); err != nil {
		t.Fatal(err)
	}
	reopenedPersistence, err := metadata.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopenedPersistence.Close() })
	reopened, err := session.Open(store.Dir(), reopenedPersistence.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := reopened.Meta()
	if snapshot.ConnectionID == nil || *snapshot.ConnectionID != "work" || snapshot.Locked.Model != "original-model" {
		t.Fatalf("saved connection and model contract = %+v", snapshot)
	}
	*snapshot.ConnectionID = "mutated-copy"
	if *reopened.Meta().ConnectionID != "work" {
		t.Fatal("snapshot mutation changed the saved binding")
	}
}
