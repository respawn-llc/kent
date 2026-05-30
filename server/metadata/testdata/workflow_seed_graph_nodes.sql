INSERT INTO workflow_nodes (id, workflow_id, node_key, kind, display_name, output_fields_json)
VALUES (?, ?, 'backlog', 'start', 'Backlog', '[]'),
       (?, ?, 'agent', 'agent', 'Agent', '[{"name":"summary","description":"Summary."}]'),
       (?, ?, 'done', 'terminal', 'Done', '[]')
