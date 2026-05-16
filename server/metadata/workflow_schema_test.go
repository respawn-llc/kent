package metadata

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestOpenCreatesWorkflowSchemaAndForeignKeys(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	for _, table := range []string{
		"workflows",
		"workflow_nodes",
		"workflow_transition_groups",
		"workflow_edges",
		"project_workflow_links",
		"tasks",
		"task_node_placements",
		"task_runs",
		"task_transitions",
		"task_transition_edges",
		"task_comments",
	} {
		if !tableExists(t, store.db, table) {
			t.Fatalf("expected table %s to exist", table)
		}
	}
	if columnExists(t, store.db, "workflows", "start_node_id") {
		t.Fatal("workflows.start_node_id should not exist; start node is derived from workflow_nodes.kind")
	}
	if columnExists(t, store.db, "workflow_edges", "source_node_id") {
		t.Fatal("workflow_edges.source_node_id should not exist; source is derived from transition group")
	}
	var foreignKeys int
	if err := store.db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
}

func TestOpenBackfillsProjectKeysForExistingMetadataDB(t *testing.T) {
	root := t.TempDir()
	dbPath := root + "/db/main.sqlite3"
	db, err := openDatabaseAtVersionForTest(t, root, dbPath, 4)
	if err != nil {
		t.Fatalf("open version 4 db: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('project-a', 'Builder', 1, 1, '{}'), ('project-b', 'Builder', 2, 2, '{}');
INSERT INTO workspaces (id, project_id, canonical_root_path, display_name, availability, is_primary, git_metadata_json, created_at_unix_ms, updated_at_unix_ms)
VALUES ('workspace-a', 'project-a', '/tmp/workflow-a', 'workflow-a', 'available', 1, '{}', 1, 1),
       ('workspace-b', 'project-b', '/tmp/workflow-b', 'workflow-b', 'available', 1, '{}', 2, 2);
`); err != nil {
		t.Fatalf("seed version 4 db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close version 4 db: %v", err)
	}

	store, err := Open(root)
	if err != nil {
		t.Fatalf("Open migrated db: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	keys := projectKeysByID(t, store.db)
	if keys["project-a"] != "BUI" {
		t.Fatalf("project-a key = %q, want BUI", keys["project-a"])
	}
	if keys["project-b"] != "BUI2" {
		t.Fatalf("project-b key = %q, want BUI2", keys["project-b"])
	}
}

func TestProjectKeyValidationCollisionAndTaskImmutability(t *testing.T) {
	store, _, binding := newMetadataTestStore(t)
	ctx := t.Context()
	other, err := store.CreateProjectForWorkspace(ctx, t.TempDir(), "Other Project")
	if err != nil {
		t.Fatalf("CreateProjectForWorkspace: %v", err)
	}

	if err := store.SetProjectKey(ctx, binding.ProjectID, "bad-key"); !errors.Is(err, ErrInvalidProjectKey) {
		t.Fatalf("expected invalid project key error, got %v", err)
	}
	if err := store.SetProjectKey(ctx, binding.ProjectID, "BLD"); err != nil {
		t.Fatalf("SetProjectKey BLD: %v", err)
	}
	if err := store.SetProjectKey(ctx, other.ProjectID, "BLD"); !errors.Is(err, ErrProjectKeyAlreadyInUse) {
		t.Fatalf("expected project key collision, got %v", err)
	}
	seedWorkflowGraph(t, store.db, binding.ProjectID, time.Now().UTC().UnixMilli())
	seedWorkflowTask(t, store, binding.ProjectID, "BLD-1")
	if err := store.SetProjectKey(ctx, binding.ProjectID, "NEW"); !errors.Is(err, ErrProjectKeyImmutable) {
		t.Fatalf("expected immutable project key error, got %v", err)
	}
	if err := store.SetProjectKey(ctx, binding.ProjectID, "BLD"); err != nil {
		t.Fatalf("SetProjectKey same value after task: %v", err)
	}
}

func TestWorkflowSchemaConstraints(t *testing.T) {
	store, _, binding := newMetadataTestStore(t)
	now := time.Now().UTC().UnixMilli()
	seedWorkflowGraph(t, store.db, binding.ProjectID, now)
	seedWorkflowTask(t, store, binding.ProjectID, "BLD-1")

	assertSQLiteConstraint(t, store.db, `INSERT INTO workflow_nodes (id, workflow_id, node_key, kind, display_name, output_fields_json, metadata_json) VALUES ('node-second-start', 'workflow-1', 'second_start', 'start', 'Second Start', '[]', '{}')`)
	assertSQLiteConstraint(t, store.db, `INSERT INTO workflow_nodes (id, workflow_id, node_key, kind, display_name, output_fields_json, metadata_json) VALUES ('node-invalid-kind', 'workflow-1', 'bad', 'robot', 'Bad', '[]', '{}')`)
	assertSQLiteConstraint(t, store.db, `INSERT INTO workflow_edges (id, workflow_id, transition_group_id, edge_key, target_node_id, requires_approval, context_mode, input_bindings_json, output_requirements_json, metadata_json) VALUES ('edge-invalid-bool', 'workflow-1', 'group-start', 'bad_bool', 'node-agent', 2, 'new_session', '{}', '{}', '{}')`)
	assertSQLiteConstraint(t, store.db, `INSERT INTO workflows (id, name, graph_revision, created_at_unix_ms, updated_at_unix_ms, metadata_json) VALUES ('workflow-bad-time', 'Bad', 1, -1, 1, '{}')`)
	assertSQLiteConstraint(t, store.db, `INSERT INTO workflows (id, name, graph_revision, created_at_unix_ms, updated_at_unix_ms, metadata_json) VALUES ('workflow-bad-rev', 'Bad', 0, 1, 1, '{}')`)
	assertSQLiteConstraint(t, store.db, `INSERT INTO workflows (id, name, graph_revision, created_at_unix_ms, updated_at_unix_ms, metadata_json) VALUES ('workflow-bad-json', 'Bad', 1, 1, 1, '{')`)
	assertSQLiteConstraint(t, store.db, `INSERT INTO task_runs (id, task_id, placement_id, node_id, workflow_revision_seen, final_answer_violation_count, invalid_completion_count, created_at_unix_ms, updated_at_unix_ms) VALUES ('run-bad-counter', 'task-1', 'placement-start', 'node-start', 1, -1, 0, 1, 1)`)
	assertSQLiteConstraint(t, store.db, `INSERT INTO task_comments (id, task_id, body, author_kind, created_at_unix_ms, updated_at_unix_ms) VALUES ('comment-too-large', 'task-1', ?, 'agent', 1, 1)`, strings.Repeat("a", 262145))
}

func TestTaskSchemaAllowsEmptyBodyAndProjectScopedSourceWorkspace(t *testing.T) {
	store, _, binding := newMetadataTestStore(t)
	ctx := t.Context()
	other, err := store.CreateProjectForWorkspace(ctx, t.TempDir(), "Other Project")
	if err != nil {
		t.Fatalf("CreateProjectForWorkspace: %v", err)
	}
	source, err := store.AttachWorkspaceToProject(ctx, binding.ProjectID, t.TempDir())
	if err != nil {
		t.Fatalf("AttachWorkspaceToProject source: %v", err)
	}
	now := time.Now().UTC().UnixMilli()
	seedWorkflowGraph(t, store.db, binding.ProjectID, now)
	seedWorkflowGraphForProject(t, store.db, other.ProjectID, now, "2")

	if _, err := store.db.Exec(`INSERT INTO tasks (id, project_id, project_workflow_link_id, workflow_id, workflow_revision_seen, task_seq, short_id, title, body, source_workspace_id, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('task-empty-body', ?, 'link-1', 'workflow-1', 1, 1, 'BLD-1', 'Task', '', ?, ?, ?, '{}')`, binding.ProjectID, source.WorkspaceID, now, now); err != nil {
		t.Fatalf("empty task body with source workspace should be allowed: %v", err)
	}
	assertSQLiteConstraint(t, store.db, `INSERT INTO tasks (id, project_id, project_workflow_link_id, workflow_id, workflow_revision_seen, task_seq, short_id, title, body, source_workspace_id, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('task-foreign-workspace', ?, 'link-2', 'workflow-2', 1, 1, 'OTH-1', 'Task', '', ?, ?, ?, '{}')`, other.ProjectID, source.WorkspaceID, now, now)
}

func TestTaskShortIDUniquenessIsProjectScoped(t *testing.T) {
	store, _, binding := newMetadataTestStore(t)
	ctx := t.Context()
	other, err := store.CreateProjectForWorkspace(ctx, t.TempDir(), "Other Project")
	if err != nil {
		t.Fatalf("CreateProjectForWorkspace: %v", err)
	}
	now := time.Now().UTC().UnixMilli()
	seedWorkflowGraph(t, store.db, binding.ProjectID, now)
	seedWorkflowGraphForProject(t, store.db, other.ProjectID, now, "2")
	seedWorkflowTask(t, store, binding.ProjectID, "BLD-1")
	assertSQLiteConstraint(t, store.db, `INSERT INTO tasks (id, project_id, project_workflow_link_id, workflow_id, workflow_revision_seen, task_seq, short_id, title, body, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('task-dup', ?, 'link-1', 'workflow-1', 1, 2, 'BLD-1', 'Task', 'Body', ?, ?, '{}')`, binding.ProjectID, now, now)
	if _, err := store.db.Exec(`INSERT INTO tasks (id, project_id, project_workflow_link_id, workflow_id, workflow_revision_seen, task_seq, short_id, title, body, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('task-other', ?, 'link-2', 'workflow-2', 1, 1, 'BLD-1', 'Task', 'Body', ?, ?, '{}')`, other.ProjectID, now, now); err != nil {
		t.Fatalf("same short id in another project should be allowed: %v", err)
	}
}

func TestTaskSequenceAllocationIsAtomic(t *testing.T) {
	store, _, binding := newMetadataTestStore(t)
	ctx := t.Context()
	if err := store.SetProjectKey(ctx, binding.ProjectID, "BLD"); err != nil {
		t.Fatalf("SetProjectKey: %v", err)
	}

	const workers = 12
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	seqs := make(chan int64, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key, seq, err := store.AllocateProjectTaskSequence(ctx, binding.ProjectID)
			if err != nil {
				errs <- err
				return
			}
			if key != "BLD" {
				errs <- fmt.Errorf("project key = %q, want BLD", key)
				return
			}
			seqs <- seq
		}()
	}
	wg.Wait()
	close(errs)
	close(seqs)
	for err := range errs {
		t.Fatalf("AllocateProjectTaskSequence: %v", err)
	}
	seen := map[int64]bool{}
	for seq := range seqs {
		if seen[seq] {
			t.Fatalf("duplicate sequence %d", seq)
		}
		seen[seq] = true
	}
	for seq := int64(1); seq <= workers; seq++ {
		if !seen[seq] {
			t.Fatalf("missing sequence %d in %+v", seq, seen)
		}
	}
}

func TestCircularTransitionPlacementReferencesUseNullableDomainValidatedPath(t *testing.T) {
	store, _, binding := newMetadataTestStore(t)
	now := time.Now().UTC().UnixMilli()
	seedWorkflowGraph(t, store.db, binding.ProjectID, now)
	seedWorkflowTask(t, store, binding.ProjectID, "BLD-1")

	if _, err := store.db.Exec(`INSERT INTO task_transitions (id, task_id, source_placement_id, source_node_id, transition_group_id, transition_id, workflow_revision_seen, actor, state, output_values_json, created_at_unix_ms)
VALUES ('transition-1', 'task-1', 'placement-start', 'node-start', 'group-start', 'start', 1, 'system', 'applied', '{}', ?)`, now); err != nil {
		t.Fatalf("insert transition referencing existing placement: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO task_node_placements (id, task_id, node_id, state, created_by_transition_id, created_at_unix_ms, updated_at_unix_ms)
VALUES ('placement-agent', 'task-1', 'node-agent', 'active', 'transition-1', ?, ?)`, now, now); err != nil {
		t.Fatalf("insert placement referencing transition: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE task_transitions SET applied_at_unix_ms = ? WHERE id = 'transition-1'`, now); err != nil {
		t.Fatalf("update transition after circular insert path: %v", err)
	}
}

func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		t.Fatalf("query table %s: %v", table, err)
	}
	return name == table
}

func columnExists(t *testing.T, db *sql.DB, table string, column string) bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("table_info %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan table_info %s: %v", table, err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table_info %s: %v", table, err)
	}
	return false
}

func projectKeysByID(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT id, project_key FROM projects ORDER BY id`)
	if err != nil {
		t.Fatalf("query project keys: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var id string
		var key string
		if err := rows.Scan(&id, &key); err != nil {
			t.Fatalf("scan project key: %v", err)
		}
		out[id] = key
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate project keys: %v", err)
	}
	return out
}

func assertSQLiteConstraint(t *testing.T, db *sql.DB, statement string, args ...any) {
	t.Helper()
	_, err := db.Exec(statement, args...)
	if err == nil {
		t.Fatalf("expected SQLite constraint failure for %s", statement)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "constraint") {
		t.Fatalf("expected constraint failure, got %v", err)
	}
}

func seedWorkflowGraph(t *testing.T, db *sql.DB, projectID string, now int64) {
	t.Helper()
	seedWorkflowGraphForProject(t, db, projectID, now, "1")
}

func seedWorkflowGraphForProject(t *testing.T, db *sql.DB, projectID string, now int64, suffix string) {
	t.Helper()
	workflowID := "workflow-" + suffix
	startID := "node-start-" + suffix
	agentID := "node-agent-" + suffix
	doneID := "node-done-" + suffix
	startGroupID := "group-start-" + suffix
	doneGroupID := "group-done-" + suffix
	if suffix == "1" {
		startID = "node-start"
		agentID = "node-agent"
		doneID = "node-done"
		startGroupID = "group-start"
		doneGroupID = "group-done"
	}
	execSeed(t, db, "workflow", `INSERT INTO workflows (id, name, description, graph_revision, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES (?, 'Workflow', '', 1, ?, ?, '{}')`, workflowID, now, now)
	execSeed(t, db, "nodes", `INSERT INTO workflow_nodes (id, workflow_id, node_key, kind, display_name, output_fields_json, metadata_json)
VALUES (?, ?, 'backlog', 'start', 'Backlog', '[]', '{}'),
       (?, ?, 'agent', 'agent', 'Agent', '[{"name":"summary","description":"Summary."}]', '{}'),
       (?, ?, 'done', 'terminal', 'Done', '[]', '{}')`, startID, workflowID, agentID, workflowID, doneID, workflowID)
	execSeed(t, db, "transition groups", `INSERT INTO workflow_transition_groups (id, workflow_id, source_node_id, transition_id, display_name, metadata_json)
VALUES (?, ?, ?, 'start', 'Start', '{}'),
       (?, ?, ?, 'done', 'Done', '{}')`, startGroupID, workflowID, startID, doneGroupID, workflowID, agentID)
	execSeed(t, db, "edges", `INSERT INTO workflow_edges (id, workflow_id, transition_group_id, edge_key, target_node_id, context_mode, input_bindings_json, output_requirements_json, metadata_json)
VALUES (?, ?, ?, 'start', ?, 'new_session', '{}', '{}', '{}'),
       (?, ?, ?, 'done', ?, 'new_session', '{}', '{"fields":["summary"]}', '{}')`, "edge-start-"+suffix, workflowID, startGroupID, agentID, "edge-done-"+suffix, workflowID, doneGroupID, doneID)
	execSeed(t, db, "project workflow link", `INSERT INTO project_workflow_links (id, project_id, workflow_id, is_default, created_at_unix_ms, updated_at_unix_ms)
VALUES (?, ?, ?, 1, ?, ?)`, "link-"+suffix, projectID, workflowID, now, now)
}

func execSeed(t *testing.T, db *sql.DB, label string, statement string, args ...any) {
	t.Helper()
	if _, err := db.Exec(statement, args...); err != nil {
		t.Fatalf("seed %s: %v", label, err)
	}
}

func seedWorkflowTask(t *testing.T, store *Store, projectID string, shortID string) {
	t.Helper()
	db := store.db
	now := time.Now().UTC().UnixMilli()
	if _, err := db.Exec(`
INSERT INTO tasks (id, project_id, project_workflow_link_id, workflow_id, workflow_revision_seen, task_seq, short_id, title, body, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('task-1', ?, 'link-1', 'workflow-1', 1, 1, ?, 'Task', 'Body', ?, ?, '{}');
INSERT INTO task_node_placements (id, task_id, node_id, state, created_at_unix_ms, updated_at_unix_ms)
VALUES ('placement-start', 'task-1', 'node-start', 'active', ?, ?);
`, projectID, shortID, now, now, now, now); err != nil {
		t.Fatalf("seed workflow task: %v", err)
	}
}
