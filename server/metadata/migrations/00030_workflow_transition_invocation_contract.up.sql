-- +goose Up

ALTER TABLE workflow_edges
    ADD COLUMN prompt_template TEXT NOT NULL DEFAULT '';

ALTER TABLE workflow_edges
    ADD COLUMN parameters_json TEXT NOT NULL DEFAULT '[]'
        CHECK (json_valid(parameters_json) AND json_type(parameters_json) = 'array');
