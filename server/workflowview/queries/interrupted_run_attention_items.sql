SELECT r.id, COALESCE(r.session_id, ''), r.interruption_reason, r.interruption_detail_json, t.project_id, t.workflow_id, t.id, t.short_id, t.title, r.interrupted_at_unix_ms
FROM task_run_records r
JOIN task_records t ON t.id = r.task_id
JOIN task_node_placements p ON p.id = r.placement_id
WHERE r.interrupted_at_unix_ms > 0
  AND r.completed_at_unix_ms = 0
  AND p.state IN ('active', 'waiting_approval')
  AND t.canceled_at_unix_ms = 0
  AND (? = '' OR t.project_id = ?)
  AND (? = '' OR t.id = ?)
ORDER BY r.interrupted_at_unix_ms DESC, (
    SELECT storage.rowid
    FROM task_runs storage
    WHERE storage.id = r.id
) DESC
