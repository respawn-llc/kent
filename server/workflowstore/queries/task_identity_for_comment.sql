SELECT t.id, t.project_id, t.workflow_id
FROM task_comments c
JOIN task_records t ON t.id = c.task_id
WHERE c.id = ?
LIMIT 1
