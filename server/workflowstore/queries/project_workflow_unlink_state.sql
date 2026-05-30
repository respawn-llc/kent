SELECT
    COALESCE(p.default_project_workflow_link_id, ''),
    (SELECT COUNT(*) FROM project_workflow_links active WHERE active.project_id = p.id)
FROM projects p
WHERE p.id = ?
