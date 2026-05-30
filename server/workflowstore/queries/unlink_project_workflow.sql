DELETE FROM project_workflow_links
WHERE id = ?
  AND NOT (
      EXISTS (
          SELECT 1
          FROM projects p
          WHERE p.id = project_workflow_links.project_id
            AND p.default_project_workflow_link_id = project_workflow_links.id
      )
      AND (
          SELECT COUNT(*)
          FROM project_workflow_links active
          WHERE active.project_id = project_workflow_links.project_id
      ) > 1
  )
