-- +goose Up
-- +goose NO TRANSACTION

PRAGMA foreign_keys = OFF;

BEGIN IMMEDIATE;

CREATE TEMP TABLE migration_workspace_check_zero(value INTEGER NOT NULL CHECK (value = 0));

UPDATE sessions
SET workspace_id = (
    SELECT wt.workspace_id
    FROM worktrees wt
    WHERE wt.id = sessions.worktree_id
)
WHERE workspace_id IS NULL
  AND worktree_id IS NOT NULL
  AND EXISTS (
      SELECT 1
      FROM worktrees wt
      JOIN workspaces w ON w.id = wt.workspace_id
      WHERE wt.id = sessions.worktree_id
        AND w.project_id = sessions.project_id
  );

INSERT INTO migration_workspace_check_zero(value)
SELECT 1
WHERE EXISTS (
    SELECT 1
    FROM sessions s
    WHERE s.workspace_id IS NOT NULL
      AND NOT EXISTS (
          SELECT 1
          FROM workspaces w
          WHERE w.id = s.workspace_id
            AND w.project_id = s.project_id
      )
);

INSERT INTO migration_workspace_check_zero(value)
SELECT 1
WHERE EXISTS (
    SELECT 1
    FROM sessions s
    WHERE s.worktree_id IS NOT NULL
      AND (
          s.workspace_id IS NULL
          OR NOT EXISTS (
              SELECT 1
              FROM worktrees wt
              WHERE wt.id = s.worktree_id
                AND wt.workspace_id = s.workspace_id
          )
      )
);

INSERT INTO migration_workspace_check_zero(value)
SELECT 1
WHERE EXISTS (
    SELECT 1
    FROM tasks t
    WHERE t.managed_worktree_id IS NOT NULL
      AND NOT EXISTS (
          SELECT 1
          FROM worktrees wt
          JOIN workspaces source_workspace ON source_workspace.id = t.source_workspace_id
          JOIN project_workflow_links pwl ON pwl.id = t.project_workflow_link_id
          WHERE wt.id = t.managed_worktree_id
            AND wt.workspace_id = t.source_workspace_id
            AND source_workspace.project_id = pwl.project_id
      )
);

ALTER TABLE projects ADD COLUMN primary_workspace_id TEXT NOT NULL DEFAULT '';

UPDATE projects
SET primary_workspace_id = COALESCE((
    SELECT w.id
    FROM workspaces w
    WHERE w.project_id = projects.id
    ORDER BY
        CASE WHEN w.is_primary != 0 THEN 0 ELSE 1 END ASC,
        w.created_at_unix_ms ASC,
        w.rowid ASC
    LIMIT 1
), '');

UPDATE workspaces
SET is_primary = CASE
    WHEN id = (
        SELECT p.primary_workspace_id
        FROM projects p
        WHERE p.id = workspaces.project_id
    ) THEN 1
    ELSE 0
END;

CREATE INDEX projects_primary_workspace_idx
    ON projects(primary_workspace_id)
    WHERE primary_workspace_id != '';

-- +goose StatementBegin
CREATE TRIGGER projects_primary_workspace_insert
BEFORE INSERT ON projects
FOR EACH ROW
WHEN NEW.primary_workspace_id != ''
 AND NOT EXISTS (
    SELECT 1
    FROM workspaces w
    WHERE w.id = NEW.primary_workspace_id
      AND w.project_id = NEW.id
 )
BEGIN
    SELECT RAISE(ABORT, 'primary workspace must belong to project');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER projects_primary_workspace_update
BEFORE UPDATE OF id, primary_workspace_id ON projects
FOR EACH ROW
WHEN NEW.primary_workspace_id != ''
 AND NOT EXISTS (
    SELECT 1
    FROM workspaces w
    WHERE w.id = NEW.primary_workspace_id
      AND w.project_id = NEW.id
 )
BEGIN
    SELECT RAISE(ABORT, 'primary workspace must belong to project');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER workspaces_child_refs_delete_cleanup
BEFORE DELETE ON workspaces
FOR EACH ROW
BEGIN
    UPDATE sessions
    SET worktree_id = NULL
    WHERE workspace_id = OLD.id
      AND worktree_id IN (
          SELECT wt.id
          FROM worktrees wt
          WHERE wt.workspace_id = OLD.id
      );

    UPDATE tasks
    SET managed_worktree_id = NULL
    WHERE source_workspace_id = OLD.id
      AND managed_worktree_id IN (
          SELECT wt.id
          FROM worktrees wt
          WHERE wt.workspace_id = OLD.id
      );
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER workspaces_primary_workspace_delete
AFTER DELETE ON workspaces
FOR EACH ROW
BEGIN
    UPDATE projects
    SET primary_workspace_id = ''
    WHERE id = OLD.project_id
      AND primary_workspace_id = OLD.id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER workspaces_primary_workspace_update
