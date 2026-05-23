package metadata

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

func TestOpenSuppressesGooseStatusLogging(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer
	previousDebug := metadataMigrationDebugLogs
	previousWriter := metadataMigrationLogWriter
	metadataMigrationDebugLogs = false
	metadataMigrationLogWriter = &buf
	t.Cleanup(func() {
		metadataMigrationDebugLogs = previousDebug
		metadataMigrationLogWriter = previousWriter
	})

	store, err := Open(root)
	if err != nil {
		t.Fatalf("open metadata store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close metadata store: %v", err)
	}
	if strings.Contains(buf.String(), "goose:") {
		t.Fatalf("did not expect goose status log output, got %q", buf.String())
	}
}

func TestOpenAllowsDatabaseAtRemovedMigrationVersion(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatalf("initial open: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close initial store: %v", err)
	}

	dbPath := filepath.Join(root, "db", "main.sqlite3")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS mutation_dedupe (
			method TEXT NOT NULL,
			resource_id TEXT NOT NULL,
			client_request_id TEXT NOT NULL,
			payload_fingerprint TEXT NOT NULL,
			response_json BLOB,
			error_text TEXT NOT NULL,
			completed_at_unix_ms INTEGER NOT NULL,
			expires_at_unix_ms INTEGER NOT NULL,
			PRIMARY KEY (method, resource_id, client_request_id)
		)
	`); err != nil {
		t.Fatalf("create legacy mutation_dedupe table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO goose_db_version (version_id, is_applied) VALUES (3, 1)`); err != nil {
		t.Fatalf("insert removed migration version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite db: %v", err)
	}

	reopened, err := Open(root)
	if err != nil {
		t.Fatalf("reopen metadata store with removed migration version: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close reopened store: %v", err)
	}
}

