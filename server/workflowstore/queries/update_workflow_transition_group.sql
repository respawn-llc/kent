UPDATE workflow_transition_groups
SET
    source_node_id = ?,
    transition_id = ?,
    display_name = ?
WHERE id = ?
  AND EXISTS (
      SELECT 1
      FROM workflow_nodes source
      WHERE source.id = workflow_transition_groups.source_node_id
        AND source.workflow_id = ?
  )
