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
FROM task_transition_edge_records
WHERE task_transition_id IN ({{placeholders}})
ORDER BY task_transition_id ASC, (
    SELECT storage.rowid
    FROM task_transition_edges storage
    WHERE storage.id = task_transition_edge_records.id
) ASC
