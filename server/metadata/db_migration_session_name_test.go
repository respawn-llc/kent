package metadata

import (
	"testing"
)

func TestSessionNameMigrationPreservesSessionFactsAcrossRepeatedMigrations(t *testing.T) {
	root := t.TempDir()
	db, err := openDatabaseAtVersionForTest(t, 94)
	if err != nil {
		t.Fatal(err)
	}
	execSeed(t, db, "project", `INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms) VALUES ('name-project', 'Project', 1000, 1000)`)
	for _, fixture := range []struct {
		id   string
		name string
	}{
		{id: "unnamed-session"},
		{id: "named-session", name: "レビュー  \"release\"\nnotes"},
	} {
		execSeed(t, db, "session", `
INSERT INTO sessions (
    id, project_id, artifact_relpath, name, input_draft, protected_input_draft,
    first_prompt_preview, metadata_json, last_sequence, created_at_unix_ms, updated_at_unix_ms
) VALUES (?, 'name-project', ?, ?, 'unsent message', 'protected message',
    'original prompt', '{"conversation_established":true}', 1234, 1000, 2000)`,
			fixture.id, "sessions/"+fixture.id, fixture.name)
	}
	for range 2 {
		store := migrateMetadataTestStore(t, root, db)
		for _, id := range []string{"unnamed-session", "named-session"} {
			record, err := store.ResolvePersistedSession(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			name := record.Meta.Name
			var project, artifact, draft, protected, preview, metadata string
			var sequence, created, updated int64
			if err := store.DB().QueryRowContext(t.Context(), `
SELECT project_id, artifact_relpath, input_draft, protected_input_draft,
    first_prompt_preview, metadata_json, last_sequence, created_at_unix_ms, updated_at_unix_ms
FROM sessions WHERE id = ?`, id).Scan(
				&project, &artifact, &draft, &protected, &preview, &metadata, &sequence, &created, &updated,
			); err != nil {
				t.Fatal(err)
			}
			if id == "unnamed-session" && name != nil {
				t.Fatalf("legacy unnamed Session name = %q, want absent", *name)
			}
			if id == "named-session" && (name == nil || *name != "レビュー  \"release\"\nnotes") {
				t.Fatalf("named Session changed: %v", name)
			}
			if project != "name-project" || artifact != "sessions/"+id ||
				draft != "unsent message" || protected != "protected message" ||
				preview != "original prompt" || metadata != `{"conversation_established":true}` ||
				sequence != 1234 || created != 1000 || updated != 2000 {
				t.Fatalf("migration changed unrelated Session facts for %s", id)
			}
		}
	}
}
