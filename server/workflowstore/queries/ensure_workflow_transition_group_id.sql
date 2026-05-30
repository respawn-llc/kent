SELECT source.workflow_id
FROM workflow_transition_groups tg
JOIN workflow_nodes source ON source.id = tg.source_node_id
WHERE tg.id = ?
LIMIT 1
