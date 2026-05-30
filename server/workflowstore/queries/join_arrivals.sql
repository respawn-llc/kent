SELECT
    p.id,
    p.parallel_branch_edge_id,
    te.workflow_edge_id,
    tr.source_node_key,
    tr.output_values_json
FROM task_node_placements p
JOIN task_transitions tr ON tr.source_placement_id = p.id
JOIN task_transition_edges te ON te.task_transition_id = tr.id
WHERE p.parallel_batch_transition_id = ?
  AND p.state = 'completed'
  AND te.target_node_id = ?
  AND te.state = 'applied'
ORDER BY p.parallel_branch_edge_id ASC, tr.created_at_unix_ms ASC, te.rowid ASC
