INSERT INTO workflow_edges (id, transition_group_id, edge_key, target_node_id, requires_approval, context_mode, context_source_kind, context_source_node_key, input_bindings_json, output_requirements_json, sort_order)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    transition_group_id = excluded.transition_group_id,
    edge_key = excluded.edge_key,
    target_node_id = excluded.target_node_id,
    requires_approval = excluded.requires_approval,
    context_mode = excluded.context_mode,
    context_source_kind = excluded.context_source_kind,
    context_source_node_key = excluded.context_source_node_key,
    input_bindings_json = excluded.input_bindings_json,
    output_requirements_json = excluded.output_requirements_json,
    sort_order = excluded.sort_order
WHERE EXISTS (
    SELECT 1
    FROM workflow_transition_groups tg
    JOIN workflow_nodes source ON source.id = tg.source_node_id
    WHERE tg.id = workflow_edges.transition_group_id
      AND source.workflow_id = ?
)
