-- name: GetWorkspaceBindingByCanonicalRoot :one
SELECT
    p.id AS project_id,
    p.display_name AS project_display_name,
    p.project_key,
    w.id AS workspace_id,
    w.canonical_root_path AS workspace_root
FROM workspaces w
JOIN projects p ON p.id = w.project_id
WHERE w.canonical_root_path = sqlc.arg(canonical_root_path)
LIMIT 1;

-- name: GetWorkspaceByCanonicalRoot :one
SELECT
    id,
    project_id,
    canonical_root_path,
    display_name,
    availability,
    is_primary,
    git_metadata_json,
    created_at_unix_ms,
    updated_at_unix_ms
FROM workspaces
WHERE canonical_root_path = sqlc.arg(canonical_root_path)
LIMIT 1;

-- name: GetWorkspaceByID :one
SELECT
    id,
    project_id,
    canonical_root_path,
    display_name,
    availability,
    is_primary,
    git_metadata_json,
    created_at_unix_ms,
    updated_at_unix_ms
FROM workspaces
WHERE id = sqlc.arg(id)
LIMIT 1;

-- name: ListProjectKeyRows :many
SELECT
    id,
    display_name,
    project_key
FROM projects
ORDER BY created_at_unix_ms ASC, rowid ASC;

-- name: GetProjectKeyState :one
SELECT
    p.id,
    p.display_name,
    p.project_key,
    p.next_task_seq,
    CAST(COALESCE(COUNT(t.id), 0) AS INTEGER) AS task_count
FROM projects p
LEFT JOIN tasks t ON t.project_id = p.id
WHERE p.id = sqlc.arg(project_id)
GROUP BY p.id, p.display_name, p.project_key, p.next_task_seq
LIMIT 1;

-- name: SetProjectKey :execrows
UPDATE projects
SET
    project_key = sqlc.arg(project_key),
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms)
WHERE id = sqlc.arg(project_id);

-- name: AllocateProjectTaskSequence :one
UPDATE projects
SET
    next_task_seq = next_task_seq + 1,
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms)
WHERE id = sqlc.arg(project_id)
RETURNING project_key, next_task_seq;

-- name: InsertWorkflow :exec
INSERT INTO workflows (
    id,
    name,
    description,
    graph_revision,
    created_at_unix_ms,
    updated_at_unix_ms,
    metadata_json
) VALUES (
    sqlc.arg(id),
    sqlc.arg(name),
    sqlc.arg(description),
    sqlc.arg(graph_revision),
    sqlc.arg(created_at_unix_ms),
    sqlc.arg(updated_at_unix_ms),
    sqlc.arg(metadata_json)
);

-- name: UpdateWorkflowInfo :execrows
UPDATE workflows
SET
    name = sqlc.arg(name),
    description = sqlc.arg(description),
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms)
WHERE id = sqlc.arg(id);

-- name: IncrementWorkflowGraphRevision :one
UPDATE workflows
SET
    graph_revision = graph_revision + 1,
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms)
WHERE id = sqlc.arg(id)
RETURNING graph_revision;

-- name: GetWorkflow :one
SELECT
    id,
    name,
    description,
    graph_revision,
    created_at_unix_ms,
    updated_at_unix_ms,
    metadata_json
FROM workflows
WHERE id = sqlc.arg(id)
LIMIT 1;

-- name: ListWorkflows :many
SELECT
    id,
    name,
    description,
    graph_revision,
    created_at_unix_ms,
    updated_at_unix_ms,
    metadata_json
FROM workflows
ORDER BY updated_at_unix_ms DESC, rowid DESC;

-- name: InsertWorkflowNode :exec
INSERT INTO workflow_nodes (
    id,
    workflow_id,
    node_key,
    kind,
    display_name,
    subagent_role,
    prompt_template,
    output_fields_json,
    group_id,
    sort_order,
    metadata_json
) VALUES (
    sqlc.arg(id),
    sqlc.arg(workflow_id),
    sqlc.arg(node_key),
    sqlc.arg(kind),
    sqlc.arg(display_name),
    sqlc.arg(subagent_role),
    sqlc.arg(prompt_template),
    sqlc.arg(output_fields_json),
    sqlc.narg(group_id),
    sqlc.arg(sort_order),
    sqlc.arg(metadata_json)
);

-- name: InsertWorkflowNodeGroup :exec
INSERT INTO workflow_node_groups (
    id,
    workflow_id,
    group_key,
    display_name,
    sort_order,
    metadata_json
) VALUES (
    sqlc.arg(id),
    sqlc.arg(workflow_id),
    sqlc.arg(group_key),
    sqlc.arg(display_name),
    sqlc.arg(sort_order),
    sqlc.arg(metadata_json)
);

-- name: UpdateWorkflowNodeGroup :execrows
UPDATE workflow_node_groups
SET
    group_key = sqlc.arg(group_key),
    display_name = sqlc.arg(display_name),
    sort_order = sqlc.arg(sort_order),
    metadata_json = sqlc.arg(metadata_json)
WHERE id = sqlc.arg(id)
  AND workflow_id = sqlc.arg(workflow_id);

-- name: DeleteWorkflowNodeGroup :execrows
DELETE FROM workflow_node_groups
WHERE id = sqlc.arg(id)
  AND workflow_id = sqlc.arg(workflow_id);

-- name: ListWorkflowNodeGroups :many
SELECT
    id,
    workflow_id,
    group_key,
    display_name,
    sort_order,
    metadata_json
FROM workflow_node_groups
WHERE workflow_id = sqlc.arg(workflow_id)
ORDER BY sort_order ASC, rowid ASC;

-- name: GetWorkflowNodeGroupByKey :one
SELECT
    id,
    workflow_id,
    group_key,
    display_name,
    sort_order,
    metadata_json
FROM workflow_node_groups
WHERE workflow_id = sqlc.arg(workflow_id)
  AND group_key = sqlc.arg(group_key)
LIMIT 1;

-- name: ListWorkflowNodes :many
SELECT
    id,
    workflow_id,
    node_key,
    kind,
    display_name,
    subagent_role,
    prompt_template,
    output_fields_json,
    group_id,
    sort_order,
    metadata_json
FROM workflow_nodes
WHERE workflow_id = sqlc.arg(workflow_id)
ORDER BY sort_order ASC, rowid ASC;

-- name: GetWorkflowNode :one
SELECT
    id,
    workflow_id,
    node_key,
    kind,
    display_name,
    subagent_role,
    prompt_template,
    output_fields_json,
    group_id,
    sort_order,
    metadata_json
FROM workflow_nodes
WHERE id = sqlc.arg(id)
LIMIT 1;

