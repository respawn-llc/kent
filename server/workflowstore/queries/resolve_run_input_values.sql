SELECT
    tr.commentary,
    tr.output_values_json,
    te.input_bindings_json
FROM task_node_placements p
JOIN task_transition_edges te ON te.target_placement_id = p.id
JOIN task_transitions tr ON tr.id = te.task_transition_id
WHERE p.id = ?
ORDER BY te.rowid ASC
LIMIT 1
