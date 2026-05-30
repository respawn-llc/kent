SELECT tr.output_values_json
FROM task_runs r
JOIN task_node_placements p ON p.id = r.placement_id
JOIN task_transitions tr ON tr.source_run_id = r.id
WHERE p.task_id = ?
  AND p.node_id = ?
  AND r.completed_at_unix_ms > 0
  AND r.completed_at_unix_ms <= ?
  AND tr.state != 'rejected'
ORDER BY r.completed_at_unix_ms DESC, tr.created_at_unix_ms DESC, r.rowid DESC
LIMIT 1
