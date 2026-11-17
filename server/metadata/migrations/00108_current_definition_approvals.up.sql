-- +goose Up

DROP VIEW workflow_task_status_records;
DROP TRIGGER task_current_nodes_pending_approval_delete;
DROP TRIGGER task_current_nodes_pending_approval_reference_update;
DROP TRIGGER workflow_nodes_current_task_anchor_delete;
DROP TRIGGER workflow_nodes_task_reference_kind_update;

CREATE TABLE task_pending_approvals_current (
    id TEXT PRIMARY KEY,
    source_task_id TEXT NOT NULL,
    source_node_id BLOB NOT NULL
        CHECK (typeof(source_node_id) = 'blob' AND length(source_node_id) = 16 AND source_node_id != zeroblob(16)),
    source_transition_branch_key TEXT
        CHECK (source_transition_branch_key IS NULL OR length(trim(source_transition_branch_key)) BETWEEN 1 AND 64),
    source_session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    transition_group_id BLOB NOT NULL
        CHECK (typeof(transition_group_id) = 'blob' AND length(transition_group_id) = 16 AND transition_group_id != zeroblob(16)),
    commentary TEXT,
    materialized_values_json TEXT NOT NULL
        CHECK (json_valid(materialized_values_json) AND json_type(materialized_values_json) = 'object'),
    created_at_unix_ms INTEGER NOT NULL CHECK (created_at_unix_ms > 0)
);

CREATE TABLE task_pending_approval_branches_current (
    approval_id TEXT NOT NULL REFERENCES task_pending_approvals_current(id) ON DELETE CASCADE,
    transition_branch_key TEXT NOT NULL CHECK (length(trim(transition_branch_key)) BETWEEN 1 AND 64),
    edge_id BLOB NOT NULL
        CHECK (typeof(edge_id) = 'blob' AND length(edge_id) = 16 AND edge_id != zeroblob(16)),
    PRIMARY KEY (approval_id, transition_branch_key)
);

INSERT INTO task_pending_approvals_current (
    rowid, id, source_task_id, source_node_id, source_transition_branch_key, source_session_id,
    transition_group_id, commentary, materialized_values_json, created_at_unix_ms
)
SELECT rowid, id, source_task_id, source_node_id, source_transition_branch_key, source_session_id,
       kent_migration_graph_entity_id_blob_v1(json_extract(transition_snapshot_json, '$.id'), 'Transition'),
       json_extract(transition_snapshot_json, '$.commentary'), materialized_values_json, created_at_unix_ms
FROM task_pending_approvals;

INSERT INTO task_pending_approval_branches_current (rowid, approval_id, transition_branch_key, edge_id)
SELECT rowid, approval_id, transition_branch_key,
       kent_migration_graph_entity_id_blob_v1(json_extract(effective_edge_configuration_json, '$.id'), 'Transition Branch')
FROM task_pending_approval_branches;

DROP TABLE task_pending_approval_branches;
DROP TABLE task_pending_approvals;
ALTER TABLE task_pending_approvals_current RENAME TO task_pending_approvals;
ALTER TABLE task_pending_approval_branches_current RENAME TO task_pending_approval_branches;

CREATE UNIQUE INDEX task_pending_approvals_parallel_source_unique_idx
    ON task_pending_approvals(source_task_id, source_node_id, source_transition_branch_key)
    WHERE source_transition_branch_key IS NOT NULL;
CREATE UNIQUE INDEX task_pending_approvals_serial_source_unique_idx
    ON task_pending_approvals(source_task_id, source_node_id)
    WHERE source_transition_branch_key IS NULL;

-- +goose StatementBegin
CREATE TRIGGER task_pending_approvals_choice_insert
BEFORE INSERT ON task_pending_approvals
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1 FROM workflow_transition_groups transition_group
    WHERE transition_group.id = NEW.transition_group_id
      AND transition_group.source_node_id = NEW.source_node_id
)
BEGIN
    SELECT RAISE(ABORT, 'pending approval transition must belong to its source node');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER task_pending_approvals_choice_update
BEFORE UPDATE OF transition_group_id, source_node_id ON task_pending_approvals
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1 FROM workflow_transition_groups transition_group
    WHERE transition_group.id = NEW.transition_group_id
      AND transition_group.source_node_id = NEW.source_node_id
)
BEGIN
    SELECT RAISE(ABORT, 'pending approval transition must belong to its source node');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER task_pending_approval_branches_choice_insert
BEFORE INSERT ON task_pending_approval_branches
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1 FROM workflow_edges edge
    JOIN task_pending_approvals approval ON approval.id = NEW.approval_id
    WHERE edge.id = NEW.edge_id AND edge.transition_group_id = approval.transition_group_id
)
BEGIN
    SELECT RAISE(ABORT, 'pending approval branch must belong to its selected transition');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER task_pending_approval_branches_choice_update
BEFORE UPDATE OF approval_id, edge_id ON task_pending_approval_branches
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1 FROM workflow_edges edge
    JOIN task_pending_approvals approval ON approval.id = NEW.approval_id
    WHERE edge.id = NEW.edge_id AND edge.transition_group_id = approval.transition_group_id
)
BEGIN
    SELECT RAISE(ABORT, 'pending approval branch must belong to its selected transition');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER task_current_nodes_pending_approval_delete
BEFORE DELETE ON task_current_nodes
FOR EACH ROW
WHEN EXISTS (
    SELECT 1 FROM task_pending_approvals approval
    WHERE approval.source_task_id = OLD.task_id
      AND approval.source_node_id = OLD.node_id
      AND approval.source_transition_branch_key IS OLD.transition_branch_key
)
BEGIN
    SELECT RAISE(ABORT, 'current node with pending approval cannot be deleted');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER task_current_nodes_pending_approval_reference_update