-- name: ArchiveWorkflowNode :execrows
UPDATE workflow_nodes
SET metadata_json = json_set(metadata_json, '$.archived_at_unix_ms', sqlc.arg(archived_at_unix_ms))
WHERE id = sqlc.arg(id);

-- name: DeleteWorkflowNode :execrows
DELETE FROM workflow_nodes
WHERE id = sqlc.arg(id);

-- name: CountWorkflowNodesByGroup :one
SELECT CAST(COUNT(*) AS INTEGER) AS node_count
FROM workflow_nodes
WHERE group_id = sqlc.arg(group_id);

-- name: InsertWorkflowTransitionGroup :exec
INSERT INTO workflow_transition_groups (
    id,
    workflow_id,
    source_node_id,
    transition_id,
    display_name,
    sort_order,
    metadata_json
) VALUES (
    sqlc.arg(id),
    sqlc.arg(workflow_id),
    sqlc.arg(source_node_id),
    sqlc.arg(transition_id),
    sqlc.arg(display_name),
    sqlc.arg(sort_order),
    sqlc.arg(metadata_json)
);

-- name: ListWorkflowTransitionGroups :many
SELECT
    id,
    workflow_id,
    source_node_id,
    transition_id,
    display_name,
    sort_order,
    metadata_json
FROM workflow_transition_groups
WHERE workflow_id = sqlc.arg(workflow_id)
ORDER BY sort_order ASC, rowid ASC;

-- name: InsertWorkflowEdge :exec
INSERT INTO workflow_edges (
    id,
    workflow_id,
    transition_group_id,
    edge_key,
    target_node_id,
    requires_approval,
    context_mode,
    input_bindings_json,
    output_requirements_json,
    sort_order,
    metadata_json
) VALUES (
    sqlc.arg(id),
    sqlc.arg(workflow_id),
    sqlc.arg(transition_group_id),
    sqlc.arg(edge_key),
    sqlc.arg(target_node_id),
    sqlc.arg(requires_approval),
    sqlc.arg(context_mode),
    sqlc.arg(input_bindings_json),
    sqlc.arg(output_requirements_json),
    sqlc.arg(sort_order),
    sqlc.arg(metadata_json)
);

-- name: ListWorkflowEdges :many
SELECT
    id,
    workflow_id,
    transition_group_id,
    edge_key,
    target_node_id,
    requires_approval,
    context_mode,
    input_bindings_json,
    output_requirements_json,
    sort_order,
    metadata_json
FROM workflow_edges
WHERE workflow_id = sqlc.arg(workflow_id)
ORDER BY sort_order ASC, rowid ASC;

-- name: GetWorkflowEdge :one
SELECT
    id,
    workflow_id,
    transition_group_id,
    edge_key,
    target_node_id,
    requires_approval,
    context_mode,
    input_bindings_json,
    output_requirements_json,
    sort_order,
    metadata_json
FROM workflow_edges
WHERE id = sqlc.arg(id)
LIMIT 1;

-- name: DeleteWorkflowEdge :execrows
DELETE FROM workflow_edges
WHERE id = sqlc.arg(id);

-- name: ClearProjectDefaultWorkflowLinks :exec
UPDATE project_workflow_links
SET
    is_default = 0,
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms)
WHERE project_id = sqlc.arg(project_id)
  AND unlinked_at_unix_ms = 0;

-- name: InsertProjectWorkflowLink :exec
INSERT INTO project_workflow_links (
    id,
    project_id,
    workflow_id,
    is_default,
    unlinked_at_unix_ms,
    created_at_unix_ms,
    updated_at_unix_ms
) VALUES (
    sqlc.arg(id),
    sqlc.arg(project_id),
    sqlc.arg(workflow_id),
    sqlc.arg(is_default),
    0,
    sqlc.arg(created_at_unix_ms),
    sqlc.arg(updated_at_unix_ms)
);

-- name: GetProjectWorkflowLink :one
SELECT
    id,
    project_id,
    workflow_id,
    is_default,
    unlinked_at_unix_ms,
    created_at_unix_ms,
    updated_at_unix_ms
FROM project_workflow_links
WHERE id = sqlc.arg(id)
LIMIT 1;

-- name: GetDefaultProjectWorkflowLink :one
SELECT
    id,
    project_id,
    workflow_id,
    is_default,
    unlinked_at_unix_ms,
    created_at_unix_ms,
    updated_at_unix_ms
FROM project_workflow_links
WHERE project_id = sqlc.arg(project_id)
  AND is_default = 1
  AND unlinked_at_unix_ms = 0
LIMIT 1;

-- name: GetActiveProjectWorkflowLinkByWorkflow :one
SELECT
    id,
    project_id,
    workflow_id,
    is_default,
    unlinked_at_unix_ms,
    created_at_unix_ms,
    updated_at_unix_ms
FROM project_workflow_links
WHERE project_id = sqlc.arg(project_id)
  AND workflow_id = sqlc.arg(workflow_id)
  AND unlinked_at_unix_ms = 0
LIMIT 1;

-- name: ListProjectWorkflowLinks :many
SELECT
    id,
    project_id,
    workflow_id,
    is_default,
    unlinked_at_unix_ms,
    created_at_unix_ms,
    updated_at_unix_ms
FROM project_workflow_links
WHERE project_id = sqlc.arg(project_id)
ORDER BY unlinked_at_unix_ms ASC, is_default DESC, created_at_unix_ms ASC;

-- name: CountActiveProjectWorkflowLinks :one
SELECT CAST(COUNT(*) AS INTEGER) AS active_link_count
FROM project_workflow_links
WHERE project_id = sqlc.arg(project_id)
  AND unlinked_at_unix_ms = 0;

-- name: CountTasksByProjectWorkflowLink :one
SELECT CAST(COUNT(*) AS INTEGER) AS task_count
FROM tasks
WHERE project_workflow_link_id = sqlc.arg(project_workflow_link_id);

-- name: CountNonTerminalTasksByProjectWorkflowLink :one
SELECT CAST(COUNT(DISTINCT t.id) AS INTEGER) AS task_count
FROM tasks t
JOIN task_node_placements p ON p.task_id = t.id AND p.state IN ('active', 'waiting_approval')
JOIN workflow_nodes n ON n.id = p.node_id
WHERE t.project_workflow_link_id = sqlc.arg(project_workflow_link_id)
  AND t.canceled_at_unix_ms = 0
  AND n.kind != 'terminal';

