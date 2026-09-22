package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/metadata"
	"core/server/workflow"
	"core/shared/runtimeids"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

func TestMigratedSerialApprovalWithMissingEnteringEdgeRequiresMove(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "db", "main.sqlite3")
	db := openLegacyCurrentStateMigrationDatabase(t, root, databasePath)
	now := time.Now().UTC().UnixMilli()
	const sourceSessionID = "550e8400-e29b-41d4-a716-446655440091"
	execLegacyMigrationSeed(t, db, "project", `
INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('project-migrated-approval-fanout', 'Project', ?, ?, '{}')`, now, now)
	execLegacyMigrationSeed(t, db, "workspace", `
INSERT INTO workspaces (
    id, project_id, canonical_root_path, git_metadata_json, created_at_unix_ms, updated_at_unix_ms
) VALUES (
    'workspace-migrated-approval-fanout',
    'project-migrated-approval-fanout',
    '/workspace-migrated-approval-fanout',
    '{}',
    ?,
    ?
)`, now, now)
	execLegacyMigrationSeed(t, db, "default workspace", `
UPDATE projects SET primary_workspace_id = 'workspace-migrated-approval-fanout'
WHERE id = 'project-migrated-approval-fanout'`)
	execLegacyMigrationSeed(t, db, "session", `
INSERT INTO sessions (
    id, project_id, workspace_id, artifact_relpath, created_at_unix_ms, updated_at_unix_ms
) VALUES (
    ?,
    'project-migrated-approval-fanout',
    'workspace-migrated-approval-fanout',
    ?,
    ?,
    ?
)`, sourceSessionID, "sessions/"+sourceSessionID, now, now)
	execLegacyMigrationSeed(t, db, "workflow", `
INSERT INTO workflows (id, name, description, version, created_at_unix_ms, updated_at_unix_ms)
VALUES ('workflow-11111111-1111-4111-8111-111111111111', 'Workflow', '', 1, ?, ?)`, now, now)
	execLegacyMigrationSeed(t, db, "workflow nodes", `
INSERT INTO workflow_nodes (
    id, workflow_id, node_key, kind, display_name, subagent_role, output_fields_json
) VALUES
    ('node-start', 'workflow-11111111-1111-4111-8111-111111111111', 'backlog', 'start', 'Backlog', '', '[]'),
    ('node-source', 'workflow-11111111-1111-4111-8111-111111111111', 'source', 'agent', 'Source', 'coder', '[]'),
    ('node-target-a', 'workflow-11111111-1111-4111-8111-111111111111', 'target_a', 'agent', 'Target A', 'coder', '[]'),
    ('node-target-b', 'workflow-11111111-1111-4111-8111-111111111111', 'target_b', 'agent', 'Target B', 'coder', '[]'),
    ('node-done', 'workflow-11111111-1111-4111-8111-111111111111', 'done', 'terminal', 'Done', '', '[]')`, now, now)
	execLegacyMigrationSeed(t, db, "workflow transition groups", `
INSERT INTO workflow_transition_groups (id, source_node_id, transition_id, display_name)
VALUES
    ('group-start', 'node-start', 'start', 'Start'),
    ('group-fanout', 'node-source', 'approve', 'Approve')`)
	execLegacyMigrationSeed(t, db, "workflow edges", `
INSERT INTO workflow_edges (
    id, transition_group_id, edge_key, target_node_id, requires_approval,
    context_mode, input_bindings_json, output_requirements_json
) VALUES
    ('edge-start', 'group-start', 'start', 'node-source', 0, 'new_session', '[]', '[]'),
    ('edge-fanout-a', 'group-fanout', 'split_a', 'node-target-a', 1, 'new_session', '[]', '[]'),
    ('edge-fanout-b', 'group-fanout', 'split_b', 'node-target-b', 1, 'continue_session', '[]', '[]')`)
	execLegacyMigrationSeed(t, db, "workflow link", `
INSERT INTO project_workflow_links (
    id, project_id, workflow_id, created_at_unix_ms, updated_at_unix_ms
) VALUES ('link-migrated-approval-fanout', 'project-migrated-approval-fanout', 'workflow-11111111-1111-4111-8111-111111111111', ?, ?)`, now, now)
	execLegacyMigrationSeed(t, db, "default workflow link", `
UPDATE projects
SET default_project_workflow_link_id = 'link-migrated-approval-fanout'
WHERE id = 'project-migrated-approval-fanout'`)
	execLegacyMigrationSeed(t, db, "task", `
INSERT INTO tasks (
    id, project_workflow_link_id, workflow_revision_seen, task_seq, short_id,
    title, body, created_at_unix_ms, updated_at_unix_ms, metadata_json
) VALUES (
    'task-migrated-approval-fanout',
    'link-migrated-approval-fanout',
    1,
    1,
    'MIG-1',
    'Task',
    'Body',
    ?,
    ?,
    '{}'
)`, now, now)
	execLegacyMigrationSeed(t, db, "approval source placement", `
INSERT INTO task_node_placements (
    id, task_id, node_id, state, created_at_unix_ms, updated_at_unix_ms
) VALUES (
    'placement-migrated-approval-fanout',
    'task-migrated-approval-fanout',
    'node-source',
    'waiting_approval',
    ?,
    ?
)`, now, now)
	execLegacyMigrationSeed(t, db, "approval source run", `
INSERT INTO task_runs (
    id, placement_id, session_id, workflow_revision_seen,
    created_at_unix_ms, updated_at_unix_ms, started_at_unix_ms, completed_at_unix_ms
) VALUES (
    'run-migrated-approval-fanout',
    'placement-migrated-approval-fanout',
    ?,
    1,
    ?,
    ?,
    ?,
    ?
)`, sourceSessionID, now, now+1, now, now+1)
	execLegacyMigrationSeed(t, db, "source entering transition", `
INSERT INTO task_transitions (
    id, task_id, source_node_key, source_node_display_name,
    transition_id, transition_display_name, workflow_revision_seen, actor, state,
    commentary, output_values_json, created_at_unix_ms, applied_at_unix_ms
) VALUES (
    'transition-migrated-start',
    'task-migrated-approval-fanout',
    'backlog',
    'Backlog',
    'start',
    'Start',
    1,
    'system',
    'applied',
    '',
    '{}',
    ?,
    ?
)`, now, now)
	execLegacyMigrationSeed(t, db, "source entering transition edge", `
INSERT INTO task_transition_edges (
    id, task_transition_id, workflow_edge_id, edge_key, target_placement_id,
    target_node_id, target_node_key, target_node_display_name, target_node_kind,
    target_placement_id, state, context_mode, requires_approval,
    input_bindings_json, output_requirements_json, metadata_json
) VALUES (
    'transition-edge-migrated-start',
    'transition-migrated-start',
    'edge-start',
    'start',
    'placement-migrated-approval-fanout',
    'node-source',
    'source',
    'Source',
    'agent',
    'placement-migrated-approval-fanout',
    'applied',
    'new_session',
    0,
    '[]',
    '[]',
    '{}'
)`)
	execLegacyMigrationSeed(t, db, "approval transition", `
INSERT INTO task_transitions (
    id, task_id, source_run_id, source_placement_id, source_node_key, source_node_display_name,
    transition_id, transition_display_name, workflow_revision_seen, actor, state,
    commentary, output_values_json, created_at_unix_ms
) VALUES (
    'transition-migrated-approval-fanout',
    'task-migrated-approval-fanout',
    'run-migrated-approval-fanout',
    'placement-migrated-approval-fanout',
    'source',
    'Source',
    'approve',
    'Approve',
    1,
    'agent',
    'pending_approval',
    '',
    '{}',
    ?
)`, now+1)
	for _, target := range []struct {
		id     string
		edgeID string
		key    string
		nodeID string
		name   string
		mode   string
	}{
		{"transition-edge-migrated-a", "edge-fanout-a", "split_a", "node-target-a", "Target A", "new_session"},
		{"transition-edge-migrated-b", "edge-fanout-b", "split_b", "node-target-b", "Target B", "continue_session"},
	} {
		execLegacyMigrationSeed(t, db, "approval transition edge", `
INSERT INTO task_transition_edges (
    id, task_transition_id, workflow_edge_id, edge_key,
    target_node_id, target_node_key, target_node_display_name, target_node_kind,
    state, context_mode, requires_approval, input_bindings_json, output_requirements_json, metadata_json
) VALUES (?, 'transition-migrated-approval-fanout', ?, ?, ?, ?, ?, 'agent', 'pending', ?, 1, '[]', '[]', json_object(
    'source_session_id', ?
))`,
			target.id,
			target.edgeID,
			target.key,
			target.nodeID,
			map[string]string{"split_a": "target_a", "split_b": "target_b"}[target.key],
			target.name,
			target.mode,
			sourceSessionID,
		)
	}
	execLegacyMigrationSeed(t, db, "delete mutable approval graph", `
DELETE FROM workflow_transition_groups
WHERE id = 'group-fanout'`)
	// The live graph has been edited into a valid serial chain, but the
	// Approval still names the deleted fan-out's incoming edges.
	execLegacyMigrationSeed(t, db, "replacement graph", `
INSERT INTO workflow_transition_groups (id,source_node_id,transition_id,display_name) VALUES
('group-replacement-source','node-source','next_source','Next'),
('group-replacement-a','node-target-a','next_a','Next'),
('group-replacement-b','node-target-b','done','Done');
INSERT INTO workflow_edges (id,transition_group_id,edge_key,target_node_id,requires_approval,context_mode,input_bindings_json,output_requirements_json) VALUES
('edge-replacement-source','group-replacement-source','next','node-target-a',0,'new_session','[]','[]'),
('edge-replacement-a','group-replacement-a','next','node-target-b',0,'new_session','[]','[]'),
('edge-replacement-b','group-replacement-b','done','node-done',0,'new_session','[]','[]');
UPDATE workflow_edges SET prompt_template = 'Continue the task' WHERE target_node_id != 'node-done'`)
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy migration database: %v", err)
	}

	metadataStore, err := metadata.Open(root)
	if err != nil {
		t.Fatalf("migrate legacy approval fanout database: %v", err)
	}
	t.Cleanup(func() { _ = metadataStore.Close() })
	if _, err := metadataStore.DB().ExecContext(t.Context(), `UPDATE tasks SET execution_target_mode = 'none', execution_target_provenance = 'resolved', pending_initial_managed_branch_name = NULL WHERE id = ?`, "task-migrated-approval-fanout"); err != nil {
		t.Fatal(err)
	}
	store, err := New(metadataStore, WithRoleResolver(testsetup.QuestionsEnabled("coder")))
	if err != nil {
		t.Fatalf("workflowstore.New: %v", err)
	}

	approvals, err := store.ListPendingApprovals(t.Context(), workflow.TaskID("task-migrated-approval-fanout"))
	if err != nil {
		t.Fatalf("ListPendingApprovals after migration: %v", err)
	}
	if len(approvals) != 1 || len(approvals[0].Branches) != 2 {
		t.Fatalf("migrated approvals = %+v, want one two-branch approval", approvals)
	}
	approval := approvals[0]
	if approval.SourceSessionID == nil || approval.SourceSessionID.String() != sourceSessionID {
		t.Fatalf("migrated approval source Session = %v, want %q", approval.SourceSessionID, sourceSessionID)
	}
	for _, branch := range approval.Branches {
		targetBranchKey, branchScoped := branch.Target.CurrentNode.Reference.TransitionBranchKey()
		if !branchScoped || targetBranchKey != branch.TransitionBranchKey {
			t.Fatalf("migrated approval branch = %+v, want target scoped to frozen branch key", branch)
		}
		switch branch.TransitionBranchKey {
		case "split_a":
			if branch.Target.CurrentNode.SessionID != nil ||
				branch.ContextSourceResolution.TargetSession.Kind() != workflow.TargetSessionIntentCreate ||
				branch.ContextSourceResolution.ActiveSource.Kind() != workflow.MaterializedContinuationSourceDeferredSelf {
				t.Fatalf("migrated new-session branch = %+v, want no retained Session", branch)
			}
		case "split_b":
			resolvedSessionID, reused := branch.ContextSourceResolution.TargetSession.SessionID()
			if branch.Target.CurrentNode.SessionID == nil ||
				branch.Target.CurrentNode.SessionID.String() != sourceSessionID ||
				!reused ||
				resolvedSessionID.String() != sourceSessionID ||
				branch.ContextSourceResolution.ActiveSource.Kind() != workflow.MaterializedContinuationSourceExact {
				t.Fatalf("migrated continuation branch = %+v, want retained Session %q with exact frozen source", branch, sourceSessionID)
			}
		default:
			t.Fatalf("migrated unexpected approval branch = %+v", branch)
		}
	}

	_, err = store.PlanPendingApproval(t.Context(), approval.ID)
	var restriction *TaskContextSelectionRequiredError
	if !errors.As(err, &restriction) || restriction.TaskID != approval.Source.TaskID {
		t.Fatalf("Approval restriction = %v", err)
	}
	_, err = store.PreflightTaskResume(t.Context(), approval.Source.TaskID)
	if !errors.As(err, &restriction) {
		t.Fatalf("Resume restriction = %v", err)
	}
	retained, err := store.PendingApproval(t.Context(), approval.ID)
	if err != nil || !reflect.DeepEqual(retained, approval) {
		t.Fatalf("restricted Approval content lost: %+v, %v", retained, err)
	}
	noOp, err := store.PrepareManualMove(t.Context(), ManualMoveRequest{TaskID: approval.Source.TaskID, TargetNodeID: approval.Source.NodeID})
	if err != nil {
		t.Fatal(err)
	}
	noOpPlan, err := store.PlanManualMove(t.Context(), noOp, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitManualMove(t.Context(), noOpPlan, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PreflightTaskResume(t.Context(), approval.Source.TaskID); !errors.As(err, &restriction) {
		t.Fatalf("no-op Move cleared context restriction: %v", err)
	}
	task, err := store.GetTaskExecutionTargetContext(t.Context(), approval.Source.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	definition, _, err := store.GetDefinition(t.Context(), task.Task.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	var destination workflow.NodeID
	for _, node := range definition.Nodes {
		if node.Kind() == workflow.NodeKindTerminal {
			destination = workflow.NodeIDOf(node)
		}
	}
	prepared, err := store.PrepareManualMove(t.Context(), ManualMoveRequest{TaskID: approval.Source.TaskID, TargetNodeID: destination})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := store.PlanManualMove(t.Context(), prepared, nil)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := store.CommitManualMove(t.Context(), plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(moved.Mutation.Created) != 1 || moved.Mutation.Created[0].Reference.NodeID != destination {
		t.Fatalf("Move did not replace restricted positions: %+v", moved)
	}
	remaining, err := store.ListPendingApprovals(t.Context(), approval.Source.TaskID)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("Move left Approval: %+v, %v", remaining, err)
	}
	var sessions int
	if err := metadataStore.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM sessions WHERE id = ?`, sourceSessionID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 {
		t.Fatal("Move deleted source chat")
	}
}

func TestResolveCurrentSessionStartContextUsesMigratedDirectOwnership(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "db", "main.sqlite3")
	db := openLegacyCurrentStateMigrationDatabase(t, root, databasePath)
	now := time.Now().UTC().UnixMilli()
	const (
		projectID  = "project-migrated-session-context"
		workflowID = "workflow-22222222-2222-4222-8222-222222222222"
		taskID     = "task-migrated-session-context"
		sessionID  = "550e8400-e29b-41d4-a716-446655440090"
		runID      = "run-migrated-session-context"
	)
	snapshotJSON := `{"workflow_id":"workflow-22222222-2222-4222-8222-222222222222","workflow_revision_seen":1,"node":{"id":"node-agent","key":"agent","display_name":"Agent","kind":"agent","subagent_role":"coder"}}`
	execLegacyMigrationSeed(t, db, "project", `
INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES (?, 'Project', ?, ?, '{}')`, projectID, now, now)
	execLegacyMigrationSeed(t, db, "workspace", `
INSERT INTO workspaces (
    id, project_id, canonical_root_path, git_metadata_json, created_at_unix_ms, updated_at_unix_ms
) VALUES ('workspace-migrated-session-context', ?, '/workspace-migrated-session-context', '{}', ?, ?)`,
		projectID,
		now,
		now,
	)
	execLegacyMigrationSeed(t, db, "session", `
INSERT INTO sessions (
    id, project_id, workspace_id, artifact_relpath, created_at_unix_ms, updated_at_unix_ms, metadata_json
) VALUES (?, ?, 'workspace-migrated-session-context', ?, ?, ?, json_object(
    'workflow_session',
    json_object(
        'run_id', 'stale-run-id',
        'task_id', 'stale-task-id',
        'workflow_id', 'stale-workflow-id'
    )
))`,
		sessionID,
		projectID,
		"sessions/"+sessionID,
		now,
		now,
	)
	execLegacyMigrationSeed(t, db, "workflow", `
INSERT INTO workflows (id, name, description, version, created_at_unix_ms, updated_at_unix_ms)
VALUES (?, 'Workflow', '', 1, ?, ?)`, workflowID, now, now)
	execLegacyMigrationSeed(t, db, "workflow nodes", `
INSERT INTO workflow_nodes (
    id, workflow_id, node_key, kind, display_name, subagent_role, output_fields_json
) VALUES
    ('node-start', ?, 'backlog', 'start', 'Backlog', '', '[]'),
    ('node-agent', ?, 'agent', 'agent', 'Agent', 'coder', '[]'),
    ('node-done', ?, 'done', 'terminal', 'Done', '', '[]')`,
		workflowID,
		workflowID,
		workflowID,
	)
	execLegacyMigrationSeed(t, db, "workflow transition group", `
INSERT INTO workflow_transition_groups (id, source_node_id, transition_id, display_name)
VALUES ('group-start', 'node-start', 'start', 'Start')`)
	execLegacyMigrationSeed(t, db, "workflow start edge", `
INSERT INTO workflow_edges (
    id, transition_group_id, edge_key, target_node_id, requires_approval,
    context_mode, input_bindings_json, output_requirements_json
) VALUES ('edge-start', 'group-start', 'start', 'node-agent', 0, 'new_session', '[]', '[]')`)
	execLegacyMigrationSeed(t, db, "workflow link", `
INSERT INTO project_workflow_links (
    id, project_id, workflow_id, created_at_unix_ms, updated_at_unix_ms
) VALUES ('link-migrated-session-context', ?, ?, ?, ?)`, projectID, workflowID, now, now)
	execLegacyMigrationSeed(t, db, "default workflow link", `
UPDATE projects
SET default_project_workflow_link_id = 'link-migrated-session-context'
WHERE id = ?`, projectID)
	execLegacyMigrationSeed(t, db, "task", `
INSERT INTO tasks (
    id, project_workflow_link_id, workflow_revision_seen, task_seq, short_id,
    title, body, created_at_unix_ms, updated_at_unix_ms, metadata_json
) VALUES (?, 'link-migrated-session-context', 1, 1, 'MIG-2', 'Task', 'Body', ?, ?, '{}')`,
		taskID,
		now,
		now,
	)
	execLegacyMigrationSeed(t, db, "agent placement", `
INSERT INTO task_node_placements (
    id, task_id, node_id, state, created_at_unix_ms, updated_at_unix_ms
) VALUES ('placement-migrated-session-context', ?, 'node-agent', 'active', ?, ?)`,
		taskID,
		now,
		now+1,
	)
	execLegacyMigrationSeed(t, db, "start transition", `
INSERT INTO task_transitions (
    id, task_id, source_placement_id, source_node_key, source_node_display_name,
    transition_id, transition_display_name, workflow_revision_seen, actor, state,
    commentary, output_values_json, created_at_unix_ms
) VALUES (
    'transition-migrated-session-context', ?, NULL, 'backlog', 'Backlog',
    'start', 'Start', 1, 'user', 'applied', '', '{}', ?
)`, taskID, now)
	execLegacyMigrationSeed(t, db, "start transition edge", `
INSERT INTO task_transition_edges (
    id, task_transition_id, workflow_edge_id, edge_key, target_placement_id,
    target_node_id, target_node_key, target_node_display_name, target_node_kind,
    state, context_mode, requires_approval, input_bindings_json, output_requirements_json, metadata_json
) VALUES (
    'transition-edge-migrated-session-context',
    'transition-migrated-session-context',
    'edge-start',
    'start',
    'placement-migrated-session-context',
    'node-agent',
    'agent',
    'Agent',
    'agent',
    'applied',
    'new_session',
    0,
    '[]',
    '[]',
    '{}'
)`)
	execLegacyMigrationSeed(t, db, "agent run", `
INSERT INTO task_runs (
    id, placement_id, session_id, workflow_revision_seen,
    created_at_unix_ms, updated_at_unix_ms, started_at_unix_ms,
    run_start_snapshot_json
) VALUES (?, 'placement-migrated-session-context', ?, 1, ?, ?, ?, ?)`,
		runID,
		sessionID,
		now,
		now+2,
		now+1,
		snapshotJSON,
	)
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy migration database: %v", err)
	}

	metadataStore, err := metadata.Open(root)
	if err != nil {
		t.Fatalf("migrate legacy session context database: %v", err)
	}
	t.Cleanup(func() { _ = metadataStore.Close() })
	store, err := New(metadataStore)
	if err != nil {
		t.Fatalf("workflowstore.New: %v", err)
	}
	parsedSessionID, err := runtimeids.ParseSessionID(sessionID)
	if err != nil {
		t.Fatalf("parse session id: %v", err)
	}
	workflowOwned, err := metadataStore.SessionHasWorkflowTask(t.Context(), sessionID)
	if err != nil {
		t.Fatalf("SessionHasWorkflowTask: %v", err)
	}
	if !workflowOwned {
		t.Fatal("migrated session direct task ownership was not retained")
	}
	input, err := store.ResolveCurrentSessionStartContext(t.Context(), parsedSessionID)
	if err != nil {
		t.Fatalf("ResolveCurrentSessionStartContext: %v", err)
	}
	definition, _, err := store.GetDefinition(t.Context(), input.Workflow.ID)
	if err != nil {
		t.Fatalf("GetDefinition migrated Workflow: %v", err)
	}
	startEdgeID := edgeByKey(t, definition, "start").ID
	if _, err := runtimeids.GraphEntityIDBlob(string(input.Node.ID)); err != nil {
		t.Fatalf("migrated context Node ID %q: %v", input.Node.ID, err)
	}
	if _, err := runtimeids.GraphEntityIDBlob(string(startEdgeID)); err != nil {
		t.Fatalf("migrated context entering Edge ID %q: %v", startEdgeID, err)
	}
	if input.Task.ID != workflow.TaskID(taskID) ||
		input.Node.Key != "agent" ||
		input.CurrentNode.EnteredByEdgeID == nil ||
		*input.CurrentNode.EnteredByEdgeID != startEdgeID {
		t.Fatalf("resolved migrated context = %+v, want direct current ownership and entering edge", input)
	}
	association, err := store.LatestTaskSessionForNode(t.Context(), input.CurrentNode.Reference)
	if err != nil {
		t.Fatalf("LatestTaskSessionForNode: %v", err)
	}
	if association.SessionID != parsedSessionID ||
		!association.CurrentNode.Equal(input.CurrentNode.Reference) {
		t.Fatalf("migrated startup association = %+v, want Session %q and Current Node %v", association, parsedSessionID, input.CurrentNode.Reference)
	}
}

func openLegacyCurrentStateMigrationDatabase(t *testing.T, root string, databasePath string) *sql.DB {
	t.Helper()
	bootstrap, err := metadata.Open(t.TempDir())
	if err != nil {
		t.Fatalf("initialize metadata SQLite extensions: %v", err)
	}
	if err := bootstrap.Close(); err != nil {
		t.Fatalf("close metadata SQLite extension bootstrap: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o755); err != nil {
		t.Fatalf("create migration database directory: %v", err)
	}
	db, err := sql.Open("sqlite", "file:"+databasePath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open legacy migration database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	migrations, err := legacyCurrentStateMigrationFiles()
	if err != nil {
		t.Fatalf("read metadata migrations: %v", err)
	}
	provider, err := goose.NewProvider(
		goose.DialectSQLite3,
		db,
		migrations,
		goose.WithLogger(goose.NopLogger()),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		t.Fatalf("create migration provider: %v", err)
	}
	if _, err := provider.UpTo(context.Background(), 58); err != nil {
		t.Fatalf("apply migrations through version 58: %v", err)
	}
	return db
}

func legacyCurrentStateMigrationFiles() (fs.FS, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return nil, os.ErrNotExist
	}
	migrationFiles := os.DirFS(filepath.Join(filepath.Dir(file), "..", "metadata", "migrations"))
	entries, err := fs.ReadDir(migrationFiles, ".")
	if err != nil {
		return nil, err
	}
	upMigrations := make(fstest.MapFS)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		contents, err := fs.ReadFile(migrationFiles, entry.Name())
		if err != nil {
			return nil, err
		}
		upMigrations[entry.Name()] = &fstest.MapFile{Data: contents}
	}
	return upMigrations, nil
}

func execLegacyMigrationSeed(t *testing.T, db *sql.DB, label string, statement string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), statement, args...); err != nil {
		t.Fatalf("seed %s: %v", label, err)
	}
}
