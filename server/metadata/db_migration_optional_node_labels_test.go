package metadata

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	sqlite3 "modernc.org/sqlite/lib"
)

func TestOptionalNodeLabelsMigrationAllowsEmptyLabels(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "db", "main.sqlite3")
	db, err := openDatabaseAtVersionForTest(t, root, path, 94)
	if err != nil {
		t.Fatal(err)
	}
	execSeed(t, db, "project", `INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms) VALUES ('label-project', 'Project', 1000, 1000)`)
	seedWorkflowGraph(t, db, "label-project", 1000)
	seedWorkflowGraphForProject(t, db, "label-project", 1000, "2")
	execSeed(t, db, "node groups", `INSERT INTO workflow_node_groups (id, workflow_id, group_key, display_name) VALUES ('label-group', ?, 'group', 'Group'), ('other-label-group', ?, 'other', 'Other')`, workflowTestID(t, "1"), workflowTestID(t, "2"))
	execSeed(t, db, "populated node facts", `UPDATE workflow_nodes SET display_name = '  Existing label  ', group_id = 'label-group', sort_order = 7, join_input_providers_json = '["agent"]', completion_mode = 'tool' WHERE id = ?`, workflowGraphSeedID(t, db, "node-agent"))
	execSeed(t, db, "script node", `INSERT INTO workflow_nodes (id, workflow_id, node_key, kind, display_name, script_path) VALUES ('label-script', ?, 'script', 'script', 'Script', 'scripts/run.sh')`, workflowTestID(t, "1"))
	execSeed(t, db, "task", workflowSeedTaskSQL, "label-task", "link-1", 1, "Task", 1000, 1000)
	insertTaskCurrentNode(t, db, "label-task", "node-agent", nil)
	insertTaskPendingApproval(t, db, "label-approval", "label-task", "node-agent", nil, 1000)
	execSeed(t, db, "pending branch", `INSERT INTO task_pending_approval_branches (approval_id, transition_branch_key, target_snapshot_json, effective_edge_configuration_json, context_source_resolution_json) VALUES ('label-approval', 'branch', json_object('node_id', ?, 'prior_values', json('{"transition_parameters":{}}')), '{}', '{}')`, workflowGraphSeedID(t, db, "node-done"))

	// Capture persisted values, including nullable columns and raw label whitespace.
	queries := []string{
		`SELECT * FROM workflow_nodes ORDER BY id`,
		`SELECT * FROM workflow_node_groups ORDER BY id`,
		`SELECT * FROM workflow_transition_groups ORDER BY id`,
		`SELECT * FROM workflow_edges ORDER BY id`,
		`SELECT * FROM workflows ORDER BY id`,
		`SELECT * FROM project_workflow_links ORDER BY id`,
		`SELECT * FROM tasks ORDER BY id`,
		`SELECT * FROM task_current_nodes ORDER BY task_id, node_id`,
		`SELECT * FROM task_pending_approvals ORDER BY id`,
		`SELECT * FROM task_pending_approval_branches ORDER BY approval_id, transition_branch_key`,
	}
	before := make([][][]any, len(queries))
	for i, query := range queries {
		before[i] = optionalNodeLabelMigrationRows(t, db, query)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenAtPath(root, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for i, query := range queries {
		if after := optionalNodeLabelMigrationRows(t, store.DB(), query); !reflect.DeepEqual(before[i], after) {
			t.Fatalf("migration changed persisted facts for %s: before=%v after=%v", query, before[i], after)
		}
	}
	if violations := optionalNodeLabelMigrationRows(t, store.DB(), `PRAGMA foreign_key_check`); len(violations) != 0 {
		t.Fatalf("foreign-key violations: %v", violations)
	}
	execSeed(t, store.DB(), "empty label insert", `INSERT INTO workflow_nodes (id, workflow_id, node_key, kind, display_name) VALUES ('empty-node', ?, 'empty', 'agent', '')`, workflowTestID(t, "1"))
	execSeed(t, store.DB(), "empty label update", `UPDATE workflow_nodes SET display_name = '' WHERE id = ?`, workflowGraphSeedID(t, store.DB(), "node-agent"))

	t.Run("label bounds", func(t *testing.T) {
		for _, label := range []string{"", "   ", strings.Repeat("界", 120), "  " + strings.Repeat("x", 120) + "  "} {
			execSeed(t, store.DB(), "valid label", `UPDATE workflow_nodes SET display_name = ? WHERE id = 'empty-node'`, label)
		}
		assertSQLiteConstraint(t, store.DB(), sqlite3.SQLITE_CONSTRAINT_CHECK, `INSERT INTO workflow_nodes (id, workflow_id, node_key, kind, display_name) VALUES ('long-label', ?, 'long', 'agent', ?)`, workflowTestID(t, "1"), strings.Repeat("x", 121))
		assertSQLiteConstraint(t, store.DB(), sqlite3.SQLITE_CONSTRAINT_CHECK, `UPDATE workflow_nodes SET display_name = ? WHERE id = 'empty-node'`, strings.Repeat("界", 121))
		assertSQLiteConstraint(t, store.DB(), sqlite3.SQLITE_CONSTRAINT_NOTNULL, `UPDATE workflow_nodes SET display_name = NULL WHERE id = 'empty-node'`)
	})
	t.Run("retained node invariants", func(t *testing.T) {
		agentID := workflowGraphSeedID(t, store.DB(), "node-agent")
		terminalID := workflowGraphSeedID(t, store.DB(), "node-done")
		assertSQLiteConstraint(t, store.DB(), sqlite3.SQLITE_CONSTRAINT_UNIQUE, `INSERT INTO workflow_nodes (id, workflow_id, node_key, kind, display_name) VALUES ('second-start', ?, 'second-start', 'start', '')`, workflowTestID(t, "1"))
		assertSQLiteConstraint(t, store.DB(), sqlite3.SQLITE_CONSTRAINT_TRIGGER, `INSERT INTO workflow_nodes (id, workflow_id, node_key, kind, display_name, group_id) VALUES ('wrong-group', ?, 'wrong-group', 'agent', '', 'other-label-group')`, workflowTestID(t, "1"))
		assertSQLiteConstraint(t, store.DB(), sqlite3.SQLITE_CONSTRAINT_TRIGGER, `UPDATE workflow_nodes SET group_id = 'other-label-group' WHERE id = ?`, agentID)
		assertSQLiteConstraint(t, store.DB(), sqlite3.SQLITE_CONSTRAINT_TRIGGER, `UPDATE workflow_nodes SET kind = 'join', completion_mode = '' WHERE id = ?`, agentID)
		assertSQLiteConstraint(t, store.DB(), sqlite3.SQLITE_CONSTRAINT_TRIGGER, `UPDATE workflow_nodes SET kind = 'join' WHERE id = ?`, terminalID)
		assertSQLiteConstraint(t, store.DB(), sqlite3.SQLITE_CONSTRAINT_TRIGGER, `DELETE FROM workflow_nodes WHERE id = ?`, agentID)
		assertSQLiteConstraint(t, store.DB(), sqlite3.SQLITE_CONSTRAINT_TRIGGER, `DELETE FROM workflow_nodes WHERE id = ?`, terminalID)
		assertSQLiteConstraint(t, store.DB(), sqlite3.SQLITE_CONSTRAINT_TRIGGER, `UPDATE workflow_edges SET target_node_id = 'missing-node' WHERE id = ?`, workflowGraphSeedID(t, store.DB(), "edge-start-1"))
		assertSQLiteConstraint(t, store.DB(), sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY, `INSERT INTO workflow_nodes (id, workflow_id, node_key, kind, display_name) VALUES ('missing-workflow', x'550e8400e29b41d4a716446655440009', 'missing', 'agent', '')`)
	})
}

func optionalNodeLabelMigrationRows(t *testing.T, db *sql.DB, query string) [][]any {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var values [][]any
	for rows.Next() {
		row := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for i := range row {
			destinations[i] = &row[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			t.Fatal(err)
		}
		values = append(values, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return values
}
