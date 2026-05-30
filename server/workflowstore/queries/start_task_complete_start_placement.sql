UPDATE task_node_placements
SET state = ?, updated_at_unix_ms = ?
WHERE id = ?
  AND state = 'active'
  AND task_id IN (
      SELECT id
      FROM tasks
      WHERE id = ?
        AND canceled_at_unix_ms = 0
  )
