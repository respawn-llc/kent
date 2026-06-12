-- +goose Up
-- +goose NO TRANSACTION

PRAGMA foreign_keys = OFF;

CREATE TABLE task_comments_new (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    body TEXT NOT NULL CHECK (length(body) <= 262144),
    author_kind TEXT NOT NULL CHECK (author_kind IN ('user', 'agent')),
    author_id TEXT NOT NULL DEFAULT '',
    created_at_unix_ms INTEGER NOT NULL CHECK (created_at_unix_ms >= 0),
    updated_at_unix_ms INTEGER NOT NULL CHECK (updated_at_unix_ms >= 0)
);

INSERT INTO task_comments_new (
    id,
    task_id,
    body,
    author_kind,
    author_id,
    created_at_unix_ms,
    updated_at_unix_ms
)
SELECT
    id,
    task_id,
    body,
    CASE WHEN author_kind = 'system' THEN 'agent' ELSE author_kind END,
    CASE WHEN author_kind = 'system' AND author_id = '' THEN 'system' ELSE author_id END,
    created_at_unix_ms,
    updated_at_unix_ms
FROM task_comments
ORDER BY rowid ASC;

DROP TABLE task_comments;
ALTER TABLE task_comments_new RENAME TO task_comments;

PRAGMA foreign_keys = ON;
