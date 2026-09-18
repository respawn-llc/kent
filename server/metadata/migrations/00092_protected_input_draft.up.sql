-- +goose Up
ALTER TABLE sessions ADD COLUMN protected_input_draft TEXT;
