UPDATE workflow_nodes
SET
    node_key = ?,
    kind = ?,
    display_name = ?,
    subagent_role = ?,
    prompt_template = ?,
    completion_mode = ?,
    input_fields_json = ?,
    join_input_providers_json = ?,
    output_fields_json = ?,
    group_id = ?
WHERE id = ?
  AND workflow_id = ?