-- name: CountNonTerminalTasksByWorkflow :one
SELECT CAST(COUNT(DISTINCT t.id) AS INTEGER) AS task_count
FROM tasks t
JOIN task_node_placements p ON p.task_id = t.id AND p.state IN ('active', 'waiting_approval')
JOIN workflow_nodes n ON n.id = p.node_id
WHERE t.workflow_id = sqlc.arg(workflow_id)
  AND t.canceled_at_unix_ms = 0
  AND n.kind != 'terminal';

-- name: SoftUnlinkProjectWorkflowLink :execrows
UPDATE project_workflow_links
SET
    is_default = 0,
    unlinked_at_unix_ms = sqlc.arg(unlinked_at_unix_ms),
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms)
WHERE id = sqlc.arg(id)
  AND unlinked_at_unix_ms = 0;

-- name: DeleteProjectWorkflowLink :execrows
DELETE FROM project_workflow_links
WHERE id = sqlc.arg(id);

-- name: CountTaskNodeReferences :one
SELECT CAST(COUNT(*) AS INTEGER) AS ref_count
FROM (
    SELECT p.id FROM task_node_placements p WHERE p.node_id = sqlc.arg(node_id)
    UNION ALL
    SELECT r.id FROM task_runs r WHERE r.node_id = sqlc.arg(node_id)
    UNION ALL
    SELECT tr.id FROM task_transitions tr WHERE tr.source_node_id = sqlc.arg(node_id)
    UNION ALL
    SELECT te.id FROM task_transition_edges te WHERE te.target_node_id = sqlc.arg(node_id)
);

-- name: CountTaskEdgeReferences :one
SELECT CAST(COUNT(*) AS INTEGER) AS ref_count
FROM task_transition_edges
WHERE workflow_edge_id = sqlc.arg(edge_id);

-- name: InsertTask :exec
INSERT INTO tasks (
    id,
    project_id,
    project_workflow_link_id,
    workflow_id,
    workflow_revision_seen,
    task_seq,
    short_id,
    title,
    body,
    source_url,
    source_workspace_id,
    managed_worktree_id,
    canceled_at_unix_ms,
    cancellation_reason,
    created_at_unix_ms,
    updated_at_unix_ms,
    metadata_json
) VALUES (
    sqlc.arg(id),
    sqlc.arg(project_id),
    sqlc.arg(project_workflow_link_id),
    sqlc.arg(workflow_id),
    sqlc.arg(workflow_revision_seen),
    sqlc.arg(task_seq),
    sqlc.arg(short_id),
    sqlc.arg(title),
    sqlc.arg(body),
    sqlc.arg(source_url),
    sqlc.narg(source_workspace_id),
    sqlc.narg(managed_worktree_id),
    0,
    '',
    sqlc.arg(created_at_unix_ms),
    sqlc.arg(updated_at_unix_ms),
    sqlc.arg(metadata_json)
);

-- name: GetTask :one
SELECT
    id,
    project_id,
    project_workflow_link_id,
    workflow_id,
    workflow_revision_seen,
    task_seq,
    short_id,
    title,
    body,
    source_url,
    source_workspace_id,
    managed_worktree_id,
    canceled_at_unix_ms,
    cancellation_reason,
    created_at_unix_ms,
    updated_at_unix_ms,
    metadata_json
FROM tasks
WHERE id = sqlc.arg(id)
LIMIT 1;

-- name: UpdateTaskManagedWorktree :execrows
UPDATE tasks
SET
    managed_worktree_id = sqlc.narg(managed_worktree_id),
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms)
WHERE id = sqlc.arg(id);

-- name: ListTasksByProject :many
SELECT
    id,
    project_id,
    project_workflow_link_id,
    workflow_id,
    workflow_revision_seen,
    task_seq,
    short_id,
    title,
    body,
    source_url,
    source_workspace_id,
    managed_worktree_id,
    canceled_at_unix_ms,
    cancellation_reason,
    created_at_unix_ms,
    updated_at_unix_ms,
    metadata_json
FROM tasks
WHERE project_id = sqlc.arg(project_id)
ORDER BY updated_at_unix_ms DESC, rowid DESC;

-- name: UpdateTaskEditableFields :execrows
UPDATE tasks
SET
    title = sqlc.arg(title),
    body = sqlc.arg(body),
    source_workspace_id = sqlc.narg(source_workspace_id),
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms)
WHERE id = sqlc.arg(id);

-- name: CountTaskRunsByTask :one
SELECT CAST(COUNT(*) AS INTEGER) AS run_count
FROM task_runs
WHERE task_id = sqlc.arg(task_id);

-- name: CountNonTerminalTasksByManagedWorktree :one
SELECT CAST(COUNT(DISTINCT t.id) AS INTEGER) AS ref_count
FROM tasks t
JOIN task_node_placements p
    ON p.task_id = t.id
    AND p.state IN ('active', 'waiting_approval')
JOIN workflow_nodes n ON n.id = p.node_id
WHERE t.managed_worktree_id = sqlc.arg(managed_worktree_id)
  AND t.canceled_at_unix_ms = 0
  AND n.kind != 'terminal';

-- name: InsertTaskNodePlacement :exec
INSERT INTO task_node_placements (
    id,
    task_id,
    node_id,
    state,
    created_by_transition_id,
    parallel_batch_transition_id,
    parallel_branch_edge_id,
    created_at_unix_ms,
    updated_at_unix_ms
) VALUES (
    sqlc.arg(id),
    sqlc.arg(task_id),
    sqlc.arg(node_id),
    sqlc.arg(state),
    sqlc.narg(created_by_transition_id),
    sqlc.narg(parallel_batch_transition_id),
    sqlc.narg(parallel_branch_edge_id),
    sqlc.arg(created_at_unix_ms),
    sqlc.arg(updated_at_unix_ms)
);

-- name: UpdateTaskNodePlacementState :execrows
UPDATE task_node_placements
SET
    state = sqlc.arg(state),
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms)
WHERE id = sqlc.arg(id);

-- name: ListTaskNodePlacements :many
SELECT
    id,
    task_id,
    node_id,
    state,
    created_by_transition_id,
    parallel_batch_transition_id,
    parallel_branch_edge_id,
    created_at_unix_ms,
    updated_at_unix_ms
FROM task_node_placements
WHERE task_id = sqlc.arg(task_id)
ORDER BY created_at_unix_ms ASC, rowid ASC;

-- name: ListTaskNodePlacementsByTasks :many
SELECT
    id,
    task_id,
    node_id,
    state,
    created_by_transition_id,
    parallel_batch_transition_id,
    parallel_branch_edge_id,
    created_at_unix_ms,
    updated_at_unix_ms
