package testsetup

import (
	"database/sql"
	"path/filepath"
	"testing"

	"core/internal/testharness/databaseseed"
	"core/server/metadata"
	"core/server/sessionstore"
)

func OpenStore(t testing.TB, persistenceRoot string) *metadata.Store {
	t.Helper()
	return openStoreDatabase(t, persistenceRoot, databaseseed.OpenCurrentMetadataDatabase(t))
}

func StoreOpener(t testing.TB, persistenceRoot string) func() *metadata.Store {
	t.Helper()
	open := databaseseed.CurrentMetadataDatabaseOpener(t)
	return func() *metadata.Store {
		return openStoreDatabase(t, persistenceRoot, open())
	}
}

func openStoreDatabase(t testing.TB, persistenceRoot string, db *sql.DB) *metadata.Store {
	t.Helper()
	store, err := metadata.NewStore(persistenceRoot, db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("metadata.NewStore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close metadata store: %v", err)
		}
	})
	return store
}

func OpenSessionProjectWorkspaceContextReader(t testing.TB, sessionStore *session.Store) *metadata.Store {
	t.Helper()
	store := OpenStore(t, filepath.Dir(sessionStore.Dir()))
	ImportSessionProjectWorkspaceContext(t, store, sessionStore)
	return store
}

func ImportSessionProjectWorkspaceContext(
	t testing.TB,
	store *metadata.Store,
	sessionStore *session.Store,
) {
	t.Helper()
	if _, err := store.RegisterWorkspaceBinding(t.Context(), sessionStore.Meta().WorkspaceRoot); err != nil {
		t.Fatalf("register Session Project Workspace: %v", err)
	}
	if err := store.ImportSessionSnapshot(t.Context(), session.PersistedStoreSnapshot{
		SessionDir:   sessionStore.Dir(),
		Meta:         sessionStore.Meta(),
		ContextFacts: sessionStore.ContextFacts(),
	}); err != nil {
		t.Fatalf("import Session Project Workspace context: %v", err)
	}
}
