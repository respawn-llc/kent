UPDATE projects
SET project_key = ?, updated_at_unix_ms = ?
WHERE id = ?
  AND (
    project_key = ?
    OR NOT EXISTS (SELECT 1 FROM task_records WHERE project_id = ?)
  )
