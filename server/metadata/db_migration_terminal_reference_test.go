package metadata

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"core/server/metadata/sqlitegen"
)

func TestTerminalReferenceCleanupPreservesUnfinishedReferencesAndValues(t *testing.T) {
	root := t.TempDir()
	db, err := openDatabaseAtVersionForTest(t, root, filepath.Join(root, "db", "main.sqlite3"), 90)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	execSeed(t, db, "project", `INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('project-terminal', 'Project', ?, ?, '{}')`, now, now)
	seedWorkflowGraph(t, db, "project-terminal", now)
	seedSessionCommandAuthorityLegacyWorkflowSession(t, db, "project-terminal", "workspace-terminal", "session-terminal", now)
	for i, fixture := range []struct {
		task, node, edge string
	}{
		{"task-done", "node-done", "edge-done-1"},
		{"task-active", "node-agent", "edge-start-1"},
	} {
		execSeed(t, db, "task", workflowSeedTaskSQL, fixture.task, "link-1", i+1, fixture.task, now, now)
		execSeed(t, db, "current node", `INSERT INTO task_current_nodes
(task_id, node_id, entered_by_edge_id, current_input_values_json, prior_node_values_json)
VALUES (?, ?, ?, '{"result":"kept"}', '{"transition_parameters":{"done":{"result":"kept"}}}')`,
			fixture.task, workflowGraphSeedID(t, db, fixture.node), workflowGraphSeedID(t, db, fixture.edge))
	}
	execSeed(t, db, "retained Session owner", `UPDATE sessions SET task_id = 'task-done' WHERE id = 'session-terminal'`)
	execSeed(t, db, "retained Session association", `INSERT INTO session_workflow_node_associations
(session_id, node_id, associated_at_unix_ms) VALUES ('session-terminal', ?, ?)`,
		workflowGraphSeedID(t, db, "node-agent"), now)
	execSeed(t, db, "retained comment", `INSERT INTO task_comments
(id, task_id, body, author_kind, author_id, created_at_unix_ms, updated_at_unix_ms)
VALUES ('comment-terminal', 'task-done', 'retained comment', 'user', 'operator', ?, ?)`, now, now)
	execSeed(t, db, "pending approval", `INSERT INTO task_pending_approvals
(id, source_task_id, source_node_id, workflow_version, transition_snapshot_json, materialized_values_json, created_at_unix_ms)
VALUES ('approval-terminal', 'task-active', ?, 1, '{}', '{}', ?)`,
		workflowGraphSeedID(t, db, "node-agent"), now)
	execSeed(t, db, "pending terminal target", `INSERT INTO task_pending_approval_branches
(approval_id, transition_branch_key, target_snapshot_json, effective_edge_configuration_json, context_source_resolution_json)
VALUES ('approval-terminal', 'done', json_object('entered_by_edge_id', ?, 'node_id', ?,
'prior_values', json('{"transition_parameters":{}}')), '{}', '{}')`,
		workflowGraphSeedID(t, db, "edge-done-1"), workflowGraphSeedID(t, db, "node-done"))
	var pendingBefore string
	if err := db.QueryRow(`SELECT target_snapshot_json FROM task_pending_approval_branches`).Scan(&pendingBefore); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var pendingAfter string
	if err := store.db.QueryRow(`SELECT target_snapshot_json FROM task_pending_approval_branches`).Scan(&pendingAfter); err != nil {
		t.Fatal(err)
	}
	if pendingAfter != pendingBefore {
		t.Fatalf("pending target changed: %s -> %s", pendingBefore, pendingAfter)
	}
	var title, body, comment, sessionOwner string
	if err := store.db.QueryRow(`SELECT title, body FROM tasks WHERE id = 'task-done'`).Scan(&title, &body); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT body FROM task_comments WHERE id = 'comment-terminal'`).Scan(&comment); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT task_id FROM sessions
JOIN session_workflow_node_associations ON session_id = sessions.id WHERE sessions.id = 'session-terminal'`).Scan(&sessionOwner); err != nil {
		t.Fatal(err)
	}
	if title != "Task" || body != "Body" || comment != "retained comment" || sessionOwner != "task-done" {
		t.Fatalf("completed content changed: %q %q %q %q", title, body, comment, sessionOwner)
	}
	for _, task := range []string{"task-done", "task-active"} {
		var edge sql.NullString
		var input, prior string
		if err := store.db.QueryRow(`SELECT entered_by_edge_id, current_input_values_json, prior_node_values_json
FROM task_current_nodes WHERE task_id = ?`, task).Scan(&edge, &input, &prior); err != nil {
			t.Fatal(err)
		}
		if edge.Valid != (task == "task-active") {
			t.Fatalf("%s incoming reference = %v", task, edge)
		}
		if input != `{"result":"kept"}` || prior != `{"transition_parameters":{"done":{"result":"kept"}}}` {
			t.Fatalf("%s lost stored values: %s / %s", task, input, prior)
		}
	}
	queries := sqlitegen.New(store.db)
	// The shared Branch remains protected by the frozen Approval, not the
	// completed Task. Once it is resolved, both graph-edit guards see no reference.
	edgeID := workflowGraphSeedID(t, store.db, "edge-done-1").(string)
	if count, err := queries.CountTaskEdgeReferences(t.Context(), edgeID); err != nil || count != 1 {
		t.Fatalf("pending removal references = %d: %v", count, err)
	}
	execSeed(t, store.db, "resolve pending approval", `DELETE FROM task_pending_approvals WHERE id = 'approval-terminal'`)
	if count, err := queries.CountTaskEdgeReferences(t.Context(), edgeID); err != nil || count != 0 {
		t.Fatalf("completed removal references = %d: %v", count, err)
	}
	if count, err := queries.CountAllTaskEdgeReferences(t.Context(), edgeID); err != nil || count != 0 {
		t.Fatalf("completed retargeting references = %d: %v", count, err)
	}
}
