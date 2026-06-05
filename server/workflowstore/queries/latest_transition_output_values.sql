SELECT tr.output_values_json
FROM task_transitions tr
WHERE tr.task_id = ?
  AND tr.transition_id = ?
  AND tr.applied_at_unix_ms > 0
  AND tr.applied_at_unix_ms <= ?
  AND tr.state != 'rejected'
ORDER BY tr.applied_at_unix_ms DESC, tr.created_at_unix_ms DESC, tr.rowid DESC
LIMIT 1
