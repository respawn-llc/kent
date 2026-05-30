SELECT tt.id, t.project_id, t.workflow_id, t.id, t.short_id, t.title, tt.created_at_unix_ms
FROM task_transitions tt
JOIN task_records t ON t.id = tt.task_id
WHERE tt.state = 'pending_approval'
  AND t.canceled_at_unix_ms = 0
  AND (? = '' OR t.project_id = ?)
  AND (? = '' OR t.id = ?)
ORDER BY tt.created_at_unix_ms DESC, tt.rowid DESC
