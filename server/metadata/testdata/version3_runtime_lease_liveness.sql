INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES ('project-1', 'Project', 1, 1, '{}');
INSERT INTO workspaces (id, project_id, canonical_root_path, display_name, availability, is_primary, git_metadata_json, created_at_unix_ms, updated_at_unix_ms)
VALUES ('workspace-1', 'project-1', '/tmp/workspace-1', 'workspace', 'available', 1, '{}', 1, 1);
INSERT INTO sessions (id, project_id, workspace_id, artifact_relpath, name, first_prompt_preview, input_draft, parent_session_id, created_at_unix_ms, updated_at_unix_ms, last_sequence, model_request_count, in_flight_step, agents_injected, cwd_relpath, continuation_json, locked_json, usage_state_json, metadata_json)
VALUES ('session-1', 'project-1', 'workspace-1', 'projects/project-1/sessions/session-1', '', '', '', '', 1, 1, 0, 0, 0, 0, '.', '{}', '{}', '{}', '{}');
INSERT INTO runtime_leases (id, session_id, client_id, request_id, state, created_at_unix_ms, acquired_at_unix_ms, released_at_unix_ms, expires_at_unix_ms, metadata_json)
VALUES ('lease-1', 'session-1', '', 'request-1', 'active', 1, 1, 0, 0, '{}');
