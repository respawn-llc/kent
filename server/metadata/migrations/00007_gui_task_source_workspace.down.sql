-- +goose Down
-- +goose NO TRANSACTION

PRAGMA foreign_keys = OFF;

DROP TRIGGER IF EXISTS tasks_source_workspace_project_update;
DROP TRIGGER IF EXISTS tasks_source_workspace_project_insert;
DROP INDEX IF EXISTS tasks_source_workspace_idx;

CREATE TABLE tasks_old (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    project_workflow_link_id TEXT NOT NULL,
    workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE RESTRICT,
    workflow_revision_seen INTEGER NOT NULL CHECK (workflow_revision_seen >= 1),
    task_seq INTEGER NOT NULL CHECK (task_seq >= 1),
    short_id TEXT NOT NULL,
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    body TEXT NOT NULL CHECK (length(trim(body)) > 0),
    source_url TEXT NOT NULL DEFAULT '',
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

INSERT INTO tasks_old (
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
    managed_worktree_id,
    canceled_at_unix_ms,
    cancellation_reason,
    created_at_unix_ms,
    updated_at_unix_ms,
    metadata_json
)
SELECT
    id,
    project_id,
    project_workflow_link_id,
    workflow_id,
    workflow_revision_seen,
    task_seq,
    short_id,
    title,
    CASE WHEN length(trim(body)) > 0 THEN body ELSE title END,
    source_url,
    managed_worktree_id,
    canceled_at_unix_ms,
    cancellation_reason,
    created_at_unix_ms,
    updated_at_unix_ms,
    metadata_json
FROM tasks;

DROP TABLE tasks;
ALTER TABLE tasks_old RENAME TO tasks;

CREATE INDEX tasks_project_workflow_link_idx
    ON tasks(project_workflow_link_id);

CREATE INDEX tasks_managed_worktree_idx
    ON tasks(managed_worktree_id);

CREATE INDEX tasks_project_updated_idx
    ON tasks(project_id, updated_at_unix_ms DESC);

CREATE INDEX tasks_project_short_id_idx
    ON tasks(project_id, short_id);

PRAGMA foreign_key_check;
PRAGMA foreign_keys = ON;
