-- +goose Up

CREATE TABLE workflow_parameters (
    edge_id BLOB NOT NULL REFERENCES workflow_edges(id) ON DELETE CASCADE,
    sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
    parameter_key TEXT NOT NULL CHECK (length(parameter_key) > 0),
    description TEXT NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('ordinary', 'target_assignee', 'target_thinking')),
    PRIMARY KEY (edge_id, sort_order)
) WITHOUT ROWID;

INSERT INTO workflow_parameters (edge_id, sort_order, parameter_key, description, purpose)
SELECT edge.id, parameter.key, json_extract(parameter.value, '$.key'),
       json_extract(parameter.value, '$.description'), json_extract(parameter.value, '$.purpose')
FROM workflow_edges edge, json_each(edge.parameters_json) parameter
WHERE json_type(edge.parameters_json) != 'null';

ALTER TABLE workflow_edges DROP COLUMN parameters_json;