FROM task_node_placements
WHERE task_id IN (sqlc.slice('task_ids'))
ORDER BY task_id ASC, created_at_unix_ms ASC, rowid ASC;

-- name: GetActiveStartPlacementForTask :one
SELECT
    p.id,
    p.task_id,
    p.node_id,
    p.state,
    p.created_by_transition_id,
    p.parallel_batch_transition_id,
    p.parallel_branch_edge_id,
    p.created_at_unix_ms,
    p.updated_at_unix_ms
FROM task_node_placements p
JOIN workflow_nodes n ON n.id = p.node_id
WHERE p.task_id = sqlc.arg(task_id)
  AND p.state = 'active'
  AND n.kind = 'start'
LIMIT 1;

-- name: InsertTaskRun :exec
INSERT INTO task_runs (
    id,
    task_id,
    placement_id,
    node_id,
    session_id,
    run_generation,
    workflow_revision_seen,
    automation_requested_at_unix_ms,
    created_at_unix_ms,
    updated_at_unix_ms,
    started_at_unix_ms,
    completed_at_unix_ms,
    interrupted_at_unix_ms,
    interruption_reason,
    interruption_detail_json,
    waiting_ask_id,
    final_answer_violation_count,
    invalid_completion_count,
    run_start_snapshot_json,
    metadata_json
) VALUES (
    sqlc.arg(id),
    sqlc.arg(task_id),
    sqlc.arg(placement_id),
    sqlc.arg(node_id),
    sqlc.narg(session_id),
    sqlc.arg(run_generation),
    sqlc.arg(workflow_revision_seen),
    sqlc.arg(automation_requested_at_unix_ms),
    sqlc.arg(created_at_unix_ms),
    sqlc.arg(updated_at_unix_ms),
    sqlc.arg(started_at_unix_ms),
    sqlc.arg(completed_at_unix_ms),
    sqlc.arg(interrupted_at_unix_ms),
    sqlc.arg(interruption_reason),
    sqlc.arg(interruption_detail_json),
    sqlc.arg(waiting_ask_id),
    sqlc.arg(final_answer_violation_count),
    sqlc.arg(invalid_completion_count),
    sqlc.arg(run_start_snapshot_json),
    sqlc.arg(metadata_json)
);

-- name: UpdateTaskRunOutcome :execrows
UPDATE task_runs
SET
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms),
    completed_at_unix_ms = sqlc.arg(completed_at_unix_ms),
    interrupted_at_unix_ms = sqlc.arg(interrupted_at_unix_ms),
    interruption_reason = sqlc.arg(interruption_reason),
    interruption_detail_json = sqlc.arg(interruption_detail_json),
    waiting_ask_id = sqlc.arg(waiting_ask_id),
    final_answer_violation_count = sqlc.arg(final_answer_violation_count),
    invalid_completion_count = sqlc.arg(invalid_completion_count)
WHERE id = sqlc.arg(id);

-- name: ListTaskRuns :many
SELECT
    id,
    task_id,
    placement_id,
    node_id,
    session_id,
    run_generation,
    workflow_revision_seen,
    automation_requested_at_unix_ms,
    created_at_unix_ms,
    updated_at_unix_ms,
    started_at_unix_ms,
    completed_at_unix_ms,
    interrupted_at_unix_ms,
    interruption_reason,
    interruption_detail_json,
    waiting_ask_id,
    final_answer_violation_count,
    invalid_completion_count,
    run_start_snapshot_json,
    metadata_json
FROM task_runs
WHERE task_id = sqlc.arg(task_id)
ORDER BY created_at_unix_ms ASC, rowid ASC;

-- name: GetTaskRun :one
SELECT
    id,
    task_id,
    placement_id,
    node_id,
    session_id,
    run_generation,
    workflow_revision_seen,
    automation_requested_at_unix_ms,
    created_at_unix_ms,
    updated_at_unix_ms,
    started_at_unix_ms,
    completed_at_unix_ms,
    interrupted_at_unix_ms,
    interruption_reason,
    interruption_detail_json,
    waiting_ask_id,
    final_answer_violation_count,
    invalid_completion_count,
    run_start_snapshot_json,
    metadata_json
FROM task_runs
WHERE id = sqlc.arg(id)
LIMIT 1;

-- name: ListRunnableWorkflowRuns :many
SELECT
    r.id,
    r.task_id,
    r.placement_id,
    r.node_id,
    r.session_id,
    r.run_generation,
    r.workflow_revision_seen,
    r.automation_requested_at_unix_ms,
    r.created_at_unix_ms,
    r.updated_at_unix_ms,
    r.started_at_unix_ms,
    r.completed_at_unix_ms,
    r.interrupted_at_unix_ms,
    r.interruption_reason,
    r.interruption_detail_json,
    r.waiting_ask_id,
    r.final_answer_violation_count,
    r.invalid_completion_count,
    r.run_start_snapshot_json,
    r.metadata_json
FROM task_runs r
JOIN tasks t ON t.id = r.task_id
JOIN task_node_placements p ON p.id = r.placement_id
JOIN workflow_nodes n ON n.id = r.node_id
WHERE r.automation_requested_at_unix_ms > 0
  AND r.started_at_unix_ms = 0
  AND r.completed_at_unix_ms = 0
  AND r.interrupted_at_unix_ms = 0
  AND r.waiting_ask_id = ''
  AND t.canceled_at_unix_ms = 0
  AND p.state = 'active'
  AND n.kind = 'agent'
ORDER BY r.automation_requested_at_unix_ms ASC, r.id ASC
LIMIT sqlc.arg(limit);

-- name: ClaimWorkflowRun :one
UPDATE task_runs
SET
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms),
    started_at_unix_ms = sqlc.arg(started_at_unix_ms),
    run_generation = run_generation + 1
WHERE task_runs.id = sqlc.arg(id)
  AND run_generation = sqlc.arg(expected_generation)
  AND automation_requested_at_unix_ms > 0
  AND started_at_unix_ms = 0
  AND completed_at_unix_ms = 0
  AND interrupted_at_unix_ms = 0
  AND waiting_ask_id = ''
  AND EXISTS (
      SELECT 1
      FROM tasks t
      JOIN task_node_placements p ON p.id = task_runs.placement_id
      JOIN workflow_nodes n ON n.id = task_runs.node_id
      WHERE t.id = task_runs.task_id
        AND t.canceled_at_unix_ms = 0
        AND p.state = 'active'
        AND n.kind = 'agent'
  )
