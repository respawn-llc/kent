UPDATE projects
SET
    default_project_workflow_link_id = '',
    updated_at_unix_ms = ?
WHERE default_project_workflow_link_id IN (
    SELECT id
    FROM project_workflow_links
    WHERE workflow_id = ?
)
