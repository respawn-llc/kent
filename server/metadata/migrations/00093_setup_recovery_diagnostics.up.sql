-- +goose Up
UPDATE task_current_nodes
SET interruption_detail_json = json_remove(
    json_set(
        interruption_detail_json,
        '$.Fields',
        json_patch(
            COALESCE(json_extract(interruption_detail_json, '$.Fields'), '{}'),
            json_object(
                'error', json_extract(interruption_detail_json, '$.setup_recovery.diagnostic'),
                'setup_cause', json_extract(interruption_detail_json, '$.setup_recovery.cause'),
                'setup_script_path', json_extract(interruption_detail_json, '$.setup_recovery.script_path'),
                'setup_operation_id', json_extract(interruption_detail_json, '$.setup_recovery.setup_operation_id'),
                'setup_requirement', json_extract(interruption_detail_json, '$.setup_recovery.setup_requirement'),
                'setup_execution_target', json_extract(interruption_detail_json, '$.setup_recovery.execution_target') || '',
                'retained_worktree_id', json_extract(interruption_detail_json, '$.setup_recovery.retained_worktree.worktree_id'),
                'retained_worktree_root', json_extract(interruption_detail_json, '$.setup_recovery.retained_worktree.root'),
                'retained_previous_worktree_id', json_extract(interruption_detail_json, '$.setup_recovery.retained_previous_worktree.worktree_id'),
                'retained_previous_worktree_root', json_extract(interruption_detail_json, '$.setup_recovery.retained_previous_worktree.root')
            )
        )
    ),
    '$.setup_recovery'
)
WHERE json_type(interruption_detail_json, '$.setup_recovery') = 'object';
