INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('project-primary', 'Primary', 1, 1, '{}'),
       ('project-fallback', 'Fallback', 2, 2, '{}'),
       ('project-empty', 'Empty', 3, 3, '{}');
INSERT INTO workspaces (id, project_id, canonical_root_path, display_name, availability, is_primary, git_metadata_json, created_at_unix_ms, updated_at_unix_ms)
VALUES ('workspace-oldest-nonprimary', 'project-primary', '/tmp/oldest-nonprimary', 'Oldest Nonprimary', 'available', 0, '{}', 1, 1),
       ('workspace-oldest-primary', 'project-primary', '/tmp/oldest-primary', 'Oldest Primary', 'available', 1, '{}', 2, 2),
       ('workspace-newest-primary', 'project-primary', '/tmp/newest-primary', 'Newest Primary', 'available', 1, '{}', 3, 3),
       ('workspace-fallback-newest', 'project-fallback', '/tmp/fallback-newest', 'Fallback Newest', 'available', 0, '{}', 5, 5),
       ('workspace-fallback-oldest', 'project-fallback', '/tmp/fallback-oldest', 'Fallback Oldest', 'available', 0, '{}', 4, 4);
