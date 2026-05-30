INSERT INTO workflow_node_groups (id, workflow_id, group_key, display_name, sort_order)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    group_key = excluded.group_key,
    display_name = excluded.display_name,
    sort_order = excluded.sort_order
WHERE workflow_node_groups.workflow_id = excluded.workflow_id
