package metadata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"core/server/sessionstore"
	"core/shared/runtimeids"
)

func TestSessionShortIDMigrationPreservesUnnumberedSessionOnUse(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	db, err := openDatabaseAtVersionForTest(t, 95)
	if err != nil {
		t.Fatal(err)
	}
	id := runtimeids.NewSessionID()
	relpath := filepath.Join("sessions", id.String())
	content := []byte("preserved historical content\n")
	if err := os.MkdirAll(filepath.Join(root, relpath), 0o700); err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(root, relpath, "events.jsonl")
	if err := os.WriteFile(eventPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(sessionMetadataDocument{WorkspaceRoot: workspace, WorkspaceContainer: "old"})
	if err != nil {
		t.Fatal(err)
	}
	execSeed(t, db, "project", `INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms) VALUES ('old-project', 'Project', 1000, 1000)`)
	execSeed(t, db, "session", `INSERT INTO sessions
		(id, project_id, artifact_relpath, name, input_draft, metadata_json, created_at_unix_ms, updated_at_unix_ms)
		VALUES (?, 'old-project', ?, 'authored name', 'authored draft', ?, 1000, 1000)`, id.String(), relpath, string(payload))
	store := migrateMetadataTestStore(t, root, db)
	record, err := store.ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ImportSessionSnapshot(t.Context(), session.PersistedStoreSnapshot{SessionDir: record.SessionDir, Meta: *record.Meta}); err != nil {
		t.Fatal(err)
	}
	updated, err := store.ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if updated.ShortID != nil || updated.Meta.SessionID != id.String() || updated.Meta.Name == nil || *updated.Meta.Name != "authored name" ||
		updated.Meta.InputDraft != "authored draft" || updated.SessionDir != filepath.Join(root, relpath) {
		t.Fatalf("migration/use altered retained identity/content: %+v", updated)
	}
	identity, err := store.SessionIdentity(t.Context(), id)
	if err != nil || identity.ID != id || identity.ShortID != nil {
		t.Fatalf("old Session identity projection = %v, %v", identity, err)
	}
	got, err := os.ReadFile(eventPath)
	if err != nil || string(got) != string(content) {
		t.Fatalf("historical content = %q, %v", got, err)
	}
}
