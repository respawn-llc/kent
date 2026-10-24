package metadata

import (
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestWorkspaceChatDraftCutoverDeletesLegacyDraftAndPreservesSessionState(t *testing.T) {
	root := t.TempDir()
	db, err := openDatabaseAtVersionForTest(t, 88)
	if err != nil {
		t.Fatalf("open version 88 database: %v", err)
	}
	now := time.Now().UTC().UnixMilli()
	workspaceRoot := filepath.Join(root, "workspace")
	execSeed(t, db, "project", `
INSERT INTO projects (
    id,
    display_name,
    created_at_unix_ms,
    updated_at_unix_ms,
    metadata_json
) VALUES ('project-chat-draft-cutover', 'Project', ?, ?, '{}')`,
		now,
		now,
	)
	execSeed(t, db, "workspace Chat draft", `
INSERT INTO workspaces (
    id,
    project_id,
    canonical_root_path,
    git_metadata_json,
    created_at_unix_ms,
    updated_at_unix_ms,
    chat_draft_json
) VALUES (
    'workspace-chat-draft-cutover',
    'project-chat-draft-cutover',
    ?,
    '{}',
    ?,
    ?,
    '{"message":"discard legacy workspace draft"}'
)`,
		workspaceRoot,
		now,
		now,
	)
	execSeed(t, db, "ordinary Session draft and settings", `
INSERT INTO sessions (
    id,
    project_id,
    workspace_id,
    artifact_relpath,
    input_draft,
    category,
    created_at_unix_ms,
    updated_at_unix_ms,
    metadata_json
) VALUES (
    '8b0a92d4-18f8-4b5f-9b66-b8ac0f3f987e',
    'project-chat-draft-cutover',
    'workspace-chat-draft-cutover',
    'projects/project-chat-draft-cutover/sessions/8b0a92d4-18f8-4b5f-9b66-b8ac0f3f987e',
    'preserve ordinary Session draft',
    'main',
    ?,
    ?,
    '{"chat_settings":{"supervisor":"all","thinking":"high","fast":false,"questions":false,"auto_compaction":false}}'
)`,
		now,
		now,
	)
	store := migrateMetadataTestStore(t, root, db)

	if columnExists(t, store.db, "workspaces", "chat_draft_json") {
		t.Fatal("legacy workspace Chat draft column survived cutover")
	}
	var inputDraft, supervisor, thinking string
	var fast, questions, autoCompaction bool
	if err := store.db.QueryRow(`
SELECT
    input_draft,
    json_extract(metadata_json, '$.chat_settings.supervisor'),
    json_extract(metadata_json, '$.chat_settings.thinking'),
    json_extract(metadata_json, '$.chat_settings.fast'),
    json_extract(metadata_json, '$.chat_settings.questions'),
    json_extract(metadata_json, '$.chat_settings.auto_compaction')
FROM sessions
WHERE id = '8b0a92d4-18f8-4b5f-9b66-b8ac0f3f987e'`,
	).Scan(
		&inputDraft,
		&supervisor,
		&thinking,
		&fast,
		&questions,
		&autoCompaction,
	); err != nil {
		t.Fatalf("read preserved ordinary Session state: %v", err)
	}
	if inputDraft != "preserve ordinary Session draft" ||
		supervisor != "all" ||
		thinking != "high" ||
		fast ||
		questions ||
		autoCompaction {
		t.Fatalf(
			"ordinary Session state = draft=%q supervisor=%q thinking=%q fast=%t questions=%t auto_compaction=%t",
			inputDraft,
			supervisor,
			thinking,
			fast,
			questions,
			autoCompaction,
		)
	}
}

func TestWorkspaceChatDraftCutoverFailureLeavesDatabaseUnchanged(t *testing.T) {
	db, err := openDatabaseAtVersionForTest(t, 88)
	if err != nil {
		t.Fatalf("open version 88 database: %v", err)
	}
	execSeed(t, db, "migration blocker", `
CREATE VIEW workspace_chat_draft_migration_blocker AS
SELECT chat_draft_json
FROM workspaces`)
	err = runMigrations(db)
	if err == nil {
		t.Fatal("workspace Chat draft cutover succeeded despite migration blocker")
	}
	version, err := readMetadataVersion(db)
	if err != nil {
		t.Fatalf("read failed migration version: %v", err)
	}
	if version != 88 {
		t.Fatalf("failed migration version = %d, want 88", version)
	}
	if !columnExists(t, db, "workspaces", "chat_draft_json") {
		t.Fatal("failed migration partially removed workspace Chat draft schema")
	}
}

func TestIndependentMetadataStoresOpenConcurrently(t *testing.T) {
	t.Parallel()
	const databaseCount = 8
	roots := make([]string, databaseCount)
	databases := make([]*sql.DB, databaseCount)
	for index := range roots {
		roots[index] = t.TempDir()
		databases[index] = openLatestMetadataTestDatabase(t)
		t.Cleanup(func() { _ = databases[index].Close() })
	}

	start := make(chan struct{})
	errs := make(chan error, databaseCount)
	var workers sync.WaitGroup
	workers.Add(databaseCount)
	for index, root := range roots {
		go func() {
			defer workers.Done()
			<-start
			store, err := NewStore(root, databases[index])
			if err == nil {
				err = store.Close()
			}
			errs <- err
		}()
	}
	close(start)
	workers.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Open: %v", err)
		}
	}
}

