SELECT project_id, workflow_id, updated_at_unix_ms
FROM project_workflow_links
WHERE (? = '' OR project_id = ?)
ORDER BY updated_at_unix_ms DESC, rowid DESC
