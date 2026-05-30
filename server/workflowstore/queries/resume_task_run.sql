UPDATE task_runs
SET
    updated_at_unix_ms = ?,
    started_at_unix_ms = 0,
    interrupted_at_unix_ms = 0,
    interruption_reason = '',
    interruption_detail_json = '{}',
    waiting_ask_id = '',
    run_generation = run_generation + 1
WHERE id = ?
  AND completed_at_unix_ms = 0
  AND interrupted_at_unix_ms > 0
