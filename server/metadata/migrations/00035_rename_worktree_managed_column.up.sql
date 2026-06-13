-- +goose Up

-- Neutralize the brand-named worktree provenance column (added in 00003) as part
-- of the Builder -> Kent rebrand. SQLite supports RENAME COLUMN since 3.25, so an
-- additive rename works for both fresh installs and any DB carried forward.
ALTER TABLE worktrees RENAME COLUMN builder_managed TO managed;
