package metadata

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"core/server/sessionstore"
)

func TestOpeningDatabaseIgnoresObsoleteSessionJournal(t *testing.T) {
	cfg := loadMetadataTestConfig(t, t.TempDir(), filepath.Join(t.TempDir(), "persistence"))
	store, err := Open(cfg.PersistenceRoot)
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
	// Reopen from the released schema, without carrying this branch's startup
	// migration state into the fixture.
	if _, err := store.db.ExecContext(t.Context(), "DELETE FROM goose_db_version WHERE version_id > 101"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(cfg.PersistenceRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	record, err := reopened.ResolvePersistedSession(t.Context(), chat.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.LastSequence != sequence {
		t.Fatal("opening changed the committed transcript boundary")
	}
	opened, err := session.Open(chat.Dir(), reopened.AuthoritativeSessionStoreOptions()...)
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
