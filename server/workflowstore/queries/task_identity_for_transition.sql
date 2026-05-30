SELECT t.id, t.project_id, t.workflow_id
FROM task_transitions tt
JOIN task_records t ON t.id = tt.task_id
WHERE tt.id = ?
LIMIT 1
