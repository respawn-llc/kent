-- +goose Up

ALTER TABLE workflow_transition_groups
    ADD COLUMN description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 1000);
