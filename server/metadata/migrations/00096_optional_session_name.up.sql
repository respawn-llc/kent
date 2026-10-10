-- +goose Up
-- +goose NO TRANSACTION

PRAGMA foreign_keys = OFF;
PRAGMA legacy_alter_table = ON;

BEGIN IMMEDIATE;

CREATE TABLE sessions_new (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workspace_id TEXT REFERENCES workspaces(id) ON DELETE SET NULL,
    worktree_id TEXT REFERENCES worktrees(id) ON DELETE SET NULL,
    artifact_relpath TEXT NOT NULL,
    name TEXT,
    first_prompt_preview TEXT NOT NULL DEFAULT '',
    input_draft TEXT NOT NULL DEFAULT '',
    category TEXT CHECK (category IS NULL OR category IN ('main', 'subagent')),
    created_at_unix_ms INTEGER NOT NULL,
    updated_at_unix_ms INTEGER NOT NULL,
    last_sequence INTEGER NOT NULL DEFAULT 0,
    model_request_count INTEGER NOT NULL DEFAULT 0,
    launch_visible INTEGER NOT NULL DEFAULT 0,
    cwd_relpath TEXT NOT NULL DEFAULT '.',
    continuation_json TEXT NOT NULL DEFAULT '{}',
    locked_json TEXT NOT NULL DEFAULT '{}',
    usage_state_json TEXT NOT NULL DEFAULT '{}',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    previous_session_id TEXT
        CHECK (previous_session_id IS NULL OR length(trim(previous_session_id)) > 0),
    parent_agent_session_id TEXT
        CHECK (parent_agent_session_id IS NULL OR length(trim(parent_agent_session_id)) > 0),
    task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    completed_compaction_count INTEGER
        CHECK (completed_compaction_count IS NULL OR completed_compaction_count >= 0),
    manual_compact_eligible INTEGER
        CHECK (manual_compact_eligible IS NULL OR manual_compact_eligible IN (0, 1)),
    protected_input_draft TEXT
);

INSERT INTO sessions_new (
    id, project_id, workspace_id, worktree_id, artifact_relpath, name,
    first_prompt_preview, input_draft, category, created_at_unix_ms, updated_at_unix_ms,
    last_sequence, model_request_count, launch_visible, cwd_relpath, continuation_json,
    locked_json, usage_state_json, metadata_json, previous_session_id, parent_agent_session_id,
    task_id, completed_compaction_count, manual_compact_eligible, protected_input_draft
)
SELECT
    id, project_id, workspace_id, worktree_id, artifact_relpath, NULLIF(name, ''),
    first_prompt_preview, input_draft, category, created_at_unix_ms, updated_at_unix_ms,
    last_sequence, model_request_count, launch_visible, cwd_relpath, continuation_json,
    locked_json, usage_state_json, metadata_json, previous_session_id, parent_agent_session_id,
    task_id, completed_compaction_count, manual_compact_eligible, protected_input_draft
FROM sessions;

DROP TABLE sessions;
ALTER TABLE sessions_new RENAME TO sessions;

CREATE UNIQUE INDEX sessions_artifact_relpath_idx ON sessions(artifact_relpath);
CREATE INDEX sessions_project_idx ON sessions(project_id, updated_at_unix_ms DESC);
CREATE INDEX sessions_task_activity_idx
    ON sessions(task_id, created_at_unix_ms DESC, CAST('session_started:' || id AS TEXT) DESC)
    WHERE task_id IS NOT NULL;
CREATE INDEX sessions_visible_category_recency_idx
    ON sessions(project_id, COALESCE(category, 'main'), updated_at_unix_ms DESC, id DESC)
    WHERE launch_visible <> 0;
CREATE INDEX sessions_workspace_idx ON sessions(workspace_id, updated_at_unix_ms DESC);
CREATE INDEX sessions_worktree_updated_idx
    ON sessions(worktree_id, updated_at_unix_ms DESC)
    WHERE worktree_id IS NOT NULL;

-- +goose StatementBegin
CREATE TRIGGER sessions_task_owner_clear_associations
AFTER UPDATE OF task_id ON sessions
FOR EACH ROW
WHEN NEW.task_id IS NULL
BEGIN
    DELETE FROM session_workflow_node_associations
    WHERE session_id = NEW.id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER sessions_task_owner_insert
BEFORE INSERT ON sessions
FOR EACH ROW
WHEN NEW.task_id IS NOT NULL
AND NOT EXISTS (
    SELECT 1 FROM task_records task
    WHERE task.id = NEW.task_id AND task.project_id = NEW.project_id
)
BEGIN
    SELECT RAISE(ABORT, 'session task owner must belong to session project');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER sessions_task_owner_update
BEFORE UPDATE OF task_id, project_id ON sessions
FOR EACH ROW
WHEN NEW.task_id IS NOT NULL
AND NOT EXISTS (
    SELECT 1 FROM task_records task
    WHERE task.id = NEW.task_id AND task.project_id = NEW.project_id
)
BEGIN
    SELECT RAISE(ABORT, 'session task owner must belong to session project');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER sessions_workspace_project_insert
BEFORE INSERT ON sessions
FOR EACH ROW
WHEN NEW.workspace_id IS NOT NULL
AND NOT EXISTS (
    SELECT 1 FROM workspaces w
    WHERE w.id = NEW.workspace_id AND w.project_id = NEW.project_id
)
BEGIN
    SELECT RAISE(ABORT, 'session workspace must belong to project');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER sessions_workspace_project_update
BEFORE UPDATE OF project_id, workspace_id ON sessions
FOR EACH ROW
WHEN NEW.workspace_id IS NOT NULL
AND NOT EXISTS (
    SELECT 1 FROM workspaces w
    WHERE w.id = NEW.workspace_id AND w.project_id = NEW.project_id
)
BEGIN
    SELECT RAISE(ABORT, 'session workspace must belong to project');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER sessions_worktree_workspace_insert
BEFORE INSERT ON sessions
FOR EACH ROW
WHEN NEW.worktree_id IS NOT NULL
AND (
    NEW.workspace_id IS NULL
    OR NOT EXISTS (
        SELECT 1 FROM worktrees wt
        WHERE wt.id = NEW.worktree_id AND wt.workspace_id = NEW.workspace_id
    )
)
BEGIN
    SELECT RAISE(ABORT, 'session worktree must belong to session workspace');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER sessions_worktree_workspace_update
BEFORE UPDATE OF workspace_id, worktree_id ON sessions
FOR EACH ROW
WHEN NEW.worktree_id IS NOT NULL
AND (
    NEW.workspace_id IS NULL
    OR NOT EXISTS (
        SELECT 1 FROM worktrees wt
        WHERE wt.id = NEW.worktree_id AND wt.workspace_id = NEW.workspace_id
    )
)
BEGIN
    SELECT RAISE(ABORT, 'session worktree must belong to session workspace');
END;
-- +goose StatementEnd

COMMIT;

PRAGMA legacy_alter_table = OFF;
PRAGMA foreign_keys = ON;
