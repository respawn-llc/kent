-- +goose Up

ALTER TABLE workflows
    RENAME COLUMN graph_revision TO version;
