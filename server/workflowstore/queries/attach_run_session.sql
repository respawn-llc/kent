UPDATE task_runs
SET
    updated_at_unix_ms = ?,
    session_id = ?
WHERE id = ?
  AND run_generation = ?
  AND started_at_unix_ms > 0
  AND completed_at_unix_ms = 0
  AND interrupted_at_unix_ms = 0
  AND (session_id IS NULL OR session_id = ?)
