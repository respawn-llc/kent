DELETE FROM tasks
WHERE id IN (
    SELECT id
    FROM task_records
    WHERE workflow_id = ?
)
