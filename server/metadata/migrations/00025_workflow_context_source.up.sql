-- +goose Up

ALTER TABLE workflow_edges
    ADD COLUMN context_source_kind TEXT NOT NULL DEFAULT 'immediate_source'
    CHECK (context_source_kind IN ('immediate_source', 'selected_node'));

ALTER TABLE workflow_edges
    ADD COLUMN context_source_node_key TEXT NOT NULL DEFAULT ''
    CHECK (
        (context_source_kind = 'immediate_source' AND context_source_node_key = '')
        OR (context_source_kind = 'selected_node' AND length(context_source_node_key) BETWEEN 1 AND 64)
    );