func TestOpenMigratesRuntimeLeaseLivenessColumnsAway(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "db", "main.sqlite3")
	db, err := openDatabaseAtVersionForTest(t, root, dbPath, 3)
	if err != nil {
		t.Fatalf("open test database at version 3: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('project-1', 'Project', 1, 1, '{}');
INSERT INTO workspaces (id, project_id, canonical_root_path, display_name, availability, is_primary, git_metadata_json, created_at_unix_ms, updated_at_unix_ms)
VALUES ('workspace-1', 'project-1', '/tmp/workspace-1', 'workspace', 'available', 1, '{}', 1, 1);
INSERT INTO sessions (id, project_id, workspace_id, artifact_relpath, name, first_prompt_preview, input_draft, parent_session_id, created_at_unix_ms, updated_at_unix_ms, last_sequence, model_request_count, in_flight_step, agents_injected, cwd_relpath, continuation_json, locked_json, usage_state_json, metadata_json)
VALUES ('session-1', 'project-1', 'workspace-1', 'projects/project-1/sessions/session-1', '', '', '', '', 1, 1, 0, 0, 0, 0, '.', '{}', '{}', '{}', '{}');
INSERT INTO runtime_leases (id, session_id, client_id, request_id, state, created_at_unix_ms, acquired_at_unix_ms, released_at_unix_ms, expires_at_unix_ms, metadata_json)
VALUES ('lease-1', 'session-1', '', 'request-1', 'active', 1, 1, 0, 0, '{}');
`); err != nil {
		t.Fatalf("seed version 3 runtime lease: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close version 3 db: %v", err)
	}

	store, err := Open(root)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer func() { _ = store.Close() }()
	columns := runtimeLeaseColumns(t, store.db)
	for _, removed := range []string{"state", "released_at_unix_ms", "expires_at_unix_ms"} {
		if columns[removed] {
			t.Fatalf("runtime_leases column %q should have been removed; columns=%+v", removed, columns)
		}
	}
	if _, err := store.ValidateRuntimeLease(t.Context(), "session-1", "lease-1"); err != nil {
		t.Fatalf("ValidateRuntimeLease after migration: %v", err)
	}
}

func TestOpenMigratesCommentsAndRuntimeLeasesToMinimalStorage(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "db", "main.sqlite3")
	db, err := openDatabaseAtVersionForTest(t, root, dbPath, 19)
	if err != nil {
		t.Fatalf("open test database at version 19: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('project-minimal', 'Project', 1, 1, '{}');
INSERT INTO sessions (id, project_id, artifact_relpath, created_at_unix_ms, updated_at_unix_ms)
VALUES ('session-minimal', 'project-minimal', 'projects/project-minimal/sessions/session-minimal', 1, 1);
INSERT INTO runtime_leases (id, session_id, client_id, request_id, created_at_unix_ms, acquired_at_unix_ms, metadata_json)
VALUES ('lease-minimal', 'session-minimal', 'client', 'request', 1, 2, '{"trace":true}');
INSERT INTO workflows (id, name, description, graph_revision, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('workflow-minimal', 'Workflow', '', 1, 1, 1, '{}');
INSERT INTO project_workflow_links (id, project_id, workflow_id, created_at_unix_ms, updated_at_unix_ms)
VALUES ('link-minimal', 'project-minimal', 'workflow-minimal', 1, 1);
INSERT INTO tasks (id, project_workflow_link_id, workflow_revision_seen, task_seq, short_id, title, body, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('task-minimal', 'link-minimal', 1, 1, 'MIN-1', 'Task', '', 1, 1, '{}');
INSERT INTO task_comments (id, task_id, body, author_kind, author_id, source_run_id, created_at_unix_ms, updated_at_unix_ms, deleted_at_unix_ms, metadata_json)
VALUES ('comment-visible', 'task-minimal', 'visible', 'user', 'nek', NULL, 1, 3, 0, '{"keep":false}'),
       ('comment-deleted', 'task-minimal', 'deleted', 'user', 'nek', NULL, 1, 4, 4, '{"keep":false}');
`); err != nil {
		t.Fatalf("seed version 19 minimal storage data: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close version 19 db: %v", err)
	}

	store, err := Open(root)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer func() { _ = store.Close() }()
	for _, column := range []string{"source_run_id", "deleted_at_unix_ms", "metadata_json"} {
		if columnExists(t, store.db, "task_comments", column) {
			t.Fatalf("task_comments.%s should have been removed", column)
		}
	}
	for _, column := range []string{"client_id", "request_id", "acquired_at_unix_ms", "metadata_json"} {
		if columnExists(t, store.db, "runtime_leases", column) {
			t.Fatalf("runtime_leases.%s should have been removed", column)
		}
	}
	comments, err := store.DB().QueryContext(t.Context(), `SELECT id, body FROM task_comments ORDER BY updated_at_unix_ms DESC`)
	if err != nil {
		t.Fatalf("query migrated comments: %v", err)
	}
	defer func() { _ = comments.Close() }()
	if !comments.Next() {
		t.Fatal("expected one visible comment after migration")
	}
	var commentID, body string
	if err := comments.Scan(&commentID, &body); err != nil {
		t.Fatalf("scan migrated comment: %v", err)
	}
	if commentID != "comment-visible" || body != "visible" {
		t.Fatalf("migrated comment = %q/%q, want visible comment", commentID, body)
	}
	if comments.Next() {
		t.Fatal("deleted comment should not survive hard-delete migration")
	}
	if err := comments.Err(); err != nil {
		t.Fatalf("iterate migrated comments: %v", err)
	}
	if _, err := store.ValidateRuntimeLease(t.Context(), "session-minimal", "lease-minimal"); err != nil {
		t.Fatalf("ValidateRuntimeLease after minimal storage migration: %v", err)
	}
}

