package metadata

import (
	"reflect"
	"testing"
)

func TestJoinProviderCollectionMigrationPreservesOrderAndIdentity(t *testing.T) {
	db, err := openDatabaseAtVersionForTest(t, 103)
	if err != nil {
		t.Fatal(err)
	}
	execSeed(t, db, "project", `INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms) VALUES ('collections-project', 'Project', 1000, 1000)`)
	seedWorkflowGraph(t, db, "collections-project", 1000)
	nodeID := workflowGraphSeedID(t, db, "node-agent")
	edgeID := workflowGraphSeedIDText(t, db, "edge-start-1")
	execSeed(t, db, "ordered providers", `UPDATE workflow_nodes SET join_input_providers_json = json_array(json_object('input_name','second','provider_edge_id',?),json_object('input_name','first','provider_edge_id',?)) WHERE id = ?`, edgeID, edgeID, nodeID)
	identities := optionalNodeLabelMigrationRows(t, db, `SELECT id, workflow_id, node_key FROM workflow_nodes ORDER BY rowid`)
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}
	got := optionalNodeLabelMigrationRows(t, db, `SELECT input_name, provider_edge_id, sort_order FROM workflow_join_providers ORDER BY sort_order`)
	want := [][]any{{"second", workflowGraphSeedID(t, db, "edge-start-1"), int64(0)}, {"first", workflowGraphSeedID(t, db, "edge-start-1"), int64(1)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("providers: got %v, want %v", got, want)
	}
	if after := optionalNodeLabelMigrationRows(t, db, `SELECT id, workflow_id, node_key FROM workflow_nodes ORDER BY rowid`); !reflect.DeepEqual(identities, after) {
		t.Fatalf("node identities changed: %v -> %v", identities, after)
	}
	if violations := optionalNodeLabelMigrationRows(t, db, `PRAGMA foreign_key_check`); len(violations) != 0 {
		t.Fatalf("foreign key violations: %v", violations)
	}
	execSeed(t, db, "unrelated provider edge", `INSERT INTO workflow_join_providers VALUES (?, 2, 'unrelated', ?)`, nodeID, workflowGraphSeedID(t, db, "edge-done-1"))
	execSeed(t, db, "delete provider edge", `DELETE FROM workflow_edges WHERE id = ?`, workflowGraphSeedID(t, db, "edge-start-1"))
	remaining := optionalNodeLabelMigrationRows(t, db, `SELECT input_name, sort_order FROM workflow_join_providers`)
	if want := [][]any{{"unrelated", int64(2)}}; !reflect.DeepEqual(remaining, want) {
		t.Fatalf("provider edge cascade removed wrong providers: got %v, want %v", remaining, want)
	}
}

func TestParameterCollectionMigrationPreservesProtectedSettingsAndOrder(t *testing.T) {
	db, err := openDatabaseAtVersionForTest(t, 104)
	if err != nil {
		t.Fatal(err)
	}
	execSeed(t, db, "project", `INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms) VALUES ('parameters-project', 'Project', 1000, 1000)`)
	seedWorkflowGraph(t, db, "parameters-project", 1000)
	edgeID := workflowGraphSeedID(t, db, "edge-start-1")
	execSeed(t, db, "protected parameters", `UPDATE workflow_edges SET parameters_json = '[{"key":"effort","description":"Dormant thinking","purpose":"target_thinking"},{"key":"summary","description":" Summary. ","purpose":"ordinary"},{"key":"role","description":"Dormant assignee","purpose":"target_assignee"}]' WHERE id = ?`, edgeID)
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}
	got := optionalNodeLabelMigrationRows(t, db, `SELECT parameter_key, description, purpose, sort_order FROM workflow_parameters ORDER BY edge_id, sort_order`)
	want := [][]any{{"effort", "Dormant thinking", "target_thinking", int64(0)}, {"summary", " Summary. ", "ordinary", int64(1)}, {"role", "Dormant assignee", "target_assignee", int64(2)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parameters: got %v, want %v", got, want)
	}
}

