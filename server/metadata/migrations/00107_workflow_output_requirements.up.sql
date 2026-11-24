-- +goose Up

CREATE TABLE workflow_output_requirements (
    edge_id BLOB NOT NULL REFERENCES workflow_edges(id) ON DELETE CASCADE,
    sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
    field_name TEXT NOT NULL CHECK (length(field_name) > 0),
    PRIMARY KEY (edge_id, sort_order)
) WITHOUT ROWID;

INSERT INTO workflow_output_requirements (edge_id, sort_order, field_name)
SELECT edge.id, requirement.key, json_extract(requirement.value, '$.field_name')
FROM workflow_edges edge, json_each(edge.output_requirements_json) requirement
WHERE json_type(edge.output_requirements_json) != 'null';

ALTER TABLE workflow_edges DROP COLUMN output_requirements_json;
