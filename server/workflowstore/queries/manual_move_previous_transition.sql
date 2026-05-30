SELECT
    tr.transition_group_id,
    tr.transition_id,
    tr.transition_display_name,
    tr.output_values_json,
    tr.source_run_id,
    te.workflow_edge_id,
    te.edge_key,
    te.context_mode,
    te.requires_approval,
    te.input_bindings_json,
    te.output_requirements_json,
    te.metadata_json
FROM task_transition_records tr
JOIN task_transitions storage ON storage.id = tr.id
JOIN task_transition_edges te ON te.task_transition_id = tr.id
JOIN task_node_placements source_placement ON source_placement.id = tr.source_placement_id
WHERE te.target_placement_id = ?
  AND source_placement.node_id = ?
ORDER BY tr.created_at_unix_ms DESC, storage.rowid DESC
LIMIT 1
