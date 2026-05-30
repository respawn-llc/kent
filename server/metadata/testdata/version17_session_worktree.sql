INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('project-a', 'A', 1, 1, '{}');
INSERT INTO workspaces (id, project_id, canonical_root_path, display_name, availability, is_primary, git_metadata_json, created_at_unix_ms, updated_at_unix_ms)
VALUES ('workspace-a', 'project-a', '/tmp/workspace-a', 'workspace-a', 'available', 1, '{}', 1, 1);
INSERT INTO worktrees (id, workspace_id, canonical_root_path, display_name, availability, is_main, git_metadata_json, created_at_unix_ms, updated_at_unix_ms)
VALUES ('worktree-a', 'workspace-a', '/tmp/worktree-a', 'worktree-a', 'available', 0, '{}', 1, 1);
INSERT INTO sessions (id, project_id, workspace_id, worktree_id, artifact_relpath, created_at_unix_ms, updated_at_unix_ms)
VALUES ('session-a', 'project-a', NULL, 'worktree-a', 'projects/project-a/sessions/session-a', 1, 1);
