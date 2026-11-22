package metadata

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/google/uuid"
	sqlite3 "modernc.org/sqlite/lib"
)

func TestGraphBlobIdentityMigrationPreservesGraphReferences(t *testing.T) {
	for _, format := range []string{"canonical-v4", "uuid-v5", "legacy"} {
		t.Run(format, func(t *testing.T) {
			db, err := openDatabaseAtVersionForTest(t, 102)
			if err != nil {
				t.Fatal(err)
			}
			ids := make(map[string]string)
			for _, name := range []string{"group", "source", "target", "transition", "edge"} {
				switch format {
				case "canonical-v4":
					ids[name] = uuid.NewString()
				case "uuid-v5":
					ids[name] = uuid.NewSHA1(uuid.NameSpaceURL, []byte(name)).String()
				case "legacy":
					ids[name] = "old-" + name
				}
			}
			seedGraphBlobIdentityMigration(t, db, ids)
			contentQueries := []string{
				`SELECT rowid, workflow_id, group_key, display_name, sort_order FROM workflow_node_groups`,
				`SELECT rowid, workflow_id, node_key, kind, display_name, subagent_role, sort_order, completion_mode, script_path FROM workflow_nodes ORDER BY rowid`,
				`SELECT json_remove(join_input_providers_json, '$[0].provider_edge_id') FROM workflow_nodes WHERE node_key = 'target'`,
				`SELECT rowid, transition_id, display_name, sort_order, description FROM workflow_transition_groups`,
				`SELECT rowid, edge_key, requires_approval, context_mode, input_bindings_json, output_requirements_json, sort_order, context_source_kind, context_source_node_key, prompt_template, parameters_json, assignee_selection, thinking_selection FROM workflow_edges`,
				`SELECT rowid, task_id, transition_branch_key, current_input_values_json, prior_node_values_json, session_id, scheduling_state, interruption_reason, interruption_detail_json, interrupted_at_unix_ms, effective_assignee, effective_thinking, assignee_origin FROM task_current_nodes`,
				`SELECT rowid, id, source_task_id, source_transition_branch_key, source_session_id, workflow_version, materialized_values_json, created_at_unix_ms, json_remove(transition_snapshot_json, '$.id', '$.source_node_id') FROM task_pending_approvals`,
				`SELECT rowid, approval_id, transition_branch_key, json_remove(target_snapshot_json, '$.node_id', '$.entered_by_edge_id'), json_remove(effective_edge_configuration_json, '$.id', '$.transition_group_id', '$.target_node_id'), context_source_resolution_json FROM task_pending_approval_branches ORDER BY rowid`,
				`SELECT rowid, session_id, transition_branch_key, associated_at_unix_ms FROM session_workflow_node_associations`,
				`SELECT * FROM tasks`,
				`SELECT * FROM sessions`,
				`SELECT * FROM workflows`,
				`SELECT * FROM project_workflow_links`,
			}
			before := make([][][]any, len(contentQueries))
			for i, query := range contentQueries {
				before[i] = optionalNodeLabelMigrationRows(t, db, query)
			}
			provider, err := newMetadataMigrationProvider(db)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := provider.UpTo(t.Context(), 103); err != nil {
				t.Fatalf("upgrade 102 graph: %v", err)
			}
			for i, query := range contentQueries {
				if after := optionalNodeLabelMigrationRows(t, db, query); !reflect.DeepEqual(before[i], after) {
					t.Fatalf("lost content for %s: before=%v after=%v", query, before[i], after)
				}
			}
			migrated := make(map[string][]byte)
			for name, query := range map[string]string{
				"group":      `SELECT id FROM workflow_node_groups`,
				"source":     `SELECT id FROM workflow_nodes WHERE node_key = 'source'`,
				"target":     `SELECT id FROM workflow_nodes WHERE node_key = 'target'`,
				"transition": `SELECT id FROM workflow_transition_groups`,
				"edge":       `SELECT id FROM workflow_edges`,
			} {
				var value any
				if err := db.QueryRowContext(t.Context(), query).Scan(&value); err != nil {
					t.Fatal(err)
				}
				blob, ok := value.([]byte)
				if !ok {
					t.Fatalf("%s identity storage = %T, want BLOB", name, value)
				}
				id, err := uuid.FromBytes(blob)
				if err != nil || id.Version() != 4 || id.Variant() != uuid.RFC4122 {
					t.Fatalf("%s identity = %x: %v", name, blob, err)
				}
				if (id.String() == ids[name]) != (format == "canonical-v4") {
					t.Fatalf("%s identity preservation/remapping: %s -> %s", name, ids[name], id)
				}
				migrated[name] = blob
			}
			for _, ref := range []struct{ name, query string }{
				{"group", `SELECT group_id FROM workflow_nodes WHERE node_key = 'source'`},
				{"source", `SELECT source_node_id FROM workflow_transition_groups`},
				{"transition", `SELECT transition_group_id FROM workflow_edges`},
				{"target", `SELECT target_node_id FROM workflow_edges`},
				{"source", `SELECT node_id FROM task_current_nodes`},
				{"edge", `SELECT entered_by_edge_id FROM task_current_nodes`},
				{"source", `SELECT source_node_id FROM task_pending_approvals`},
				{"target", `SELECT node_id FROM session_workflow_node_associations`},
			} {
				var value any
				if err := db.QueryRowContext(t.Context(), ref.query).Scan(&value); err != nil {
					t.Fatal(err)
				}
				blob, ok := value.([]byte)
				if !ok || !bytes.Equal(blob, migrated[ref.name]) {
					t.Fatalf("%s reference mismatch: %T %v", ref.query, value, value)
				}
			}
			for _, ref := range []struct{ name, query string }{
				{"edge", `SELECT json_extract(join_input_providers_json, '$[0].provider_edge_id') FROM workflow_nodes WHERE node_key = 'target'`},
				{"transition", `SELECT json_extract(transition_snapshot_json, '$.id') FROM task_pending_approvals`},
				{"source", `SELECT json_extract(transition_snapshot_json, '$.source_node_id') FROM task_pending_approvals`},
				{"target", `SELECT json_extract(target_snapshot_json, '$.node_id') FROM task_pending_approval_branches`},
				{"edge", `SELECT json_extract(target_snapshot_json, '$.entered_by_edge_id') FROM task_pending_approval_branches WHERE transition_branch_key = 'present'`},
				{"edge", `SELECT json_extract(effective_edge_configuration_json, '$.id') FROM task_pending_approval_branches`},
				{"transition", `SELECT json_extract(effective_edge_configuration_json, '$.transition_group_id') FROM task_pending_approval_branches`},
				{"target", `SELECT json_extract(effective_edge_configuration_json, '$.target_node_id') FROM task_pending_approval_branches`},
			} {
				rows := optionalNodeLabelMigrationRows(t, db, ref.query)
				for _, row := range rows {
					if want := uuid.Must(uuid.FromBytes(migrated[ref.name])).String(); row[0] != want {
						t.Fatalf("%s = %v, want %s", ref.query, row[0], want)
					}
				}
			}
			var absent any
			var nullType string
			if err := db.QueryRow(`SELECT json_type(target_snapshot_json, '$.entered_by_edge_id') FROM task_pending_approval_branches WHERE transition_branch_key = 'absent'`).Scan(&absent); err != nil || absent != nil {
				t.Fatalf("absent optional reference changed: %v, %v", absent, err)
			}
			if err := db.QueryRow(`SELECT json_type(target_snapshot_json, '$.entered_by_edge_id') FROM task_pending_approval_branches WHERE transition_branch_key = 'null'`).Scan(&nullType); err != nil || nullType != "null" {
				t.Fatalf("null optional reference changed: %v, %v", nullType, err)
			}
			var statusJSON string
			if err := db.QueryRow(`SELECT node_ids_json FROM workflow_task_status_records WHERE task_id = 'blob-task'`).Scan(&statusJSON); err != nil {
				t.Fatal(err)
			}
			var statusIDs []string
			if err := json.Unmarshal([]byte(statusJSON), &statusIDs); err != nil || !reflect.DeepEqual(statusIDs, []string{uuid.Must(uuid.FromBytes(migrated["source"])).String()}) {
				t.Fatalf("status node projection = %s: %v", statusJSON, err)
			}
			if violations := optionalNodeLabelMigrationRows(t, db, `PRAGMA foreign_key_check`); len(violations) != 0 {
				t.Fatalf("foreign-key violations: %v", violations)
			}
			var targetGroup any
			if err := db.QueryRow(`SELECT group_id FROM workflow_nodes WHERE node_key = 'target'`).Scan(&targetGroup); err != nil || targetGroup != nil {
				t.Fatalf("nullable group reference changed: %v, %v", targetGroup, err)
			}
			execSeed(t, db, "nullable group name", `UPDATE workflow_node_groups SET display_name = NULL`)
			execSeed(t, db, "empty node label", `UPDATE workflow_nodes SET display_name = ''`)
			// The target is protected only by its frozen Approval snapshot, not a current position.
			assertSQLiteConstraint(t, db, sqlite3.SQLITE_CONSTRAINT_TRIGGER, `DELETE FROM workflow_nodes WHERE id = ?`, migrated["target"])
			assertSQLiteConstraint(t, db, sqlite3.SQLITE_CONSTRAINT_TRIGGER, `UPDATE workflow_nodes SET kind = 'terminal' WHERE id = ?`, migrated["target"])
			assertSQLiteConstraint(t, db, sqlite3.SQLITE_CONSTRAINT_UNIQUE, `INSERT INTO workflow_nodes (id, workflow_id, node_key, kind, display_name) SELECT ?, workflow_id, node_key, kind, display_name FROM workflow_nodes WHERE node_key = 'source'`, graphUUIDBytes(uuid.NewString()))
		})
	}
}

