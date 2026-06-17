-- +goose Up

ALTER TABLE workflow_nodes
    ADD COLUMN completion_mode TEXT NOT NULL DEFAULT ''
    CHECK (
        completion_mode IN ('', 'auto', 'structured_output', 'tool', 'shell_command', 'unstructured_output')
        AND (completion_mode = '' OR kind = 'agent')
    );
