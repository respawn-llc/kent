SELECT r.id
FROM task_runs r
JOIN task_node_placements p ON p.id = r.placement_id
WHERE p.task_id = ?
  AND p.node_id = ?
  AND r.completed_at_unix_ms > 0
  AND r.completed_at_unix_ms <= ?
ORDER BY r.completed_at_unix_ms DESC, r.rowid DESC
LIMIT 1
