package metadata

import (
	"database/sql"
	"encoding/json"
	"testing"

	"core/server/metadata/sqlitegen"
	"core/shared/sessioncontract"
)

func TestWorkflowSnapshotCutoverPreservesSessionFacts(t *testing.T) {
	root := t.TempDir()
	db, err := openDatabaseAtVersionForTest(t, 97)
	if err != nil {
		t.Fatal(err)
	}
	execSeed(t, db, "project", `INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms)
VALUES ('snapshot-project', 'Project', 1000, 1000)`)
	for _, mode := range []string{"tool", "shell_command", "absent", "null"} {
		locked := map[string]any{"model": "model", "system_prompt": "retained"}
		if mode == "null" {
			locked["workflow_completion_mode"] = nil
		} else if mode != "absent" {
			locked["workflow_completion_mode"] = mode
		}
		payload, err := json.Marshal(locked)
		if err != nil {
			t.Fatal(err)
		}
		execSeed(t, db, "session", `INSERT INTO sessions (
id, project_id, artifact_relpath, locked_json, metadata_json, created_at_unix_ms, updated_at_unix_ms
) VALUES (?, 'snapshot-project', ?, ?, '{"conversation_established":true}', 1000, 2000)`,
			mode, "sessions/"+mode, string(payload))
	}
	store := migrateMetadataTestStore(t, root, db)
	for _, mode := range []string{"tool", "shell_command", "absent", "null"} {
		var lockedJSON, metadataJSON string
		var created, updated int64
		if err := store.DB().QueryRowContext(t.Context(), `SELECT locked_json, metadata_json, created_at_unix_ms, updated_at_unix_ms FROM sessions WHERE id = ?`, mode).
			Scan(&lockedJSON, &metadataJSON, &created, &updated); err != nil {
			t.Fatal(err)
		}
		var locked map[string]any
		if err := json.Unmarshal([]byte(lockedJSON), &locked); err != nil {
			t.Fatal(err)
		}
		if _, exists := locked["workflow_completion_mode"]; exists {
			t.Fatal("legacy authority remains")
		}
		if locked["model"] != "model" || locked["system_prompt"] != "retained" || created != 1000 || updated != 2000 {
			t.Fatal("migration changed unrelated session facts")
		}
		meta, err := sessionMetaFromRecordRow(sqlitegen.GetSessionRecordByIDRow{
			ID: mode, LockedJson: lockedJSON, MetadataJson: metadataJSON,
			ContinuationJson: "{}", UsageStateJson: "{}", Category: sql.NullString{String: "main", Valid: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !meta.ConversationEstablished {
			t.Fatal("migration lost existing metadata")
		}
		if mode == "absent" || mode == "null" {
			if meta.WorkflowSnapshot != nil {
				t.Fatal("migration invented a workflow snapshot")
			}
		} else if meta.WorkflowSnapshot == nil || meta.WorkflowSnapshot.CompletionMode != sessioncontract.WorkflowCompletionMode(mode) {
			t.Fatalf("migrated workflow snapshot = %+v", meta.WorkflowSnapshot)
		}
	}
}
