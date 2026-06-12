-- +goose Up

-- Back the task-comment list/keyset-cursor queries, which order by
-- (created_at_unix_ms DESC, id DESC) within a task, so the task-detail comments
-- sidebar doesn't sort a task's full comment history per infinite-scroll page.
-- The pre-existing task_comments_task_updated_idx only covers the task_id
-- prefix, not this ordering.
CREATE INDEX IF NOT EXISTS task_comments_task_created_idx
    ON task_comments(task_id, created_at_unix_ms DESC, id DESC);
