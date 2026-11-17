-- +goose Up

CREATE TABLE workflow_join_providers (
    node_id BLOB NOT NULL REFERENCES workflow_nodes(id) ON DELETE CASCADE,
    sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
    input_name TEXT NOT NULL CHECK (length(input_name) > 0),
    provider_edge_id BLOB NOT NULL REFERENCES workflow_edges(id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
    PRIMARY KEY (node_id, sort_order)
) WITHOUT ROWID;

INSERT INTO workflow_join_providers (node_id, sort_order, input_name, provider_edge_id)
SELECT node.id, provider.key, json_extract(provider.value, '$.input_name'),
       kent_migration_graph_entity_id_blob_v1(json_extract(provider.value, '$.provider_edge_id'), 'Transition Branch')
FROM workflow_nodes node, json_each(node.join_input_providers_json) provider
WHERE json_type(node.join_input_providers_json) != 'null';

ALTER TABLE workflow_nodes DROP COLUMN join_input_providers_json;