func TestMetadataSQLiteDSNNormalizesWindowsPaths(t *testing.T) {
	t.Parallel()
	dsn, err := metadataSQLiteDSN(`C:\Users\Nek\kent db\main ? #.sqlite3`)
	if err != nil {
		t.Fatalf("metadataSQLiteDSN: %v", err)
	}
	if !strings.HasPrefix(dsn, "file:///C:/Users/Nek/kent%20db/main%20%3F%20%23.sqlite3?") {
		t.Fatalf("dsn = %q, want file URL with normalized Windows drive path", dsn)
	}
	if !strings.Contains(dsn, "_pragma=foreign_keys%281%29") {
		t.Fatalf("dsn = %q, want pragma query values preserved", dsn)
	}
}

func TestReadMetadataVersionPreservesUnsupportedDatabaseRecords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		recordedVersion *int64
		wantVersion     int64
	}{
		{name: "unversioned", wantVersion: 0},
		{name: "version zero", recordedVersion: new(int64), wantVersion: 0},
		{name: "version 34", recordedVersion: int64Pointer(34), wantVersion: 34},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			db := createUnsupportedMetadataDatabase(t, test.recordedVersion)
			version, err := readMetadataVersion(db)
			if err != nil || version != test.wantVersion {
				t.Fatalf("metadata version = %d, error = %v", version, err)
			}
			var marker string
			if err := db.QueryRow(`SELECT value FROM metadata_floor_probe WHERE id = 1`).Scan(&marker); err != nil {
				t.Fatalf("read rejection marker: %v", err)
			}
			if marker != "preserve" {
				t.Fatalf("rejection marker = %q, want preserve", marker)
			}
			if test.recordedVersion == nil {
				if tableExists(t, db, "goose_db_version") {
					t.Fatal("rejection created Goose version storage")
				}
				return
			}
			var latest int64
			if err := db.QueryRow(`
SELECT MAX(version_id)
FROM goose_db_version
WHERE is_applied = 1`).Scan(&latest); err != nil {
				t.Fatalf("read rejected migration version: %v", err)
			}
			if latest != test.wantVersion {
				t.Fatalf("rejected migration version = %d, want %d", latest, test.wantVersion)
			}
		})
	}
}

