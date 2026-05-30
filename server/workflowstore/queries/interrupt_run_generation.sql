UPDATE task_runs
SET
    updated_at_unix_ms = ?,
    interrupted_at_unix_ms = ?,
    interruption_reason = ?,
    interruption_detail_json = ?,
    waiting_ask_id = ''
WHERE id = ?
  AND run_generation = ?
  AND completed_at_unix_ms = 0
  AND interrupted_at_unix_ms = 0
