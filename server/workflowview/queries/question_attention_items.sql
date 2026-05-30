SELECT r.id, COALESCE(r.session_id, ''), r.waiting_ask_id, t.project_id, t.workflow_id, t.id, t.short_id, t.title, r.updated_at_unix_ms
FROM task_run_records r
JOIN task_records t ON t.id = r.task_id
WHERE trim(r.waiting_ask_id) != ''
  AND r.completed_at_unix_ms = 0
  AND r.interrupted_at_unix_ms = 0
  AND t.canceled_at_unix_ms = 0
  AND (? = '' OR t.project_id = ?)
  AND (? = '' OR t.id = ?)
ORDER BY r.updated_at_unix_ms DESC, (
    SELECT storage.rowid
    FROM task_runs storage
    WHERE storage.id = r.id
) DESC