func TestOpenDropsPersistedWorkflowEvents(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "db", "main.sqlite3")
	db, err := openDatabaseAtVersionForTest(t, root, dbPath, 20)
	if err != nil {
		t.Fatalf("open test database at version 20: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO workflow_events (project_id, workflow_id, resource, action, changed_ids_json, occurred_at_unix_ms)
VALUES ('project-events', 'workflow-events', 'task', 'updated', '["task-1"]', 1);
`); err != nil {
		t.Fatalf("seed workflow events: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close version 20 db: %v", err)
	}

	store, err := Open(root)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer func() { _ = store.Close() }()
	if tableExists(t, store.db, "workflow_events") {
		t.Fatal("workflow_events should have been dropped")
	}
}

func TestOpenRemovesRedundantIndexesAndArchiveMetadata(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "db", "main.sqlite3")
	db, err := openDatabaseAtVersionForTest(t, root, dbPath, 21)
	if err != nil {
		t.Fatalf("open test database at version 21: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO workflows (id, name, description, graph_revision, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('workflow-archive', 'Workflow', '', 1, 1, 1, '{}');
INSERT INTO workflow_nodes (id, workflow_id, node_key, kind, display_name, output_fields_json, metadata_json)
VALUES ('node-archive', 'workflow-archive', 'archived', 'terminal', 'Archived', '[]', '{"archived_at_unix_ms": 1, "other": true}');
`); err != nil {
		t.Fatalf("seed archive metadata: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close version 21 db: %v", err)
	}

	store, err := Open(root)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer func() { _ = store.Close() }()
	for _, index := range []string{
		"runtime_leases_session_idx",
		"workspaces_project_idx",
		"workflow_transition_groups_source_transition_idx",
		"tasks_project_short_id_idx",
	} {
		if indexExists(t, store.db, index) {
			t.Fatalf("index %s should have been dropped", index)
		}
	}
	if columnExists(t, store.db, "workflow_nodes", "metadata_json") {
		t.Fatal("workflow_nodes.metadata_json should have been removed by workflow definition metadata migration")
	}
}

func TestOpenRejectsInconsistentWorkflowGraphDenormalization(t *testing.T) {
	tests := []struct {
		name string
		seed string
	}{
		{
			name: "transition group workflow disagrees with source node",
			seed: `INSERT INTO workflow_transition_groups (id, workflow_id, source_node_id, transition_id, display_name)
VALUES ('group-bad', 'workflow-b', 'node-a', 'bad', 'Bad');`,
		},
		{
			name: "edge workflow disagrees with transition group source node",
			seed: `
INSERT INTO workflow_transition_groups (id, workflow_id, source_node_id, transition_id, display_name)
VALUES ('group-a', 'workflow-a', 'node-a', 'next', 'Next');
INSERT INTO workflow_edges (id, workflow_id, transition_group_id, edge_key, target_node_id, context_mode, input_bindings_json, output_requirements_json)
VALUES ('edge-bad', 'workflow-b', 'group-a', 'next', 'node-a', 'new_session', '{}', '{}');`,
		},
		{
			name: "edge target node belongs to different workflow",
			seed: `
INSERT INTO workflow_transition_groups (id, workflow_id, source_node_id, transition_id, display_name)
VALUES ('group-a', 'workflow-a', 'node-a', 'next', 'Next');
INSERT INTO workflow_edges (id, workflow_id, transition_group_id, edge_key, target_node_id, context_mode, input_bindings_json, output_requirements_json)
VALUES ('edge-bad', 'workflow-a', 'group-a', 'next', 'node-b', 'new_session', '{}', '{}');`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			dbPath := filepath.Join(root, "db", "main.sqlite3")
			db, err := openDatabaseAtVersionForTest(t, root, dbPath, 23)
			if err != nil {
				t.Fatalf("open test database at version 23: %v", err)
			}
			if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
				t.Fatalf("disable foreign keys: %v", err)
			}
			if _, err := db.Exec(`
INSERT INTO workflows (id, name, description, graph_revision, created_at_unix_ms, updated_at_unix_ms)
VALUES ('workflow-a', 'A', '', 1, 1, 1),
       ('workflow-b', 'B', '', 1, 1, 1);
INSERT INTO workflow_nodes (id, workflow_id, node_key, kind, display_name, output_fields_json)
VALUES ('node-a', 'workflow-a', 'start', 'start', 'Start A', '[]'),
       ('node-b', 'workflow-b', 'done', 'terminal', 'Done B', '[]');
`); err != nil {
				t.Fatalf("seed version 23 graph base: %v", err)
			}
			if _, err := db.Exec(tt.seed); err != nil {
				t.Fatalf("seed version 23 contradiction: %v", err)
			}
			if err := db.Close(); err != nil {
				t.Fatalf("close version 23 db: %v", err)
			}

			if store, err := Open(root); err == nil {
				_ = store.Close()
				t.Fatal("expected migration to reject inconsistent workflow graph denormalization")
			}
		})
	}
}

