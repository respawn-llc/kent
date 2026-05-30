INSERT INTO workflow_edges (id, transition_group_id, edge_key, target_node_id, context_mode, input_bindings_json, output_requirements_json)
VALUES (?, ?, 'start', ?, 'new_session', '{}', '{}'),
       (?, ?, 'done', ?, 'new_session', '{}', '{"fields":["summary"]}')
