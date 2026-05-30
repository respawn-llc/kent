INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('project-a', 'A', 1, 1, '{}'),
       ('project-b', 'B', 1, 1, '{}');
INSERT INTO workspaces (id, project_id, canonical_root_path, display_name, availability, is_primary, git_metadata_json, created_at_unix_ms, updated_at_unix_ms)
VALUES ('workspace-a', 'project-a', '/tmp/workspace-a', 'workspace-a', 'available', 1, '{}', 1, 1),
       ('workspace-b', 'project-b', '/tmp/workspace-b', 'workspace-b', 'available', 1, '{}', 1, 1);