RETURNING
    id,
    task_id,
    placement_id,
    node_id,
    session_id,
    run_generation,
    workflow_revision_seen,
    automation_requested_at_unix_ms,
    created_at_unix_ms,
    updated_at_unix_ms,
    started_at_unix_ms,
    completed_at_unix_ms,
    interrupted_at_unix_ms,
    interruption_reason,
    interruption_detail_json,
    waiting_ask_id,
    final_answer_violation_count,
    invalid_completion_count,
    run_start_snapshot_json,
    metadata_json;

-- name: ListWaitingAskWorkflowRuns :many
SELECT
    id,
    task_id,
    placement_id,
    node_id,
    session_id,
    run_generation,
    workflow_revision_seen,
    automation_requested_at_unix_ms,
    created_at_unix_ms,
    updated_at_unix_ms,
    started_at_unix_ms,
    completed_at_unix_ms,
    interrupted_at_unix_ms,
    interruption_reason,
    interruption_detail_json,
    waiting_ask_id,
    final_answer_violation_count,
    invalid_completion_count,
    run_start_snapshot_json,
    metadata_json
FROM task_runs
WHERE waiting_ask_id != ''
  AND completed_at_unix_ms = 0
  AND interrupted_at_unix_ms = 0
ORDER BY updated_at_unix_ms ASC, id ASC;

-- name: InterruptWorkflowRun :execrows
UPDATE task_runs
SET
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms),
    interrupted_at_unix_ms = sqlc.arg(interrupted_at_unix_ms),
    interruption_reason = sqlc.arg(interruption_reason),
    interruption_detail_json = sqlc.arg(interruption_detail_json),
    waiting_ask_id = ''
WHERE id = sqlc.arg(id)
  AND completed_at_unix_ms = 0
  AND interrupted_at_unix_ms = 0;

-- name: InterruptStartedWorkflowRunsForRecovery :execrows
UPDATE task_runs
SET
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms),
    interrupted_at_unix_ms = sqlc.arg(interrupted_at_unix_ms),
    interruption_reason = sqlc.arg(interruption_reason),
    interruption_detail_json = sqlc.arg(interruption_detail_json)
WHERE started_at_unix_ms > 0
  AND completed_at_unix_ms = 0
  AND interrupted_at_unix_ms = 0
  AND waiting_ask_id = '';

-- name: InterruptActiveTaskRuns :execrows
UPDATE task_runs
SET
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms),
    interrupted_at_unix_ms = sqlc.arg(interrupted_at_unix_ms),
    interruption_reason = sqlc.arg(interruption_reason),
    interruption_detail_json = sqlc.arg(interruption_detail_json),
    waiting_ask_id = ''
WHERE task_id = sqlc.arg(task_id)
  AND completed_at_unix_ms = 0
  AND interrupted_at_unix_ms = 0;

-- name: CancelTask :execrows
UPDATE tasks
SET
    canceled_at_unix_ms = sqlc.arg(canceled_at_unix_ms),
    cancellation_reason = sqlc.arg(cancellation_reason),
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms)
WHERE id = sqlc.arg(id);

-- name: InsertTaskTransition :exec
INSERT INTO task_transitions (
    id,
    task_id,
    source_run_id,
    source_placement_id,
    source_node_id,
    source_node_key,
    source_node_display_name,
    transition_group_id,
    transition_id,
    transition_display_name,
    workflow_revision_seen,
    actor,
    state,
    commentary,
    output_values_json,
    created_at_unix_ms,
    applied_at_unix_ms
) VALUES (
    sqlc.arg(id),
    sqlc.arg(task_id),
    sqlc.narg(source_run_id),
    sqlc.narg(source_placement_id),
    sqlc.narg(source_node_id),
    sqlc.arg(source_node_key),
    sqlc.arg(source_node_display_name),
    sqlc.narg(transition_group_id),
    sqlc.arg(transition_id),
    sqlc.arg(transition_display_name),
    sqlc.arg(workflow_revision_seen),
    sqlc.arg(actor),
    sqlc.arg(state),
    sqlc.arg(commentary),
    sqlc.arg(output_values_json),
    sqlc.arg(created_at_unix_ms),
    sqlc.arg(applied_at_unix_ms)
);

-- name: ListTaskTransitions :many
SELECT
    id,
    task_id,
    source_run_id,
    source_placement_id,
    source_node_id,
    source_node_key,
    source_node_display_name,
    transition_group_id,
    transition_id,
    transition_display_name,
    workflow_revision_seen,
    actor,
    state,
    commentary,
    output_values_json,
    created_at_unix_ms,
    applied_at_unix_ms
FROM task_transitions
WHERE task_id = sqlc.arg(task_id)
ORDER BY created_at_unix_ms ASC, rowid ASC;

-- name: InsertTaskTransitionEdge :exec
INSERT INTO task_transition_edges (
    id,
    task_transition_id,
    workflow_edge_id,
    edge_key,
    workflow_revision_seen,
    target_node_id,
    target_node_key,
    target_node_display_name,
    target_node_kind,
    target_placement_id,
    state,
    context_mode,
    requires_approval,
    input_bindings_json,
    output_requirements_json,
    metadata_json
) VALUES (
    sqlc.arg(id),
    sqlc.arg(task_transition_id),
    sqlc.narg(workflow_edge_id),
    sqlc.arg(edge_key),
    sqlc.arg(workflow_revision_seen),
    sqlc.narg(target_node_id),
    sqlc.arg(target_node_key),
    sqlc.arg(target_node_display_name),
    sqlc.arg(target_node_kind),
    sqlc.narg(target_placement_id),
    sqlc.arg(state),
    sqlc.arg(context_mode),
    sqlc.arg(requires_approval),
    sqlc.arg(input_bindings_json),
    sqlc.arg(output_requirements_json),
    sqlc.arg(metadata_json)
);

-- name: ListTaskTransitionEdges :many
SELECT
    id,
    task_transition_id,
    workflow_edge_id,
    edge_key,
    workflow_revision_seen,
    target_node_id,
    target_node_key,
    target_node_display_name,
    target_node_kind,
    target_placement_id,
    state,
    context_mode,
    requires_approval,
    input_bindings_json,
    output_requirements_json,
    metadata_json
FROM task_transition_edges
WHERE task_transition_id = sqlc.arg(task_transition_id)
ORDER BY rowid ASC;

-- name: InsertTaskComment :exec
INSERT INTO task_comments (
    id,
    task_id,
    body,
    author_kind,
    author_id,
    source_run_id,
    created_at_unix_ms,
    updated_at_unix_ms,
    deleted_at_unix_ms,
    metadata_json
) VALUES (
    sqlc.arg(id),
    sqlc.arg(task_id),
    sqlc.arg(body),
    sqlc.arg(author_kind),
    sqlc.arg(author_id),
    sqlc.narg(source_run_id),
    sqlc.arg(created_at_unix_ms),
    sqlc.arg(updated_at_unix_ms),
    0,
    sqlc.arg(metadata_json)
);