BEFORE UPDATE OF task_id, node_id, transition_branch_key ON task_current_nodes
FOR EACH ROW
WHEN EXISTS (
    SELECT 1 FROM task_pending_approvals approval
    WHERE approval.source_task_id = OLD.task_id
      AND approval.source_node_id = OLD.node_id
      AND approval.source_transition_branch_key IS OLD.transition_branch_key
)
BEGIN
    SELECT RAISE(ABORT, 'current node with pending approval cannot change identity');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER task_pending_approvals_source_current_insert
BEFORE INSERT ON task_pending_approvals
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1 FROM task_current_nodes current_node
    WHERE current_node.task_id = NEW.source_task_id
      AND current_node.node_id = NEW.source_node_id
      AND current_node.transition_branch_key IS NEW.source_transition_branch_key
)
BEGIN
    SELECT RAISE(ABORT, 'pending approval source must be a current node');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER task_pending_approvals_source_current_update
BEFORE UPDATE OF source_task_id, source_node_id, source_transition_branch_key ON task_pending_approvals
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1 FROM task_current_nodes current_node
    WHERE current_node.task_id = NEW.source_task_id
      AND current_node.node_id = NEW.source_node_id
      AND current_node.transition_branch_key IS NEW.source_transition_branch_key
)
BEGIN
    SELECT RAISE(ABORT, 'pending approval source must be a current node');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER workflow_nodes_current_task_anchor_delete
BEFORE DELETE ON workflow_nodes
FOR EACH ROW
WHEN EXISTS (
    SELECT 1 FROM task_pending_approvals approval WHERE approval.source_node_id = OLD.id
) OR EXISTS (
    SELECT 1 FROM task_pending_approval_branches branch
    JOIN workflow_edges edge ON edge.id = branch.edge_id
    WHERE edge.target_node_id = OLD.id
)
BEGIN
    SELECT RAISE(ABORT, 'workflow node has current task references');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER workflow_nodes_task_reference_kind_update
BEFORE UPDATE OF kind ON workflow_nodes
FOR EACH ROW
WHEN NEW.kind != OLD.kind AND (
    EXISTS (SELECT 1 FROM task_current_nodes current_node WHERE current_node.node_id = OLD.id)
    OR EXISTS (SELECT 1 FROM task_pending_approvals approval WHERE approval.source_node_id = OLD.id)
    OR EXISTS (
        SELECT 1 FROM task_pending_approval_branches branch
        JOIN workflow_edges edge ON edge.id = branch.edge_id
        WHERE edge.target_node_id = OLD.id
    )
)
BEGIN
    SELECT RAISE(ABORT, 'workflow node kind changes are blocked for nodes referenced by current task state');
END;
-- +goose StatementEnd

CREATE VIEW workflow_task_status_records AS
WITH current_positions AS (
    SELECT task_id, node_id, scheduling_state, interruption_reason FROM task_current_nodes
), status_inputs AS (
    SELECT
        task.id AS task_id,
        EXISTS (
            SELECT 1 FROM current_positions position
            JOIN workflow_nodes node ON node.id = position.node_id
            WHERE position.task_id = task.id AND node.kind = 'terminal'
        ) AS has_done,
        EXISTS (
            SELECT 1 FROM task_pending_approvals approval WHERE approval.source_task_id = task.id
        ) AS has_waiting_approval,
        EXISTS (
            SELECT 1 FROM current_positions position
            WHERE position.task_id = task.id AND position.scheduling_state = 'interrupted'
        ) AS has_interrupted,
        EXISTS (
            SELECT 1 FROM current_positions position
            WHERE position.task_id = task.id AND position.scheduling_state = 'interrupted'
              AND position.interruption_reason NOT IN ('user_interrupt', 'workflow_runtime_canceled')
        ) AS has_interrupted_attention,
        EXISTS (
            SELECT 1 FROM current_positions position
            JOIN workflow_nodes node ON node.id = position.node_id
            WHERE position.task_id = task.id AND node.kind = 'start'
        ) AS has_backlog
    FROM task_records task
)
SELECT
    task.id AS task_id,
    CAST(input.has_done AS INTEGER) AS is_done,
    CASE
        WHEN input.has_done THEN 'done'
        WHEN input.has_waiting_approval THEN 'waiting_approval'
        WHEN input.has_interrupted THEN 'interrupted'
        WHEN input.has_backlog THEN 'backlog'
        ELSE 'active'
    END AS kind,
    CASE
        WHEN input.has_done THEN 1
        WHEN input.has_waiting_approval THEN 3
        WHEN input.has_interrupted THEN 4
        WHEN input.has_backlog THEN 7
        ELSE 8
    END AS primary_status_rank,
    COALESCE((
        SELECT json_group_array(kent_graph_entity_id_text_v1(node_id))
        FROM (
            SELECT position.node_id FROM current_positions position
            WHERE position.task_id = task.id
            ORDER BY position.node_id
        )
    ), '[]') AS node_ids_json,
    COALESCE((
        SELECT json_group_array(attention_type)
        FROM (
            SELECT 'approval' AS attention_type WHERE input.has_waiting_approval
            UNION
            SELECT 'interrupted' WHERE input.has_interrupted_attention
            ORDER BY attention_type
        )
    ), '[]') AS attention_types_json
FROM task_records task
JOIN status_inputs input ON input.task_id = task.id;
