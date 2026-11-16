-- +goose Up
ALTER TABLE sessions
ADD COLUMN last_agent_activity_at_unix_ms INTEGER;