-- name: UpdateTaskCommentBody :execrows
UPDATE task_comments
SET
    body = sqlc.arg(body),
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms)
WHERE id = sqlc.arg(id)
  AND deleted_at_unix_ms = 0;

-- name: SoftDeleteTaskComment :execrows
UPDATE task_comments
SET
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms),
    deleted_at_unix_ms = sqlc.arg(deleted_at_unix_ms)
WHERE id = sqlc.arg(id)
  AND deleted_at_unix_ms = 0;

-- name: ListTaskComments :many
SELECT
    id,
    task_id,
    body,
    author_kind,
    author_id,
    source_run_id,
    created_at_unix_ms,
    updated_at_unix_ms,
    deleted_at_unix_ms,
    metadata_json
FROM task_comments
WHERE task_id = sqlc.arg(task_id)
  AND (sqlc.arg(include_deleted) != 0 OR deleted_at_unix_ms = 0)
ORDER BY updated_at_unix_ms DESC, rowid DESC;

-- name: InsertWorkflowEvent :one
INSERT INTO workflow_events (
    project_id,
    workflow_id,
    resource,
    action,
    changed_ids_json,
    occurred_at_unix_ms
) VALUES (
    sqlc.arg(project_id),
    sqlc.arg(workflow_id),
    sqlc.arg(resource),
    sqlc.arg(action),
    sqlc.arg(changed_ids_json),
    sqlc.arg(occurred_at_unix_ms)
)
RETURNING sequence;

-- name: GetLatestWorkflowEventSequence :one
SELECT COALESCE(MAX(sequence), 0) AS sequence
FROM workflow_events
WHERE sqlc.arg(project_id) = ''
   OR project_id = sqlc.arg(project_id)
   OR project_id = '';

-- name: ListWorkflowEventsAfter :many
SELECT
    sequence,
    project_id,
    workflow_id,
    resource,
    action,
    changed_ids_json,
    occurred_at_unix_ms
FROM workflow_events
WHERE sequence > sqlc.arg(after_sequence)
  AND (
      sqlc.arg(project_id) = ''
      OR project_id = sqlc.arg(project_id)
      OR project_id = ''
  )
ORDER BY sequence ASC
LIMIT sqlc.arg(limit_rows);

-- name: GetWorkspaceBindingByID :one
SELECT
    p.id AS project_id,
    p.display_name AS project_display_name,
    p.project_key,
    w.id AS workspace_id,
    w.canonical_root_path AS workspace_root
FROM workspaces w
JOIN projects p ON p.id = w.project_id
WHERE w.id = sqlc.arg(workspace_id)
LIMIT 1;

-- name: UpsertProject :exec
INSERT INTO projects (
    id,
    display_name,
    created_at_unix_ms,
    updated_at_unix_ms,
    metadata_json
) VALUES (
    sqlc.arg(id),
    sqlc.arg(display_name),
    sqlc.arg(created_at_unix_ms),
    sqlc.arg(updated_at_unix_ms),
    sqlc.arg(metadata_json)
)
ON CONFLICT(id) DO UPDATE SET
    display_name = excluded.display_name,
    updated_at_unix_ms = excluded.updated_at_unix_ms,
    metadata_json = excluded.metadata_json;

-- name: UpsertWorkspace :exec
INSERT INTO workspaces (
    id,
    project_id,
    canonical_root_path,
    display_name,
    availability,
    is_primary,
    git_metadata_json,
    created_at_unix_ms,
    updated_at_unix_ms
) VALUES (
    sqlc.arg(id),
    sqlc.arg(project_id),
    sqlc.arg(canonical_root_path),
    sqlc.arg(display_name),
    sqlc.arg(availability),
    sqlc.arg(is_primary),
    sqlc.arg(git_metadata_json),
    sqlc.arg(created_at_unix_ms),
    sqlc.arg(updated_at_unix_ms)
)
ON CONFLICT(id) DO UPDATE SET
    project_id = excluded.project_id,
    canonical_root_path = excluded.canonical_root_path,
    display_name = excluded.display_name,
    availability = excluded.availability,
    is_primary = excluded.is_primary,
    git_metadata_json = excluded.git_metadata_json,
    updated_at_unix_ms = excluded.updated_at_unix_ms;

-- name: InsertWorkspaceBinding :execrows
INSERT INTO workspaces (
    id,
    project_id,
    canonical_root_path,
    display_name,
    availability,
    is_primary,
    git_metadata_json,
    created_at_unix_ms,
    updated_at_unix_ms
) VALUES (
    sqlc.arg(id),
    sqlc.arg(project_id),
    sqlc.arg(canonical_root_path),
    sqlc.arg(display_name),
    sqlc.arg(availability),
    sqlc.arg(is_primary),
    sqlc.arg(git_metadata_json),
    sqlc.arg(created_at_unix_ms),
    sqlc.arg(updated_at_unix_ms)
)
ON CONFLICT(canonical_root_path) DO NOTHING;

-- name: UpdateWorkspaceBindingCanonicalRoot :execrows
UPDATE workspaces
SET
    canonical_root_path = sqlc.arg(canonical_root_path),
    display_name = sqlc.arg(display_name),
    availability = sqlc.arg(availability),
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms)
WHERE id = sqlc.arg(id);

-- name: ListWorktreesByWorkspaceID :many
SELECT
    id,
    workspace_id,
    canonical_root_path,
    display_name,
    availability,
    is_main,
    builder_managed,
    created_branch,
    origin_session_id,
    git_metadata_json,
    created_at_unix_ms,
    updated_at_unix_ms
FROM worktrees
WHERE workspace_id = sqlc.arg(workspace_id)
ORDER BY created_at_unix_ms ASC, rowid ASC;

-- name: GetWorktreeByID :one
SELECT
    id,
    workspace_id,
    canonical_root_path,
    display_name,
    availability,
    is_main,
    builder_managed,
    created_branch,
    origin_session_id,
    git_metadata_json,
    created_at_unix_ms,
    updated_at_unix_ms
FROM worktrees
WHERE id = sqlc.arg(id)
LIMIT 1;

-- name: GetWorktreeByCanonicalRoot :one
SELECT
    id,
    workspace_id,
    canonical_root_path,
    display_name,
    availability,
    is_main,
    builder_managed,
    created_branch,
    origin_session_id,
    git_metadata_json,
    created_at_unix_ms,
    updated_at_unix_ms
