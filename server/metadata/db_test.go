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
