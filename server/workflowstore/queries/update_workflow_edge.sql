UPDATE workflow_edges
SET
    transition_group_id = ?,
    edge_key = ?,
    target_node_id = ?,
    requires_approval = ?,
    context_mode = ?,
    context_source_kind = ?,
    context_source_node_key = ?,
    input_bindings_json = ?,
    output_requirements_json = ?
WHERE id = ?
  AND EXISTS (
      SELECT 1
      FROM workflow_transition_groups tg
      JOIN workflow_nodes source ON source.id = tg.source_node_id
      WHERE tg.id = workflow_edges.transition_group_id
        AND source.workflow_id = ?
  )