FROM worktrees
WHERE canonical_root_path = sqlc.arg(canonical_root_path)
LIMIT 1;

-- name: UpsertWorktree :exec
INSERT INTO worktrees (
    id,
    workspace_id,
    canonical_root_path,
    display_name,
    availability,
    is_main,
    builder_managed,
    created_branch,
    origin_session_id,
    git_metadata_json,
    created_at_unix_ms,
    updated_at_unix_ms
) VALUES (
    sqlc.arg(id),
    sqlc.arg(workspace_id),
    sqlc.arg(canonical_root_path),
    sqlc.arg(display_name),
    sqlc.arg(availability),
    sqlc.arg(is_main),
    sqlc.arg(builder_managed),
    sqlc.arg(created_branch),
    sqlc.arg(origin_session_id),
    sqlc.arg(git_metadata_json),
    sqlc.arg(created_at_unix_ms),
    sqlc.arg(updated_at_unix_ms)
)
ON CONFLICT(canonical_root_path) DO UPDATE SET
    workspace_id = excluded.workspace_id,
    display_name = excluded.display_name,
    availability = excluded.availability,
    is_main = excluded.is_main,
    builder_managed = excluded.builder_managed,
    created_branch = excluded.created_branch,
    origin_session_id = excluded.origin_session_id,
    git_metadata_json = excluded.git_metadata_json,
    updated_at_unix_ms = excluded.updated_at_unix_ms;

-- name: DeleteWorktreeByID :execrows
DELETE FROM worktrees
WHERE id = sqlc.arg(id);

-- name: UpdateWorktreeCanonicalRoot :execrows
UPDATE worktrees
SET
    canonical_root_path = sqlc.arg(canonical_root_path),
    display_name = sqlc.arg(display_name),
    availability = sqlc.arg(availability),
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms)
WHERE id = sqlc.arg(id);

-- name: UpsertSession :exec
INSERT INTO sessions (
    id,
    project_id,
    workspace_id,
    worktree_id,
    artifact_relpath,
    name,
    first_prompt_preview,
    input_draft,
    parent_session_id,
    created_at_unix_ms,
    updated_at_unix_ms,
    last_sequence,
    model_request_count,
    in_flight_step,
    agents_injected,
    launch_visible,
    cwd_relpath,
    continuation_json,
    locked_json,
    usage_state_json,
    metadata_json
) VALUES (
    sqlc.arg(id),
    sqlc.arg(project_id),
    sqlc.arg(workspace_id),
    sqlc.narg(worktree_id),
    sqlc.arg(artifact_relpath),
    sqlc.arg(name),
    sqlc.arg(first_prompt_preview),
    sqlc.arg(input_draft),
    sqlc.arg(parent_session_id),
    sqlc.arg(created_at_unix_ms),
    sqlc.arg(updated_at_unix_ms),
    sqlc.arg(last_sequence),
    sqlc.arg(model_request_count),
    sqlc.arg(in_flight_step),
    sqlc.arg(agents_injected),
    sqlc.arg(launch_visible),
    sqlc.arg(cwd_relpath),
    sqlc.arg(continuation_json),
    sqlc.arg(locked_json),
    sqlc.arg(usage_state_json),
    sqlc.arg(metadata_json)
)
ON CONFLICT(id) DO UPDATE SET
    project_id = excluded.project_id,
    workspace_id = excluded.workspace_id,
    worktree_id = excluded.worktree_id,
    artifact_relpath = excluded.artifact_relpath,
    name = excluded.name,
    first_prompt_preview = excluded.first_prompt_preview,
    input_draft = excluded.input_draft,
    parent_session_id = excluded.parent_session_id,
    updated_at_unix_ms = excluded.updated_at_unix_ms,
    last_sequence = excluded.last_sequence,
    model_request_count = excluded.model_request_count,
    in_flight_step = excluded.in_flight_step,
    agents_injected = excluded.agents_injected,
    launch_visible = CASE
        WHEN sessions.launch_visible <> 0 OR excluded.launch_visible <> 0 THEN 1
        ELSE 0
    END,
    cwd_relpath = excluded.cwd_relpath,
    continuation_json = excluded.continuation_json,
    locked_json = excluded.locked_json,
    usage_state_json = excluded.usage_state_json,
    metadata_json = excluded.metadata_json;

-- name: GetProjectDisplayName :one
SELECT display_name
FROM projects
WHERE id = sqlc.arg(project_id)
LIMIT 1;

-- name: CountProjectWorkspaces :one
SELECT CAST(COUNT(*) AS INTEGER) AS workspace_count
FROM workspaces
WHERE project_id = sqlc.arg(project_id);

-- name: ListProjects :many
SELECT
    p.id,
    p.display_name,
    p.project_key,
    w.canonical_root_path AS root_path,
    CAST(COALESCE(COUNT(s.id), 0) AS INTEGER) AS session_count,
    COALESCE(MAX(s.updated_at_unix_ms), p.updated_at_unix_ms) AS latest_activity_unix_ms
FROM projects p
JOIN workspaces w ON w.project_id = p.id AND w.is_primary = 1
LEFT JOIN sessions s ON s.project_id = p.id AND s.launch_visible <> 0
GROUP BY p.id, p.display_name, p.project_key, w.canonical_root_path, p.updated_at_unix_ms
ORDER BY latest_activity_unix_ms DESC;

