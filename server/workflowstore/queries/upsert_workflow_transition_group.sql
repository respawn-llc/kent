INSERT INTO workflow_transition_groups (id, source_node_id, transition_id, display_name, description, sort_order)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    source_node_id = excluded.source_node_id,
    transition_id = excluded.transition_id,
    display_name = excluded.display_name,
    description = excluded.description,
    sort_order = excluded.sort_order
WHERE EXISTS (
    SELECT 1
    FROM workflow_nodes source
    WHERE source.id = workflow_transition_groups.source_node_id
      AND source.workflow_id = ?
)
