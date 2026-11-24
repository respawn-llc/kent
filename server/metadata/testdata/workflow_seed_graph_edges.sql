INSERT INTO workflow_edges (id, transition_group_id, edge_key, target_node_id, context_mode)
VALUES (?, ?, 'start', ?, 'new_session'),
       (?, ?, 'done', ?, 'new_session')
