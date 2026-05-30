UPDATE task_runs
SET
    updated_at_unix_ms = ?,
    final_answer_violation_count = final_answer_violation_count + 1,
    interrupted_at_unix_ms = CASE WHEN final_answer_violation_count + 1 >= ? THEN ? ELSE interrupted_at_unix_ms END,
    interruption_reason = CASE WHEN final_answer_violation_count + 1 >= ? THEN 'workflow_protocol_violation_limit' ELSE interruption_reason END,
    interruption_detail_json = CASE WHEN final_answer_violation_count + 1 >= ? THEN ? ELSE interruption_detail_json END
WHERE id = ?
  AND completed_at_unix_ms = 0
  AND interrupted_at_unix_ms = 0
  AND (? = 0 OR run_generation = ?)
RETURNING final_answer_violation_count, interrupted_at_unix_ms
