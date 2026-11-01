package metadata

import (
	"reflect"
	"strings"
	"testing"

	sqlite3 "modernc.org/sqlite/lib"
)

func TestOptionalGroupNamesMigrationPreservesGraphAndNullableNames(t *testing.T) {
	root := t.TempDir()
	db, err := openDatabaseAtVersionForTest(t, 100)
	if err != nil {
		t.Fatal(err)
	}
	execSeed(t, db, "project", `INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms) VALUES ('group-project', 'Project', 1000, 1000)`)
	seedWorkflowGraph(t, db, "group-project", 1000)
	execSeed(t, db, "groups", `INSERT INTO workflow_node_groups (rowid, id, workflow_id, group_key, display_name, sort_order) VALUES (7, 'named-group', ?, 'group', '  Existing name  ', 4)`, workflowTestID(t, "1"))
	execSeed(t, db, "member", `UPDATE workflow_nodes SET group_id = 'named-group' WHERE id = ?`, workflowGraphSeedID(t, db, "node-agent"))
	execSeed(t, db, "task", workflowSeedTaskSQL, "group-task", "link-1", 1, "Task", 1000, 1000)
	insertTaskCurrentNode(t, db, "group-task", "node-agent", nil)
	queries := []string{
		`SELECT rowid, * FROM workflow_node_groups ORDER BY rowid`,
		`SELECT * FROM workflow_nodes ORDER BY id`,
		`SELECT * FROM workflow_edges ORDER BY id`,
		`SELECT * FROM tasks ORDER BY id`,
		`SELECT * FROM task_current_nodes ORDER BY task_id, node_id`,
	}
	before := make([][][]any, len(queries))
	for i, query := range queries {
		before[i] = optionalNodeLabelMigrationRows(t, db, query)
	}
	store := migrateMetadataTestStore(t, root, db)
	for i, query := range queries {
		if after := optionalNodeLabelMigrationRows(t, store.DB(), query); !reflect.DeepEqual(before[i], after) {
			t.Fatalf("migration changed facts for %s: before=%v after=%v", query, before[i], after)
		}
	}
	execSeed(t, store.DB(), "clear name", `UPDATE workflow_node_groups SET display_name = NULL WHERE id = 'named-group'`)
	var name *string
	if err := store.DB().QueryRowContext(t.Context(), `SELECT display_name FROM workflow_node_groups WHERE id = 'named-group'`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != nil {
		t.Fatalf("cleared name = %v", name)
	}
	for _, invalid := range []string{"", "   ", strings.Repeat("x", 121)} {
		assertSQLiteConstraint(t, store.DB(), sqlite3.SQLITE_CONSTRAINT_CHECK, `UPDATE workflow_node_groups SET display_name = ? WHERE id = 'named-group'`, invalid)
	}
	if violations := optionalNodeLabelMigrationRows(t, store.DB(), `PRAGMA foreign_key_check`); len(violations) != 0 {
		t.Fatalf("foreign-key violations: %v", violations)
	}
}