func seedGraphBlobIdentityMigration(t *testing.T, db *sql.DB, ids map[string]string) {
	t.Helper()
	execSeed(t, db, "project", `INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms) VALUES ('blob-project', 'Project', 1000, 1000)`)
	execSeed(t, db, "workflow", `INSERT INTO workflows (id, name, description, version, created_at_unix_ms, updated_at_unix_ms) VALUES (?, 'Workflow', 'retained', 3, 1000, 1000)`, workflowTestID(t, "1"))
	execSeed(t, db, "group", `INSERT INTO workflow_node_groups (rowid, id, workflow_id, group_key, display_name, sort_order) VALUES (7, ?, ?, 'group', NULL, 4)`, ids["group"], workflowTestID(t, "1"))
	execSeed(t, db, "source", `INSERT INTO workflow_nodes (rowid, id, workflow_id, node_key, kind, display_name, subagent_role, group_id, completion_mode) VALUES (8, ?, ?, 'source', 'agent', '', 'default', ?, 'tool')`, ids["source"], workflowTestID(t, "1"), ids["group"])
	execSeed(t, db, "target", `INSERT INTO workflow_nodes (rowid, id, workflow_id, node_key, kind, display_name, join_input_providers_json) VALUES (9, ?, ?, 'target', 'join', '  Target  ', json_array(json_object('input_name', 'summary', 'provider_edge_id', ?, 'extra', 'retained')))`, ids["target"], workflowTestID(t, "1"), ids["edge"])
	execSeed(t, db, "transition", `INSERT INTO workflow_transition_groups (id, source_node_id, transition_id, description) VALUES (?, ?, 'next', 'retained transition')`, ids["transition"], ids["source"])
	execSeed(t, db, "edge", `INSERT INTO workflow_edges (id, transition_group_id, edge_key, target_node_id, requires_approval, context_mode, prompt_template) VALUES (?, ?, 'next', ?, 1, 'new_session', 'retained prompt')`, ids["edge"], ids["transition"], ids["target"])
	execSeed(t, db, "link", `INSERT INTO project_workflow_links (id, project_id, workflow_id, created_at_unix_ms, updated_at_unix_ms) VALUES ('blob-link', 'blob-project', ?, 1000, 1000)`, workflowTestID(t, "1"))
	execSeed(t, db, "task", workflowSeedTaskSQL, "blob-task", "blob-link", 1, "Task", 1000, 1000)
	execSeed(t, db, "session", `INSERT INTO sessions (id, project_id, task_id, artifact_relpath, created_at_unix_ms, updated_at_unix_ms) VALUES ('blob-session', 'blob-project', 'blob-task', 'sessions/blob', 1000, 1000)`)
	execSeed(t, db, "current node", `INSERT INTO task_current_nodes (task_id, node_id, entered_by_edge_id, current_input_values_json, session_id) VALUES ('blob-task', ?, ?, '{"summary":"retained"}', 'blob-session')`, ids["source"], ids["edge"])
	execSeed(t, db, "association", `INSERT INTO session_workflow_node_associations (session_id, node_id, associated_at_unix_ms) VALUES ('blob-session', ?, 1000)`, ids["target"])
	execSeed(t, db, "approval", `INSERT INTO task_pending_approvals (id, source_task_id, source_node_id, workflow_version, transition_snapshot_json, materialized_values_json, created_at_unix_ms) VALUES ('blob-approval', 'blob-task', ?, 3, json_object('id', ?, 'source_node_id', ?, 'extra', 'retained'), '{"summary":"retained"}', 1000)`, ids["source"], ids["transition"], ids["source"])
	for _, branch := range []string{"present", "absent", "null"} {
		target := map[string]any{"node_id": ids["target"], "prior_values": map[string]any{"transition_parameters": map[string]any{"next": map[string]any{"summary": "retained"}}}}
		switch branch {
		case "present":
			target["entered_by_edge_id"] = ids["edge"]
		case "null":
			target["entered_by_edge_id"] = nil
		}
		payload, err := json.Marshal(target)
		if err != nil {
			t.Fatal(err)
		}
		execSeed(t, db, "approval branch", `INSERT INTO task_pending_approval_branches (approval_id, transition_branch_key, target_snapshot_json, effective_edge_configuration_json, context_source_resolution_json) VALUES ('blob-approval', ?, ?, json_object('id', ?, 'transition_group_id', ?, 'target_node_id', ?, 'extra', 'retained'), '{"retained":true}')`, branch, string(payload), ids["edge"], ids["transition"], ids["target"])
	}
}

