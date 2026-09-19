package metadata

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestSetupRecoveryMigrationPreservesDiagnostics(t *testing.T) {
	root := t.TempDir()
	db, err := openDatabaseAtVersionForTest(t, root, filepath.Join(root, "db", "main.sqlite3"), 92)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	execSeed(t, db, "project", `INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('project-setup', 'Project', ?, ?, '{}')`, now, now)
	seedWorkflowGraph(t, db, "project-setup", now)
	execSeed(t, db, "task", workflowSeedTaskSQL, "task-setup", "link-1", 1, "Setup", now, now)
	detail := `{"Code":"setup_failed","Fields":{"preserve":"value"},"setup_recovery":{"setup_operation_id":"0d41399b-1fbb-4ab8-a42f-4cbf7ff7b8f5","cause":"process_exit","diagnostic":"script failed\nwith diagnostics","script_path":"/source/setup.sh","setup_requirement":"required","execution_target":{"mode":"head"},"retained_worktree":{"worktree_id":"retained","root":"/worktrees/retained"},"retained_previous_worktree":{"worktree_id":"previous","root":"/worktrees/previous"}}}`
	execSeed(t, db, "interrupted node", `INSERT INTO task_current_nodes
(task_id,node_id,entered_by_edge_id,current_input_values_json,prior_node_values_json,scheduling_state,interruption_reason,interruption_detail_json,interrupted_at_unix_ms)
VALUES (?,?,?,'{}','{"transition_parameters":{}}','interrupted','setup_failed',?,1000)`,
		"task-setup", workflowGraphSeedID(t, db, "node-agent"), workflowGraphSeedID(t, db, "edge-start-1"), detail)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var encoded string
	if err := store.DB().QueryRowContext(t.Context(), `SELECT interruption_detail_json FROM task_current_nodes WHERE task_id='task-setup'`).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	var migrated struct {
		Code          string
		Fields        map[string]string
		SetupRecovery json.RawMessage `json:"setup_recovery"`
	}
	if err := json.Unmarshal([]byte(encoded), &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.SetupRecovery != nil {
		t.Fatal("obsolete recovery payload retained")
	}
	want := map[string]string{
		"preserve": "value", "error": "script failed\nwith diagnostics",
		"setup_cause": "process_exit", "setup_script_path": "/source/setup.sh",
		"retained_worktree_root": "/worktrees/retained", "retained_worktree_id": "retained",
		"retained_previous_worktree_root": "/worktrees/previous", "retained_previous_worktree_id": "previous",
	}
	if migrated.Code != "setup_failed" {
		t.Fatalf("lost failure code: %q", migrated.Code)
	}
	for key, value := range want {
		if migrated.Fields[key] != value {
			t.Errorf("diagnostic %s=%q, want %q", key, migrated.Fields[key], value)
		}
	}
}