func TestMigrationsUpgradeVersion35DatabaseWithHistoricalLedger(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	db, err := openDatabaseAtVersionForTest(t, 35)
	if err != nil {
		t.Fatalf("open version 35 database: %v", err)
	}
	for version := int64(1); version < 35; version++ {
		if _, err := db.Exec(`
INSERT INTO goose_db_version (version_id, is_applied)
SELECT ?, 1
WHERE NOT EXISTS (
    SELECT 1
    FROM goose_db_version
    WHERE version_id = ?
      AND is_applied = 1
)`, version, version); err != nil {
			t.Fatalf("record historical migration version %d: %v", version, err)
		}
	}
	if _, err := db.Exec(`
INSERT INTO projects (
    id, display_name, created_at_unix_ms, updated_at_unix_ms, metadata_json
) VALUES (
    'project-v35', 'Version 35', 1, 1, '{}'
);
INSERT INTO workspaces (
    id, project_id, canonical_root_path, git_metadata_json,
    created_at_unix_ms, updated_at_unix_ms
) VALUES (
    'workspace-v35', 'project-v35', '/workspace-v35', '{}', 1, 1
);
UPDATE projects
SET primary_workspace_id = 'workspace-v35'
WHERE id = 'project-v35';
INSERT INTO sessions (
    id, project_id, workspace_id, artifact_relpath, name,
    created_at_unix_ms, updated_at_unix_ms
) VALUES (
    'session-v35', 'project-v35', 'workspace-v35',
    'sessions/session-v35', 'Preserved session', 1, 1
)`); err != nil {
		t.Fatalf("seed version 35 data: %v", err)
	}
	store := migrateMetadataTestStore(t, root, db)

	var projectName, sessionName string
	if err := store.db.QueryRow(`SELECT display_name FROM projects WHERE id = 'project-v35'`).Scan(&projectName); err != nil {
		t.Fatalf("read upgraded project: %v", err)
	}
	if err := store.db.QueryRow(`SELECT name FROM sessions WHERE id = 'session-v35'`).Scan(&sessionName); err != nil {
		t.Fatalf("read upgraded session: %v", err)
	}
	if projectName != "Version 35" || sessionName != "Preserved session" {
		t.Fatalf("upgraded data = %q/%q", projectName, sessionName)
	}
}

func createUnsupportedMetadataDatabase(t *testing.T, recordedVersion *int64) *sql.DB {
	t.Helper()
	db := openEmptyMetadataTestDatabase(t)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
CREATE TABLE metadata_floor_probe (
    id INTEGER PRIMARY KEY,
    value TEXT NOT NULL
);
INSERT INTO metadata_floor_probe (id, value) VALUES (1, 'preserve')`); err != nil {
		_ = db.Close()
		t.Fatalf("create unsupported database marker: %v", err)
	}
	if recordedVersion != nil {
		if _, err := db.Exec(`
CREATE TABLE goose_db_version (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    version_id INTEGER NOT NULL,
    is_applied INTEGER NOT NULL,
    tstamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
)`); err != nil {
			_ = db.Close()
			t.Fatalf("create Goose version storage: %v", err)
		}
		for version := int64(0); version <= *recordedVersion; version++ {
			if _, err := db.Exec(
				`INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)`,
				version,
			); err != nil {
				_ = db.Close()
				t.Fatalf("record migration version %d: %v", version, err)
			}
		}
	}
	return db
}

func int64Pointer(value int64) *int64 {
	return &value
}

func openDatabaseAtVersionForTest(t *testing.T, version int64) (*sql.DB, error) {
	t.Helper()
	snapshot, err := emptyMetadataSnapshotAtVersion(t, version)
	if err != nil {
		return nil, err
	}
	db := openEmptyMetadataTestDatabase(t)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close in-memory migration database: %v", err)
		}
	})
	if err := withMetadataSnapshotConnection(t.Context(), db, func(conn metadataSnapshotDriver) error {
		return conn.Deserialize(snapshot)
	}); err != nil {
		return nil, err
	}
	return db, nil
}

func migrateMetadataTestStore(t *testing.T, root string, db *sql.DB) *Store {
	t.Helper()
	if err := runMigrations(db); err != nil {
		t.Fatalf("migrate metadata database: %v", err)
	}
	store, err := NewStore(root, db)
	if err != nil {
		t.Fatalf("initialize migrated metadata store: %v", err)
	}
	return store
}
