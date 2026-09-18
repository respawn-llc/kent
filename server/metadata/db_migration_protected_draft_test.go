package metadata

import (
	"core/server/session"
	"path/filepath"
	"testing"
)

func TestProtectedDraftsPersistAcrossReopen(t *testing.T) {
	store, cfg, binding := newMetadataTestStore(t)
	sess := createMetadataTestSession(t, store, cfg, binding)
	protected := "original\nunsent draft"
	if err := sess.SetInputDraft("edited recalled prompt", &session.ProtectedInputDraftUpdate{Text: &protected}); err != nil {
		t.Fatal(err)
	}
	record, err := store.ResolvePersistedSession(t.Context(), sess.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.InputDraft != "edited recalled prompt" || record.Meta.ProtectedInputDraft == nil || *record.Meta.ProtectedInputDraft != protected {
		t.Fatalf("saved drafts = %+v", record.Meta)
	}
	if err := sess.SetInputDraft("", nil); err != nil {
		t.Fatal(err)
	}
	record, err = store.ResolvePersistedSession(t.Context(), sess.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.InputDraft != "" || record.Meta.ProtectedInputDraft == nil || *record.Meta.ProtectedInputDraft != protected {
		t.Fatalf("active-only save lost protected draft: %+v", record.Meta)
	}
	if err := sess.SetInputDraft(protected, &session.ProtectedInputDraftUpdate{}); err != nil {
		t.Fatal(err)
	}
	record, err = store.ResolvePersistedSession(t.Context(), sess.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.InputDraft != protected || record.Meta.ProtectedInputDraft != nil {
		t.Fatalf("restore did not clear protected draft: %+v", record.Meta)
	}
}

func TestProtectedDraftMigrationPreservesActiveDraft(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "db", "main.sqlite3")
	db, err := openDatabaseAtVersionForTest(t, root, path, 91)
	if err != nil {
		t.Fatal(err)
	}
	execSeed(t, db, "project", `INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms) VALUES ('draft-project', 'Project', 1000, 1000)`)
	want := "  unsent\nmultiline draft  "
	execSeed(t, db, "session", `INSERT INTO sessions (id, project_id, artifact_relpath, input_draft, created_at_unix_ms, updated_at_unix_ms) VALUES ('draft-session', 'draft-project', 'sessions/draft-session', ?, 1000, 1000)`, want)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenAtPath(root, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var active string
	var protected *string
	if err := store.DB().QueryRowContext(t.Context(), `SELECT input_draft, protected_input_draft FROM sessions WHERE id = 'draft-session'`).Scan(&active, &protected); err != nil {
		t.Fatal(err)
	}
	if active != want || protected != nil {
		t.Fatalf("migrated drafts = %q, %v", active, protected)
	}
}