-- name: ListProjectHomeSummaries :many
SELECT
    p.id AS project_id,
    p.project_key,
    p.display_name,
    w.id AS primary_workspace_id,
    w.display_name AS primary_workspace_display_name,
    w.canonical_root_path AS primary_workspace_root_path,
    w.updated_at_unix_ms AS primary_workspace_updated_at_unix_ms,
    COALESCE(default_workflow.id, '') AS default_workflow_id,
    COALESCE(default_workflow.name, '') AS default_workflow_name,
    CASE WHEN default_workflow.id IS NULL THEN 0 ELSE 1 END AS default_workflow_valid,
    CAST(MAX(
        p.updated_at_unix_ms,
        w.updated_at_unix_ms,
        COALESCE((SELECT MAX(s.updated_at_unix_ms) FROM sessions s WHERE s.project_id = p.id AND s.launch_visible <> 0), 0),
        COALESCE((SELECT MAX(t.updated_at_unix_ms) FROM tasks t WHERE t.project_id = p.id), 0),
        COALESCE((SELECT MAX(pwl.updated_at_unix_ms) FROM project_workflow_links pwl WHERE pwl.project_id = p.id AND pwl.unlinked_at_unix_ms = 0), 0)
    ) AS INTEGER) AS latest_activity_unix_ms,
    CAST((SELECT COUNT(*) FROM tasks t WHERE t.project_id = p.id) AS INTEGER) AS task_count,
    CAST((
        SELECT COUNT(DISTINCT attention_tasks.id)
        FROM tasks attention_tasks
        WHERE attention_tasks.project_id = p.id
          AND attention_tasks.canceled_at_unix_ms = 0
          AND (
              EXISTS (
                  SELECT 1
                  FROM task_node_placements tnp
                  WHERE tnp.task_id = attention_tasks.id
                    AND tnp.state = 'waiting_approval'
              )
              OR EXISTS (
                  SELECT 1
                  FROM task_transitions tt
                  WHERE tt.task_id = attention_tasks.id
                    AND tt.state = 'pending_approval'
              )
              OR EXISTS (
                  SELECT 1
                  FROM task_runs tr
                  WHERE tr.task_id = attention_tasks.id
                    AND tr.completed_at_unix_ms = 0
                    AND (
                        tr.interrupted_at_unix_ms > 0
                        OR trim(tr.waiting_ask_id) <> ''
                    )
              )
          )
    ) AS INTEGER) AS attention_count,
    CAST((
        SELECT COUNT(*)
        FROM project_workflow_links pwl
        WHERE pwl.project_id = p.id
          AND pwl.unlinked_at_unix_ms = 0
    ) AS INTEGER) AS workflow_count
FROM projects p
JOIN workspaces w ON w.project_id = p.id AND w.is_primary = 1
LEFT JOIN project_workflow_links default_link
    ON default_link.project_id = p.id
   AND default_link.is_default = 1
   AND default_link.unlinked_at_unix_ms = 0
LEFT JOIN workflows default_workflow ON default_workflow.id = default_link.workflow_id
ORDER BY latest_activity_unix_ms DESC, p.rowid DESC
LIMIT sqlc.arg(limit_rows)
OFFSET sqlc.arg(offset_rows);

-- name: GetProjectSummary :one
SELECT
    p.id,
    p.display_name,
    p.project_key,
    w.canonical_root_path AS root_path,
    CAST(COALESCE(COUNT(s.id), 0) AS INTEGER) AS session_count,
    COALESCE(MAX(s.updated_at_unix_ms), p.updated_at_unix_ms) AS latest_activity_unix_ms
FROM projects p
JOIN workspaces w ON w.project_id = p.id AND w.is_primary = 1
LEFT JOIN sessions s ON s.project_id = p.id AND s.launch_visible <> 0
WHERE p.id = sqlc.arg(project_id)
GROUP BY p.id, p.display_name, p.project_key, w.canonical_root_path, p.updated_at_unix_ms
LIMIT 1;

-- name: ListProjectWorkspaces :many
SELECT
    w.id,
    w.display_name,
    w.canonical_root_path AS root_path,
    w.is_primary,
    CAST(COALESCE(COUNT(s.id), 0) AS INTEGER) AS session_count,
    COALESCE(MAX(s.updated_at_unix_ms), w.updated_at_unix_ms) AS latest_activity_unix_ms
FROM workspaces w
LEFT JOIN sessions s ON s.workspace_id = w.id AND s.launch_visible <> 0
WHERE w.project_id = sqlc.arg(project_id)
GROUP BY w.id, w.display_name, w.canonical_root_path, w.is_primary, w.updated_at_unix_ms
ORDER BY w.is_primary DESC, latest_activity_unix_ms DESC, w.created_at_unix_ms ASC, w.rowid ASC;

-- name: ListSessionsByProject :many
SELECT
    id,
    name,
    first_prompt_preview,
    updated_at_unix_ms
FROM sessions
WHERE project_id = sqlc.arg(project_id)
  AND launch_visible <> 0
ORDER BY updated_at_unix_ms DESC, rowid DESC;

-- name: GetSessionRecordByID :one
SELECT
    s.id,
    s.artifact_relpath,
    s.name,
    s.first_prompt_preview,
    s.input_draft,
    s.parent_session_id,
    s.created_at_unix_ms,
    s.updated_at_unix_ms,
    s.last_sequence,
    s.model_request_count,
    s.in_flight_step,
    s.agents_injected,
    s.continuation_json,
    s.locked_json,
    s.usage_state_json,
    s.metadata_json,
    w.canonical_root_path AS workspace_root
FROM sessions s
JOIN workspaces w ON w.id = s.workspace_id
WHERE s.id = sqlc.arg(session_id)
LIMIT 1;

-- name: GetSessionExecutionTargetByID :one
SELECT
    s.id AS session_id,
    s.project_id,
    s.workspace_id,
    w.display_name AS workspace_name,
    w.canonical_root_path AS workspace_root,
    w.availability AS workspace_availability,
    s.worktree_id,
    COALESCE(wt.display_name, '') AS worktree_name,
    COALESCE(wt.canonical_root_path, '') AS worktree_root,
    COALESCE(wt.availability, '') AS worktree_availability,
    s.cwd_relpath
FROM sessions s
JOIN workspaces w ON w.id = s.workspace_id
LEFT JOIN worktrees wt ON wt.id = s.worktree_id
WHERE s.id = sqlc.arg(session_id)
LIMIT 1;

-- name: UpdateSessionExecutionTargetByID :execrows
UPDATE sessions
SET
    workspace_id = sqlc.arg(workspace_id),
    worktree_id = sqlc.narg(worktree_id),
    cwd_relpath = sqlc.arg(cwd_relpath),
    updated_at_unix_ms = sqlc.arg(updated_at_unix_ms)
WHERE id = sqlc.arg(session_id);

-- name: ListSessionsTargetingWorktree :many
SELECT
    id,
    name,
    updated_at_unix_ms
FROM sessions
WHERE worktree_id = sqlc.arg(worktree_id)
ORDER BY updated_at_unix_ms DESC, rowid DESC;

-- name: InsertRuntimeLease :exec
INSERT INTO runtime_leases (
    id,
    session_id,
    client_id,
    request_id,
    created_at_unix_ms,
    acquired_at_unix_ms,
    metadata_json
) VALUES (
    sqlc.arg(id),
    sqlc.arg(session_id),
    sqlc.arg(client_id),
    sqlc.arg(request_id),
    sqlc.arg(created_at_unix_ms),
    sqlc.arg(acquired_at_unix_ms),
    sqlc.arg(metadata_json)
);

-- name: GetRuntimeLeaseByID :one
SELECT
    id,
    session_id,
    client_id,
    request_id,
    created_at_unix_ms,
    acquired_at_unix_ms,
    metadata_json
FROM runtime_leases
WHERE id = sqlc.arg(lease_id)
LIMIT 1;
