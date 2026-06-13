-- +goose Down

ALTER TABLE worktrees RENAME COLUMN managed TO builder_managed;
