SELECT
    id,
    task_id,
    placement_id,
    node_id,
    session_id,
    run_generation,
    workflow_revision_seen,
    automation_requested_at_unix_ms,
    created_at_unix_ms,
    updated_at_unix_ms,
    started_at_unix_ms,
    completed_at_unix_ms,
    interrupted_at_unix_ms,
    interruption_reason,
    interruption_detail_json,
    waiting_ask_id,
    final_answer_violation_count,
    invalid_completion_count,
    run_start_snapshot_json,
    metadata_json
FROM task_run_records
WHERE task_id = ?
  AND waiting_ask_id = ?
  AND (? = '' OR id = ?)
  AND completed_at_unix_ms = 0
  AND interrupted_at_unix_ms = 0
  AND trim(COALESCE(session_id, '')) != ''
ORDER BY updated_at_unix_ms DESC, (
    SELECT storage.rowid
    FROM task_runs storage
    WHERE storage.id = task_run_records.id
) DESC
