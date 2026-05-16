-- +goose Up
-- +goose NO TRANSACTION

PRAGMA foreign_keys = OFF;

CREATE TABLE tasks_new (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    project_workflow_link_id TEXT NOT NULL,
    workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE RESTRICT,
    workflow_revision_seen INTEGER NOT NULL CHECK (workflow_revision_seen >= 1),
    task_seq INTEGER NOT NULL CHECK (task_seq >= 1),
    short_id TEXT NOT NULL,
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    body TEXT NOT NULL DEFAULT '',
    source_url TEXT NOT NULL DEFAULT '',
    source_workspace_id TEXT REFERENCES workspaces(id) ON DELETE SET NULL,
    managed_worktree_id TEXT REFERENCES worktrees(id) ON DELETE SET NULL,
    canceled_at_unix_ms INTEGER NOT NULL DEFAULT 0 CHECK (canceled_at_unix_ms >= 0),
    cancellation_reason TEXT NOT NULL DEFAULT '',
    created_at_unix_ms INTEGER NOT NULL CHECK (created_at_unix_ms >= 0),
    updated_at_unix_ms INTEGER NOT NULL CHECK (updated_at_unix_ms >= 0),
    metadata_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    UNIQUE (project_id, task_seq),
    UNIQUE (project_id, short_id),
    FOREIGN KEY (project_id, project_workflow_link_id, workflow_id) REFERENCES project_workflow_links(project_id, id, workflow_id) ON DELETE RESTRICT
);

INSERT INTO tasks_new (
    id,
    project_id,
    project_workflow_link_id,
    workflow_id,
    workflow_revision_seen,
    task_seq,
    short_id,
    title,
    body,
    source_url,
    source_workspace_id,
    managed_worktree_id,
    canceled_at_unix_ms,
    cancellation_reason,
    created_at_unix_ms,
    updated_at_unix_ms,
    metadata_json
)
SELECT
    t.id,
    t.project_id,
    t.project_workflow_link_id,
    t.workflow_id,
    t.workflow_revision_seen,
    t.task_seq,
    t.short_id,
    t.title,
    t.body,
    t.source_url,
    (
        SELECT w.id
        FROM workspaces w
        WHERE w.project_id = t.project_id
          AND w.is_primary = 1
        ORDER BY w.created_at_unix_ms ASC, w.rowid ASC
        LIMIT 1
    ),
    t.managed_worktree_id,
    t.canceled_at_unix_ms,
    t.cancellation_reason,
    t.created_at_unix_ms,
    t.updated_at_unix_ms,
    t.metadata_json
FROM tasks t;

DROP TABLE tasks;
ALTER TABLE tasks_new RENAME TO tasks;

CREATE INDEX tasks_project_workflow_link_idx
    ON tasks(project_workflow_link_id);

CREATE INDEX tasks_source_workspace_idx
    ON tasks(source_workspace_id);

CREATE INDEX tasks_managed_worktree_idx
    ON tasks(managed_worktree_id);

CREATE INDEX tasks_project_updated_idx
    ON tasks(project_id, updated_at_unix_ms DESC);

CREATE INDEX tasks_project_short_id_idx
    ON tasks(project_id, short_id);

-- +goose StatementBegin
CREATE TRIGGER tasks_source_workspace_project_insert
BEFORE INSERT ON tasks
FOR EACH ROW
WHEN NEW.source_workspace_id IS NOT NULL
 AND NOT EXISTS (
    SELECT 1
    FROM workspaces w
    WHERE w.id = NEW.source_workspace_id
      AND w.project_id = NEW.project_id
 )
BEGIN
    SELECT RAISE(ABORT, 'source workspace must belong to task project');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER tasks_source_workspace_project_update
BEFORE UPDATE OF project_id, source_workspace_id ON tasks
FOR EACH ROW
WHEN NEW.source_workspace_id IS NOT NULL
 AND NOT EXISTS (
    SELECT 1
    FROM workspaces w
    WHERE w.id = NEW.source_workspace_id
      AND w.project_id = NEW.project_id
 )
BEGIN
    SELECT RAISE(ABORT, 'source workspace must belong to task project');
END;
-- +goose StatementEnd

PRAGMA foreign_key_check;
PRAGMA foreign_keys = ON;
