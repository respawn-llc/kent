-- +goose Up
ALTER TABLE sessions
ADD COLUMN last_model_tool_activity_at_unix_ms INTEGER;
