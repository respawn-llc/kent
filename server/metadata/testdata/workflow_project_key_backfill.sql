INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('project-a', 'Builder', 1, 1, '{}'), ('project-b', 'Builder', 2, 2, '{}');
INSERT INTO workspaces (id, project_id, canonical_root_path, display_name, availability, is_primary, git_metadata_json, created_at_unix_ms, updated_at_unix_ms)
VALUES ('workspace-a', 'project-a', '/tmp/workflow-a', 'workflow-a', 'available', 1, '{}', 1, 1),
       ('workspace-b', 'project-b', '/tmp/workflow-b', 'workflow-b', 'available', 1, '{}', 2, 2);
