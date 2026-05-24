-- +goose Up

ALTER TABLE workflows
    ADD COLUMN definition_revision INTEGER NOT NULL DEFAULT 1 CHECK (definition_revision >= 1);

UPDATE workflows
SET definition_revision = graph_revision
WHERE definition_revision != graph_revision;
