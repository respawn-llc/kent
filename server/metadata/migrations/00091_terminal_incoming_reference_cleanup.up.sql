-- +goose Up

UPDATE task_current_nodes
SET entered_by_edge_id = NULL
WHERE node_id IN (SELECT id FROM workflow_nodes WHERE kind = 'terminal');
