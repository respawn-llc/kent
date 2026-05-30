SELECT id, session_id
FROM task_runs
WHERE placement_id = ?
ORDER BY created_at_unix_ms DESC, rowid DESC
LIMIT 1
