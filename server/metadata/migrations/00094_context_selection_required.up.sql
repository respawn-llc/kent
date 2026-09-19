-- +goose Up
-- Old parallel bindings cannot distinguish a borrowed clone source from an
-- assigned target Session. Do not guess from ancestry or the edited graph.
-- Snapshot membership before updating any Current Node.
CREATE TEMP TABLE context_selection_required_tasks (
    task_id TEXT PRIMARY KEY
);
INSERT INTO context_selection_required_tasks (task_id)
    SELECT current.task_id
    FROM task_current_nodes AS current
    JOIN workflow_nodes AS node ON node.id = current.node_id
    WHERE current.transition_branch_key IS NOT NULL
      AND current.session_id IS NOT NULL
      AND node.kind = 'agent'
      AND current.scheduling_state IN ('ready', 'admitted', 'interrupted')
    UNION
    SELECT approval.source_task_id
    FROM task_pending_approvals AS approval
    JOIN task_pending_approval_branches AS branch ON branch.approval_id = approval.id
    WHERE json_type(branch.target_snapshot_json, '$.entered_by_edge_id') = 'text'
      AND length(trim(json_extract(branch.target_snapshot_json, '$.entered_by_edge_id'))) > 0
      AND NOT EXISTS (
          SELECT 1 FROM workflow_edges AS edge
          WHERE edge.id = json_extract(branch.target_snapshot_json, '$.entered_by_edge_id')
      );
UPDATE task_current_nodes
SET scheduling_state = 'interrupted',
    interruption_reason = 'context_selection_required',
    interruption_detail_json = json_object(
        'Code', 'context_selection_required',
        'Fields', json_patch('{}', json_object(
            'previous_scheduling_state', scheduling_state,
            'previous_interruption_reason', interruption_reason,
            'previous_interruption_detail', interruption_detail_json,
            'previous_interrupted_at_unix_ms', CAST(interrupted_at_unix_ms AS TEXT)
        ))
    ),
    interrupted_at_unix_ms = CAST(strftime('%s', 'now') AS INTEGER) * 1000
WHERE task_id IN (SELECT task_id FROM context_selection_required_tasks)
  AND node_id IN (SELECT id FROM workflow_nodes WHERE kind IN ('agent', 'script'));

DROP TABLE context_selection_required_tasks;
