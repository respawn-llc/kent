-- +goose Up

ALTER TABLE sessions DROP COLUMN agents_injected;