func TestOpenMigratesWorkspaceHistorySnapshots(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "db", "main.sqlite3")
	db, err := openDatabaseAtVersionForTest(t, root, dbPath, 8)
	if err != nil {
		t.Fatalf("open test database at version 8: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO projects (id, display_name, project_key, next_task_seq, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('project-1', 'Project', 'PR', 1, 1, 1, '{}');
INSERT INTO workspaces (id, project_id, canonical_root_path, display_name, availability, is_primary, git_metadata_json, created_at_unix_ms, updated_at_unix_ms)
VALUES ('workspace-1', 'project-1', '/tmp/workspace-1', 'Workspace One', 'available', 1, '{}', 1, 1);
INSERT INTO sessions (id, project_id, workspace_id, artifact_relpath, name, first_prompt_preview, input_draft, parent_session_id, created_at_unix_ms, updated_at_unix_ms, last_sequence, model_request_count, in_flight_step, agents_injected, launch_visible, cwd_relpath, continuation_json, locked_json, usage_state_json, metadata_json)
VALUES ('session-1', 'project-1', 'workspace-1', 'projects/project-1/sessions/session-1', 'Session', '', '', '', 1, 1, 0, 1, 0, 0, 1, '.', '{}', '{}', '{}', '{invalid json');
INSERT INTO workflows (id, name, description, graph_revision, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('workflow-1', 'Workflow', '', 1, 1, 1, '{}');
INSERT INTO project_workflow_links (id, project_id, workflow_id, is_default, created_at_unix_ms, updated_at_unix_ms)
VALUES ('link-1', 'project-1', 'workflow-1', 1, 1, 1);
INSERT INTO tasks (id, project_id, project_workflow_link_id, workflow_id, workflow_revision_seen, task_seq, short_id, title, body, source_workspace_id, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('task-1', 'project-1', 'link-1', 'workflow-1', 1, 1, 'PR-1', 'Task', '', 'workspace-1', 1, 1, '{}');
`); err != nil {
		t.Fatalf("seed version 8 workspace history: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close version 8 db: %v", err)
	}

	store, err := Open(root)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer func() { _ = store.Close() }()
	record, err := store.ResolvePersistedSession(t.Context(), "session-1")
	if err != nil {
		t.Fatalf("ResolvePersistedSession after migration: %v", err)
	}
	if record.Meta.WorkspaceRoot != "/tmp/workspace-1" || record.Meta.WorkspaceContainer != "Workspace One" {
		t.Fatalf("session workspace snapshot = %q/%q", record.Meta.WorkspaceRoot, record.Meta.WorkspaceContainer)
	}
	var taskMetadata string
	if err := store.db.QueryRow(`SELECT metadata_json FROM tasks WHERE id = 'task-1'`).Scan(&taskMetadata); err != nil {
		t.Fatalf("scan task metadata: %v", err)
	}
	var taskMetadataJSON struct {
		SourceWorkspaceSnapshot struct {
			RootPath    string `json:"root_path"`
			DisplayName string `json:"display_name"`
		} `json:"source_workspace_snapshot"`
	}
	if err := json.Unmarshal([]byte(taskMetadata), &taskMetadataJSON); err != nil {
		t.Fatalf("unmarshal task metadata: %v", err)
	}
	if taskMetadataJSON.SourceWorkspaceSnapshot.RootPath != "/tmp/workspace-1" || taskMetadataJSON.SourceWorkspaceSnapshot.DisplayName != "Workspace One" {
		t.Fatalf("task workspace snapshot = %+v", taskMetadataJSON.SourceWorkspaceSnapshot)
	}
}

func TestOpenMigratesPrimaryWorkspacePointerDeterministically(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "db", "main.sqlite3")
	db, err := openDatabaseAtVersionForTest(t, root, dbPath, 17)
	if err != nil {
		t.Fatalf("open test database at version 17: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('project-primary', 'Primary', 1, 1, '{}'),
       ('project-fallback', 'Fallback', 2, 2, '{}'),
       ('project-empty', 'Empty', 3, 3, '{}');
INSERT INTO workspaces (id, project_id, canonical_root_path, display_name, availability, is_primary, git_metadata_json, created_at_unix_ms, updated_at_unix_ms)
VALUES ('workspace-oldest-nonprimary', 'project-primary', '/tmp/oldest-nonprimary', 'Oldest Nonprimary', 'available', 0, '{}', 1, 1),
       ('workspace-oldest-primary', 'project-primary', '/tmp/oldest-primary', 'Oldest Primary', 'available', 1, '{}', 2, 2),
       ('workspace-newest-primary', 'project-primary', '/tmp/newest-primary', 'Newest Primary', 'available', 1, '{}', 3, 3),
       ('workspace-fallback-newest', 'project-fallback', '/tmp/fallback-newest', 'Fallback Newest', 'available', 0, '{}', 5, 5),
       ('workspace-fallback-oldest', 'project-fallback', '/tmp/fallback-oldest', 'Fallback Oldest', 'available', 0, '{}', 4, 4);
`); err != nil {
		t.Fatalf("seed version 17 primary workspace data: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close version 17 db: %v", err)
	}

	store, err := Open(root)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer func() { _ = store.Close() }()
	got := primaryWorkspaceIDsByProject(t, store.db)
	if got["project-primary"] != "workspace-oldest-primary" {
		t.Fatalf("project-primary primary workspace = %q, want workspace-oldest-primary", got["project-primary"])
	}
	if got["project-fallback"] != "workspace-fallback-oldest" {
		t.Fatalf("project-fallback primary workspace = %q, want workspace-fallback-oldest", got["project-fallback"])
	}
	if got["project-empty"] != "" {
		t.Fatalf("project-empty primary workspace = %q, want empty", got["project-empty"])
	}
}

func TestOpenRejectsWorkspaceSessionRelationshipContradictions(t *testing.T) {
	tests := []struct {
		name string
		seed string
	}{
		{
			name: "session workspace outside project",
			seed: `
INSERT INTO sessions (id, project_id, workspace_id, artifact_relpath, created_at_unix_ms, updated_at_unix_ms)
VALUES ('session-cross-workspace', 'project-a', 'workspace-b', 'projects/project-a/sessions/session-cross-workspace', 1, 1);
`,
		},
		{
			name: "session worktree outside workspace",
			seed: `
INSERT INTO worktrees (id, workspace_id, canonical_root_path, display_name, availability, is_main, git_metadata_json, created_at_unix_ms, updated_at_unix_ms)
VALUES ('worktree-b', 'workspace-b', '/tmp/worktree-b', 'worktree-b', 'available', 0, '{}', 1, 1);
INSERT INTO sessions (id, project_id, workspace_id, worktree_id, artifact_relpath, created_at_unix_ms, updated_at_unix_ms)
VALUES ('session-cross-worktree', 'project-a', 'workspace-a', 'worktree-b', 'projects/project-a/sessions/session-cross-worktree', 1, 1);
`,
		},
		{
			name: "managed task worktree outside source workspace",
			seed: `
INSERT INTO worktrees (id, workspace_id, canonical_root_path, display_name, availability, is_main, git_metadata_json, created_at_unix_ms, updated_at_unix_ms)
VALUES ('worktree-b', 'workspace-b', '/tmp/worktree-b', 'worktree-b', 'available', 0, '{}', 1, 1);
INSERT INTO workflows (id, name, description, graph_revision, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('workflow-a', 'Workflow', '', 1, 1, 1, '{}');
INSERT INTO project_workflow_links (id, project_id, workflow_id, created_at_unix_ms, updated_at_unix_ms)
VALUES ('link-a', 'project-a', 'workflow-a', 1, 1);
INSERT INTO tasks (id, project_workflow_link_id, workflow_revision_seen, task_seq, short_id, title, body, source_workspace_id, managed_worktree_id, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('task-cross-worktree', 'link-a', 1, 1, 'A-1', 'Task', '', 'workspace-a', 'worktree-b', 1, 1, '{}');
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			dbPath := filepath.Join(root, "db", "main.sqlite3")
			db, err := openDatabaseAtVersionForTest(t, root, dbPath, 17)
			if err != nil {
				t.Fatalf("open test database at version 17: %v", err)
			}
			if _, err := db.Exec(`
INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('project-a', 'A', 1, 1, '{}'),
       ('project-b', 'B', 1, 1, '{}');
INSERT INTO workspaces (id, project_id, canonical_root_path, display_name, availability, is_primary, git_metadata_json, created_at_unix_ms, updated_at_unix_ms)
VALUES ('workspace-a', 'project-a', '/tmp/workspace-a', 'workspace-a', 'available', 1, '{}', 1, 1),
       ('workspace-b', 'project-b', '/tmp/workspace-b', 'workspace-b', 'available', 1, '{}', 1, 1);
`); err != nil {
				t.Fatalf("seed version 17 base data: %v", err)
			}
			if _, err := db.Exec(tt.seed); err != nil {
				t.Fatalf("seed version 17 contradiction: %v", err)
			}
			if err := db.Close(); err != nil {
				t.Fatalf("close version 17 db: %v", err)
			}

			if store, err := Open(root); err == nil {
				_ = store.Close()
				t.Fatal("expected migration to reject contradictory workspace/session data")
			}
		})
	}
}

func TestOpenBackfillsSessionWorkspaceFromSameProjectWorktree(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "db", "main.sqlite3")
	db, err := openDatabaseAtVersionForTest(t, root, dbPath, 17)
	if err != nil {
		t.Fatalf("open test database at version 17: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('project-a', 'A', 1, 1, '{}');
INSERT INTO workspaces (id, project_id, canonical_root_path, display_name, availability, is_primary, git_metadata_json, created_at_unix_ms, updated_at_unix_ms)
VALUES ('workspace-a', 'project-a', '/tmp/workspace-a', 'workspace-a', 'available', 1, '{}', 1, 1);
INSERT INTO worktrees (id, workspace_id, canonical_root_path, display_name, availability, is_main, git_metadata_json, created_at_unix_ms, updated_at_unix_ms)
VALUES ('worktree-a', 'workspace-a', '/tmp/worktree-a', 'worktree-a', 'available', 0, '{}', 1, 1);
INSERT INTO sessions (id, project_id, workspace_id, worktree_id, artifact_relpath, created_at_unix_ms, updated_at_unix_ms)
VALUES ('session-a', 'project-a', NULL, 'worktree-a', 'projects/project-a/sessions/session-a', 1, 1);
`); err != nil {
		t.Fatalf("seed version 17 session worktree data: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close version 17 db: %v", err)
	}

	store, err := Open(root)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer func() { _ = store.Close() }()
	var workspaceID sql.NullString
	if err := store.db.QueryRow(`SELECT workspace_id FROM sessions WHERE id = 'session-a'`).Scan(&workspaceID); err != nil {
		t.Fatalf("scan migrated session workspace: %v", err)
	}
	if !workspaceID.Valid || workspaceID.String != "workspace-a" {
		t.Fatalf("session workspace = %+v, want workspace-a", workspaceID)
	}
}

func TestOpenMigratesWorkspaceWorktreeDerivedStorageAway(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "db", "main.sqlite3")
	workspaceRoot := filepath.Join(t.TempDir(), "derived-workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll workspace root: %v", err)
	}
	db, err := openDatabaseAtVersionForTest(t, root, dbPath, 18)
	if err != nil {
		t.Fatalf("open test database at version 18: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('project-derived', 'Project', 1, 1, '{}');
INSERT INTO workspaces (id, project_id, canonical_root_path, display_name, availability, is_primary, git_metadata_json, created_at_unix_ms, updated_at_unix_ms)
VALUES ('workspace-derived', 'project-derived', ?, 'Stored Workspace Label', 'missing', 0, '{"workspace":"metadata"}', 1, 1);
UPDATE projects SET primary_workspace_id = 'workspace-derived' WHERE id = 'project-derived';
INSERT INTO worktrees (id, workspace_id, canonical_root_path, display_name, availability, is_main, builder_managed, created_branch, origin_session_id, git_metadata_json, created_at_unix_ms, updated_at_unix_ms)
VALUES ('worktree-derived', 'workspace-derived', ?, 'Stored Worktree Label', 'missing', 0, 1, 1, 'session-derived', '{"branch_name":"main"}', 1, 1);
`, workspaceRoot, workspaceRoot); err != nil {
		t.Fatalf("seed version 18 derived workspace data: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close version 18 db: %v", err)
	}

	store, err := Open(root)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer func() { _ = store.Close() }()
	for _, column := range []string{"display_name", "availability", "is_primary"} {
		if columnExists(t, store.db, "workspaces", column) {
			t.Fatalf("workspaces.%s should have been removed", column)
		}
	}
	for _, column := range []string{"display_name", "availability", "is_main"} {
		if columnExists(t, store.db, "worktrees", column) {
			t.Fatalf("worktrees.%s should have been removed", column)
		}
	}
	workspaces, err := store.ListProjectWorkspaces(t.Context(), "project-derived")
	if err != nil {
		t.Fatalf("ListProjectWorkspaces: %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("workspace count = %d, want 1", len(workspaces))
	}
	if workspaces[0].DisplayName != filepath.Base(workspaceRoot) || string(workspaces[0].Availability) != "available" || !workspaces[0].IsPrimary {
		t.Fatalf("derived workspace summary = %+v", workspaces[0])
	}
	home, err := store.ListProjectHomeSummaries(t.Context(), "project-derived", 1, 0)
	if err != nil {
		t.Fatalf("ListProjectHomeSummaries: %v", err)
	}
	if len(home) != 1 || home[0].PrimaryWorkspace.DisplayName != filepath.Base(workspaceRoot) || home[0].PrimaryWorkspace.Availability != "available" {
		t.Fatalf("derived home summary = %+v", home)
	}
	worktree, err := store.GetWorktreeRecordByID(t.Context(), "worktree-derived")
	if err != nil {
		t.Fatalf("GetWorktreeRecordByID: %v", err)
	}
	if worktree.DisplayName != filepath.Base(workspaceRoot) || worktree.Availability != "available" || !worktree.IsMain {
		t.Fatalf("derived worktree record = %+v", worktree)
	}
	if !strings.Contains(worktree.GitMetadataJSON, "branch_name") {
		t.Fatalf("worktree git metadata not preserved: %q", worktree.GitMetadataJSON)
	}
}

func openDatabaseAtVersionForTest(t *testing.T, root string, dbPath string, version int64) (*sql.DB, error) {
	t.Helper()
	db, err := openDatabaseAtPathWithoutMigrationsForTest(root, dbPath)
	if err != nil {
		return nil, err
	}
	migrations, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrations, goose.WithLogger(goose.NopLogger()), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := provider.UpTo(context.Background(), version); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func openDatabaseAtPathWithoutMigrationsForTest(root string, dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if err := configureDatabase(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func runtimeLeaseColumns(t *testing.T, db *sql.DB) map[string]bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(runtime_leases)")
	if err != nil {
		t.Fatalf("query runtime_leases columns: %v", err)
	}
	defer func() { _ = rows.Close() }()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan runtime_leases column: %v", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate runtime_leases columns: %v", err)
	}
	return columns
}

func primaryWorkspaceIDsByProject(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT id, primary_workspace_id FROM projects`)
	if err != nil {
		t.Fatalf("query project primary workspace ids: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var projectID string
		var workspaceID sql.NullString
		if err := rows.Scan(&projectID, &workspaceID); err != nil {
			t.Fatalf("scan project primary workspace id: %v", err)
		}
		out[projectID] = workspaceID.String
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate project primary workspace ids: %v", err)
	}
	return out
}
