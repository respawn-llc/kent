SELECT
    r.id,
    r.run_start_snapshot_json
FROM task_run_records r
JOIN task_node_placements p ON p.id = r.placement_id
JOIN workflow_nodes n ON n.id = r.node_id
WHERE r.task_id = ?
  AND (? = '' OR r.id = ?)
  AND r.completed_at_unix_ms = 0
  AND r.interrupted_at_unix_ms > 0
  AND p.state = 'active'
  AND n.kind = 'agent'
ORDER BY r.interrupted_at_unix_ms DESC, (
    SELECT storage.rowid
    FROM task_runs storage
    WHERE storage.id = r.id
) DESC
