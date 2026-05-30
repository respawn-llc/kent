INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('project-derived', 'Project', 1, 1, '{}');
INSERT INTO workspaces (id, project_id, canonical_root_path, display_name, availability, is_primary, git_metadata_json, created_at_unix_ms, updated_at_unix_ms)
VALUES ('workspace-derived', 'project-derived', ?, 'Stored Workspace Label', 'missing', 0, '{"workspace":"metadata"}', 1, 1);
UPDATE projects SET primary_workspace_id = 'workspace-derived' WHERE id = 'project-derived';
INSERT INTO worktrees (id, workspace_id, canonical_root_path, display_name, availability, is_main, builder_managed, created_branch, origin_session_id, git_metadata_json, created_at_unix_ms, updated_at_unix_ms)
VALUES ('worktree-derived', 'workspace-derived', ?, 'Stored Worktree Label', 'missing', 0, 1, 1, 'session-derived', '{"branch_name":"main"}', 1, 1);