func TestInputBindingCollectionMigrationPreservesReferencesAndEmptyValues(t *testing.T) {
	db, err := openDatabaseAtVersionForTest(t, 105)
	if err != nil {
		t.Fatal(err)
	}
	execSeed(t, db, "project", `INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms) VALUES ('bindings-project', 'Project', 1000, 1000)`)
	seedWorkflowGraph(t, db, "bindings-project", 1000)
	execSeed(t, db, "input bindings", `UPDATE workflow_edges SET input_bindings_json = '[{"name":"summary","source":"transition_output","field":"summary"},{"name":"title","source":"task","field":"title"}]' WHERE id = ?`, workflowGraphSeedID(t, db, "edge-start-1"))
	execSeed(t, db, "legacy empty bindings", `UPDATE workflow_edges SET input_bindings_json = 'null' WHERE id = ?`, workflowGraphSeedID(t, db, "edge-done-1"))
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}
	got := optionalNodeLabelMigrationRows(t, db, `SELECT name, source, field, sort_order FROM workflow_input_bindings ORDER BY edge_id, sort_order`)
	want := [][]any{{"summary", "transition_output", "summary", int64(0)}, {"title", "task", "title", int64(1)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bindings: got %v, want %v", got, want)
	}
}

func TestOutputRequirementCollectionMigrationPreservesSemanticOrder(t *testing.T) {
	db, err := openDatabaseAtVersionForTest(t, 106)
	if err != nil {
		t.Fatal(err)
	}
	execSeed(t, db, "project", `INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms) VALUES ('outputs-project', 'Project', 1000, 1000)`)
	seedWorkflowGraph(t, db, "outputs-project", 1000)
	execSeed(t, db, "output requirements", `UPDATE workflow_edges SET output_requirements_json = '[{"field_name":"summary"},{"field_name":"risk"}]' WHERE id = ?`, workflowGraphSeedID(t, db, "edge-start-1"))
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}
	got := optionalNodeLabelMigrationRows(t, db, `SELECT field_name, sort_order FROM workflow_output_requirements ORDER BY edge_id, sort_order`)
	want := [][]any{{"summary", int64(0)}, {"risk", int64(1)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("output requirements: got %v, want %v", got, want)
	}
}

func TestPendingApprovalChoiceMigrationPreservesUnresolvedWork(t *testing.T) {
	db, err := openDatabaseAtVersionForTest(t, 107)
	if err != nil {
		t.Fatal(err)
	}
	execSeed(t, db, "project", `INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms) VALUES ('approval-project', 'Project', 1000, 1000)`)
	seedWorkflowGraph(t, db, "approval-project", 1000)
	execSeed(t, db, "task", workflowSeedTaskSQL, "approval-task", "link-1", 1, "Task", 1000, 1000)
	insertTaskCurrentNode(t, db, "approval-task", "node-agent", nil)
	sourceID := workflowGraphSeedID(t, db, "node-agent")
	groupID := workflowGraphSeedIDText(t, db, "group-done")
	edgeID := workflowGraphSeedIDText(t, db, "edge-done-1")
	targetID := workflowGraphSeedIDText(t, db, "node-done")
	execSeed(t, db, "pending work", `INSERT INTO task_pending_approvals (id, source_task_id, source_node_id, workflow_version, transition_snapshot_json, materialized_values_json, created_at_unix_ms) VALUES ('pending-choice', 'approval-task', ?, 10, json_object('id', ?, 'commentary', 'Keep commentary'), '{"summary":"Keep submitted value"}', 1000)`, sourceID, groupID)
	execSeed(t, db, "pending branch", `INSERT INTO task_pending_approval_branches (approval_id, transition_branch_key, target_snapshot_json, effective_edge_configuration_json, context_source_resolution_json) VALUES ('pending-choice', 'done', json_object('node_id', ?, 'entered_by_edge_id', ?, 'prior_values', json('{"transition_parameters":{}}')), json_object('id', ?), '{}')`, targetID, edgeID, edgeID)
	before := optionalNodeLabelMigrationRows(t, db, `SELECT id, source_task_id, source_node_id, source_transition_branch_key, source_session_id, materialized_values_json, created_at_unix_ms FROM task_pending_approvals`)
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}
	if after := optionalNodeLabelMigrationRows(t, db, `SELECT id, source_task_id, source_node_id, source_transition_branch_key, source_session_id, materialized_values_json, created_at_unix_ms FROM task_pending_approvals`); !reflect.DeepEqual(before, after) {
		t.Fatalf("lost pending work: %v -> %v", before, after)
	}
	got := optionalNodeLabelMigrationRows(t, db, `SELECT transition_group_id, commentary FROM task_pending_approvals`)
	want := [][]any{{workflowGraphSeedID(t, db, "group-done"), "Keep commentary"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected transition: got %v, want %v", got, want)
	}
	got = optionalNodeLabelMigrationRows(t, db, `SELECT approval_id, transition_branch_key, edge_id FROM task_pending_approval_branches`)
	want = [][]any{{"pending-choice", "done", workflowGraphSeedID(t, db, "edge-done-1")}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected branch: got %v, want %v", got, want)
	}
}