func TestGraphBlobIdentityMigrationPreservesNullJoinProviders(t *testing.T) {
	db, err := openDatabaseAtVersionForTest(t, 102)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, name := range []string{"group", "source", "target", "transition", "edge"} {
		ids[name] = uuid.NewString()
	}
	seedGraphBlobIdentityMigration(t, db, ids)
	execSeed(t, db, "null join providers", `UPDATE workflow_nodes SET join_input_providers_json = 'null' WHERE node_key = 'source'`)
	provider, err := newMetadataMigrationProvider(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(t.Context(), 103); err != nil {
		t.Fatalf("upgrade graph with null join providers: %v", err)
	}
	var providers string
	if err := db.QueryRow(`SELECT join_input_providers_json FROM workflow_nodes WHERE node_key = 'source'`).Scan(&providers); err != nil {
		t.Fatal(err)
	}
	if providers != "null" {
		t.Fatalf("null join providers changed: %s", providers)
	}
	if violations := optionalNodeLabelMigrationRows(t, db, `PRAGMA foreign_key_check`); len(violations) != 0 {
		t.Fatalf("foreign-key violations: %v", violations)
	}
}

func TestGraphBlobIdentityMigrationFailurePreservesExistingData(t *testing.T) {
	for _, invalid := range []string{"", "   ", uuid.Nil.String()} {
		t.Run("invalid-"+invalid, func(t *testing.T) {
			db, err := openDatabaseAtVersionForTest(t, 102)
			if err != nil {
				t.Fatal(err)
			}
			execSeed(t, db, "workflow", `INSERT INTO workflows (id, name, description, version, created_at_unix_ms, updated_at_unix_ms) VALUES (?, 'Workflow', 'retained', 1, 1000, 1000)`, workflowTestID(t, "1"))
			execSeed(t, db, "invalid historical identity", `INSERT INTO workflow_node_groups (id, workflow_id, group_key, display_name) VALUES (?, ?, 'group', 'Retained')`, invalid, workflowTestID(t, "1"))
			before := optionalNodeLabelMigrationRows(t, db, `SELECT * FROM workflow_node_groups`)
			if err := runMigrations(db); err == nil {
				t.Fatal("invalid historical identity migrated successfully")
			}
			if after := optionalNodeLabelMigrationRows(t, db, `SELECT * FROM workflow_node_groups`); !reflect.DeepEqual(before, after) {
				t.Fatalf("failed migration changed original data: %v -> %v", before, after)
			}
			var version int64
			if err := db.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied = 1`).Scan(&version); err != nil || version != 102 {
				t.Fatalf("failed migration advanced version: %d, %v", version, err)
			}
			// A corrected identity can be migrated again on the same connection.
			execSeed(t, db, "correct invalid identity", `UPDATE workflow_node_groups SET id = ?`, uuid.NewString())
			if err := runMigrations(db); err != nil {
				t.Fatalf("retry corrected migration: %v", err)
			}
		})
	}
}
