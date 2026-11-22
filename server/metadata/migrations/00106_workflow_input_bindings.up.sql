-- +goose Up

CREATE TABLE workflow_input_bindings (
    edge_id BLOB NOT NULL REFERENCES workflow_edges(id) ON DELETE CASCADE,
    sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
    name TEXT NOT NULL CHECK (length(name) > 0),
    source TEXT NOT NULL CHECK (source IN ('task', 'transition_output', 'join')),
    field TEXT NOT NULL CHECK (length(field) > 0),
    PRIMARY KEY (edge_id, sort_order)
) WITHOUT ROWID;

INSERT INTO workflow_input_bindings (edge_id, sort_order, name, source, field)
SELECT edge.id, binding.key, json_extract(binding.value, '$.name'),
       json_extract(binding.value, '$.source'), json_extract(binding.value, '$.field')
FROM workflow_edges edge, json_each(edge.input_bindings_json) binding
WHERE json_type(edge.input_bindings_json) != 'null';

ALTER TABLE workflow_edges DROP COLUMN input_bindings_json;
