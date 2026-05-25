-- +goose Up

ALTER TABLE workflow_nodes
    ADD COLUMN input_fields_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(input_fields_json));

ALTER TABLE workflow_nodes
    ADD COLUMN join_input_providers_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(join_input_providers_json));