BEFORE UPDATE OF id, project_id ON workspaces
FOR EACH ROW
WHEN EXISTS (
    SELECT 1
    FROM projects p
    WHERE p.primary_workspace_id = OLD.id
      AND (
          p.id != NEW.project_id
          OR OLD.id != NEW.id
      )
)
BEGIN
    SELECT RAISE(ABORT, 'primary workspace must belong to project');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER workspaces_session_project_update
BEFORE UPDATE OF id, project_id ON workspaces
FOR EACH ROW
WHEN EXISTS (
    SELECT 1
    FROM sessions s
    WHERE s.workspace_id = OLD.id
      AND (
          OLD.id != NEW.id
          OR s.project_id != NEW.project_id
      )
)
BEGIN
    SELECT RAISE(ABORT, 'session workspace must belong to project');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER workspaces_task_source_project_update
BEFORE UPDATE OF id, project_id ON workspaces
FOR EACH ROW
WHEN EXISTS (
    SELECT 1
    FROM tasks t
    JOIN project_workflow_links pwl ON pwl.id = t.project_workflow_link_id
    WHERE t.source_workspace_id = OLD.id
      AND (
          OLD.id != NEW.id
          OR pwl.project_id != NEW.project_id
      )
)
BEGIN
    SELECT RAISE(ABORT, 'source workspace must belong to task project');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER worktrees_session_workspace_update
BEFORE UPDATE OF id, workspace_id ON worktrees
FOR EACH ROW
WHEN EXISTS (
    SELECT 1
    FROM sessions s
    WHERE s.worktree_id = OLD.id
      AND (
          OLD.id != NEW.id
          OR s.workspace_id IS NULL
          OR s.workspace_id != NEW.workspace_id
      )
)
BEGIN
    SELECT RAISE(ABORT, 'session worktree must belong to session workspace');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER worktrees_managed_task_workspace_update
BEFORE UPDATE OF id, workspace_id ON worktrees
FOR EACH ROW
WHEN EXISTS (
    SELECT 1
    FROM tasks t
    WHERE t.managed_worktree_id = OLD.id
      AND (
          OLD.id != NEW.id
          OR t.source_workspace_id IS NULL
          OR t.source_workspace_id != NEW.workspace_id
      )
)
BEGIN
    SELECT RAISE(ABORT, 'managed worktree must belong to task source workspace');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER sessions_workspace_project_insert
BEFORE INSERT ON sessions
FOR EACH ROW
WHEN NEW.workspace_id IS NOT NULL
 AND NOT EXISTS (
    SELECT 1
    FROM workspaces w
    WHERE w.id = NEW.workspace_id
      AND w.project_id = NEW.project_id
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
    SELECT 1
    FROM workspaces w
    WHERE w.id = NEW.workspace_id
      AND w.project_id = NEW.project_id
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
        SELECT 1
        FROM worktrees wt
        WHERE wt.id = NEW.worktree_id
          AND wt.workspace_id = NEW.workspace_id
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
        SELECT 1
        FROM worktrees wt
        WHERE wt.id = NEW.worktree_id
          AND wt.workspace_id = NEW.workspace_id
    )
 )
BEGIN
    SELECT RAISE(ABORT, 'session worktree must belong to session workspace');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER tasks_managed_worktree_context_insert
BEFORE INSERT ON tasks
FOR EACH ROW
WHEN NEW.managed_worktree_id IS NOT NULL
 AND NOT EXISTS (
    SELECT 1
    FROM worktrees wt
    JOIN workspaces source_workspace ON source_workspace.id = NEW.source_workspace_id
    JOIN project_workflow_links pwl ON pwl.id = NEW.project_workflow_link_id
    WHERE wt.id = NEW.managed_worktree_id
      AND wt.workspace_id = NEW.source_workspace_id
      AND source_workspace.project_id = pwl.project_id
 )
BEGIN
    SELECT RAISE(ABORT, 'managed worktree must belong to task source workspace');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER tasks_managed_worktree_context_update
BEFORE UPDATE OF project_workflow_link_id, source_workspace_id, managed_worktree_id ON tasks
FOR EACH ROW
WHEN NEW.managed_worktree_id IS NOT NULL
 AND NOT EXISTS (
    SELECT 1
    FROM worktrees wt
    JOIN workspaces source_workspace ON source_workspace.id = NEW.source_workspace_id
    JOIN project_workflow_links pwl ON pwl.id = NEW.project_workflow_link_id
    WHERE wt.id = NEW.managed_worktree_id
      AND wt.workspace_id = NEW.source_workspace_id
      AND source_workspace.project_id = pwl.project_id
 )
BEGIN
    SELECT RAISE(ABORT, 'managed worktree must belong to task source workspace');
END;
-- +goose StatementEnd

INSERT INTO migration_workspace_check_zero(value)
SELECT 1
WHERE EXISTS (
    SELECT 1
    FROM pragma_foreign_key_check
);

DROP TABLE migration_workspace_check_zero;

COMMIT;

PRAGMA foreign_keys = ON;
