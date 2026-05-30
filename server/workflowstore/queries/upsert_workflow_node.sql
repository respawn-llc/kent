INSERT INTO workflow_nodes (id, workflow_id, node_key, kind, display_name, subagent_role, prompt_template, input_fields_json, join_input_providers_json, output_fields_json, group_id, sort_order)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    node_key = excluded.node_key,
    kind = excluded.kind,
    display_name = excluded.display_name,
    subagent_role = excluded.subagent_role,
    prompt_template = excluded.prompt_template,
    input_fields_json = excluded.input_fields_json,
    join_input_providers_json = excluded.join_input_providers_json,
    output_fields_json = excluded.output_fields_json,
    group_id = excluded.group_id,
    sort_order = excluded.sort_order
WHERE workflow_nodes.workflow_id = excluded.workflow_id
