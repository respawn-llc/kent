-- +goose Up
-- +goose NO TRANSACTION

PRAGMA foreign_keys = OFF;

CREATE TABLE sessions_new (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workspace_id TEXT REFERENCES workspaces(id) ON DELETE SET NULL,
    worktree_id TEXT REFERENCES worktrees(id) ON DELETE SET NULL,
    artifact_relpath TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    first_prompt_preview TEXT NOT NULL DEFAULT '',
    input_draft TEXT NOT NULL DEFAULT '',
    parent_session_id TEXT NOT NULL DEFAULT '',
    created_at_unix_ms INTEGER NOT NULL,
    updated_at_unix_ms INTEGER NOT NULL,
    last_sequence INTEGER NOT NULL DEFAULT 0,
    model_request_count INTEGER NOT NULL DEFAULT 0,
    in_flight_step INTEGER NOT NULL DEFAULT 0,
    agents_injected INTEGER NOT NULL DEFAULT 0,
    launch_visible INTEGER NOT NULL DEFAULT 0,
    cwd_relpath TEXT NOT NULL DEFAULT '.',
    continuation_json TEXT NOT NULL DEFAULT '{}',
    locked_json TEXT NOT NULL DEFAULT '{}',
    usage_state_json TEXT NOT NULL DEFAULT '{}',
    metadata_json TEXT NOT NULL DEFAULT '{}'
);

INSERT INTO sessions_new (
    id,
    project_id,
    workspace_id,
    worktree_id,
    artifact_relpath,
    name,
    first_prompt_preview,
    input_draft,
    parent_session_id,
    created_at_unix_ms,
    updated_at_unix_ms,
    last_sequence,
    model_request_count,
    in_flight_step,
    agents_injected,
    launch_visible,
    cwd_relpath,
    continuation_json,
    locked_json,
    usage_state_json,
    metadata_json
)
SELECT
    s.id,
    s.project_id,
    s.workspace_id,
    s.worktree_id,
    s.artifact_relpath,
    s.name,
    s.first_prompt_preview,
    s.input_draft,
    s.parent_session_id,
    s.created_at_unix_ms,
    s.updated_at_unix_ms,
    s.last_sequence,
    s.model_request_count,
    s.in_flight_step,
    s.agents_injected,
    s.launch_visible,
    s.cwd_relpath,
    s.continuation_json,
    s.locked_json,
    s.usage_state_json,
    json_set(
        CASE WHEN json_valid(s.metadata_json) THEN s.metadata_json ELSE '{}' END,
        '$.workspace_root',
        COALESCE(
            NULLIF(w.canonical_root_path, ''),
            NULLIF(CASE WHEN json_valid(s.metadata_json) THEN json_extract(s.metadata_json, '$.workspace_root') ELSE '' END, ''),
            ''
        ),
        '$.workspace_container',
        COALESCE(
            NULLIF(CASE WHEN json_valid(s.metadata_json) THEN json_extract(s.metadata_json, '$.workspace_container') ELSE '' END, ''),
            NULLIF(w.display_name, ''),
            ''
        )
    )
FROM sessions s
LEFT JOIN workspaces w ON w.id = s.workspace_id;

DROP TABLE sessions;
ALTER TABLE sessions_new RENAME TO sessions;

CREATE INDEX sessions_project_idx ON sessions(project_id, updated_at_unix_ms DESC);
CREATE INDEX sessions_workspace_idx ON sessions(workspace_id, updated_at_unix_ms DESC);
CREATE UNIQUE INDEX sessions_artifact_relpath_idx ON sessions(artifact_relpath);

UPDATE tasks
SET metadata_json = json_set(
    CASE WHEN json_valid(metadata_json) THEN metadata_json ELSE '{}' END,
    '$.source_workspace_snapshot',
    json_object(
        'workspace_id', (
            SELECT w.id
            FROM workspaces w
            WHERE w.id = tasks.source_workspace_id
        ),
        'display_name', (
            SELECT w.display_name
            FROM workspaces w
            WHERE w.id = tasks.source_workspace_id
        ),
        'root_path', (
            SELECT w.canonical_root_path
            FROM workspaces w
            WHERE w.id = tasks.source_workspace_id
        )
    )
)
WHERE tasks.source_workspace_id IS NOT NULL
  AND EXISTS (
      SELECT 1
      FROM workspaces w
      WHERE w.id = tasks.source_workspace_id
  );

PRAGMA foreign_keys = ON;
