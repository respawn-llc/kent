package metadata

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"core/server/sessionstore"
)

func TestMetadataMigrationIgnoresObsoleteSessionJournal(t *testing.T) {
	cfg := loadMetadataTestConfig(t, t.TempDir(), filepath.Join(t.TempDir(), "persistence"))
	db, err := openDatabaseAtVersionForTest(t, 101)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(cfg.PersistenceRoot, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	binding, err := store.RegisterWorkspaceBinding(t.Context(), cfg.WorkspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	chat := createMetadataTestSession(t, store, cfg, binding)
	log, err := chat.MaterializeEventLog()
	if err != nil {
		t.Fatal(err)
	}
	content := "committed history"
	if _, _, err := log.AppendRecord(nil, session.MessageRecord{Role: session.MessageRoleUser, Content: &content}); err != nil {
		t.Fatal(err)
	}
	sequence := chat.Meta().LastSequence
	journalPath := filepath.Join(chat.Dir(), "append-recovery.json")
	obsolete := []byte("obsolete unreadable journal")
	if err := os.WriteFile(journalPath, obsolete, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(store.db); err != nil {
		t.Fatal(err)
	}
	record, err := store.ResolvePersistedSession(t.Context(), chat.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.LastSequence != sequence {
		t.Fatal("opening changed the committed transcript boundary")
	}
	opened, err := session.Open(chat.Dir(), store.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Meta().LastSequence != sequence {
		t.Fatal("opening the Session changed its committed transcript boundary")
	}
	after, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, obsolete) {
		t.Fatal("opening rewrote the obsolete journal")
	}
}
