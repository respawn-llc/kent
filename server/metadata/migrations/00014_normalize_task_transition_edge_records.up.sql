-- +goose Up
-- +goose NO TRANSACTION

PRAGMA foreign_keys = OFF;

DROP TRIGGER IF EXISTS task_transition_edges_runtime_insert;
DROP TRIGGER IF EXISTS task_transition_edges_runtime_update;

DROP VIEW IF EXISTS task_transition_edge_records;

CREATE TEMP TABLE migration_transition_edge_check_zero(value INTEGER NOT NULL CHECK (value = 0));

INSERT INTO migration_transition_edge_check_zero(value)
SELECT 1
WHERE EXISTS (
    SELECT 1
    FROM task_transition_edges te
    LEFT JOIN task_transitions tt ON tt.id = te.task_transition_id
    WHERE tt.id IS NULL
       OR te.workflow_revision_seen != tt.workflow_revision_seen
);

CREATE TABLE task_transition_edges_new (
    id TEXT PRIMARY KEY,
    task_transition_id TEXT NOT NULL REFERENCES task_transitions(id) ON DELETE CASCADE,
    workflow_edge_id TEXT REFERENCES workflow_edges(id) ON DELETE SET NULL,
    edge_key TEXT NOT NULL DEFAULT '',
    target_node_id TEXT REFERENCES workflow_nodes(id) ON DELETE SET NULL,
    target_node_key TEXT NOT NULL DEFAULT '',
    target_node_display_name TEXT NOT NULL DEFAULT '',
    target_node_kind TEXT NOT NULL DEFAULT '',
    target_placement_id TEXT REFERENCES task_node_placements(id) ON DELETE SET NULL,
    state TEXT NOT NULL CHECK (state IN ('pending', 'applied', 'completed', 'blocked')),
    context_mode TEXT NOT NULL DEFAULT '' CHECK (context_mode IN ('', 'new_session', 'continue_session', 'compact_and_continue_session')),
    requires_approval INTEGER NOT NULL DEFAULT 0 CHECK (requires_approval IN (0, 1)),
    input_bindings_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(input_bindings_json) AND json_type(input_bindings_json) = 'array'),
    output_requirements_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(output_requirements_json) AND json_type(output_requirements_json) = 'array'),
    metadata_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json))
);

INSERT INTO task_transition_edges_new (
    id,
    task_transition_id,
    workflow_edge_id,
    edge_key,
    target_node_id,
    target_node_key,
    target_node_display_name,
    target_node_kind,
    target_placement_id,
    state,
    context_mode,
    requires_approval,
    input_bindings_json,
    output_requirements_json,
    metadata_json
)
SELECT
    id,
    task_transition_id,
    workflow_edge_id,
    edge_key,
    target_node_id,
    target_node_key,
    target_node_display_name,
    target_node_kind,
    target_placement_id,
    state,
    context_mode,
    requires_approval,
    CASE
        WHEN json_valid(input_bindings_json) AND json_type(input_bindings_json) = 'array' THEN input_bindings_json
        ELSE '[]'
    END,
    CASE
        WHEN json_valid(output_requirements_json) AND json_type(output_requirements_json) = 'array' THEN output_requirements_json
        ELSE '[]'
    END,
    metadata_json
FROM task_transition_edges;

DROP TABLE task_transition_edges;
ALTER TABLE task_transition_edges_new RENAME TO task_transition_edges;

CREATE INDEX task_transition_edges_transition_state_idx
    ON task_transition_edges(task_transition_id, state);

CREATE INDEX task_transition_edges_workflow_edge_idx
    ON task_transition_edges(workflow_edge_id);

CREATE INDEX task_transition_edges_target_placement_idx
    ON task_transition_edges(target_placement_id);

CREATE VIEW task_transition_edge_records AS
SELECT
    te.id,
    te.task_transition_id,
    te.workflow_edge_id,
    te.edge_key,
    tt.workflow_revision_seen,
    te.target_node_id,
    te.target_node_key,
    te.target_node_display_name,
    te.target_node_kind,
    te.target_placement_id,
    te.state,
    te.context_mode,
    te.requires_approval,
    te.input_bindings_json,
    te.output_requirements_json,
    te.metadata_json
FROM task_transition_edges te
JOIN task_transitions tt ON tt.id = te.task_transition_id;

-- +goose StatementBegin
CREATE TRIGGER task_transition_edges_runtime_insert
BEFORE INSERT ON task_transition_edges
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1
    FROM task_transitions tt
    WHERE tt.id = NEW.task_transition_id
)
OR (
    NEW.target_placement_id IS NOT NULL
    AND trim(NEW.target_placement_id) != ''
    AND NOT EXISTS (
        SELECT 1
        FROM task_transitions tt
        JOIN task_node_placements p ON p.id = NEW.target_placement_id
        WHERE tt.id = NEW.task_transition_id
          AND p.task_id = tt.task_id
          AND (
              NEW.target_node_id IS NULL
              OR trim(NEW.target_node_id) = ''
              OR p.node_id = NEW.target_node_id
          )
    )
)
OR (
    NEW.target_node_id IS NOT NULL
    AND trim(NEW.target_node_id) != ''
    AND NOT EXISTS (
        SELECT 1
        FROM task_transitions tt
        JOIN task_records t ON t.id = tt.task_id
        JOIN workflow_nodes n ON n.id = NEW.target_node_id
        WHERE tt.id = NEW.task_transition_id
          AND n.workflow_id = t.workflow_id
    )
)
OR (
    NEW.workflow_edge_id IS NOT NULL
    AND trim(NEW.workflow_edge_id) != ''
    AND NOT EXISTS (
        SELECT 1
        FROM task_transitions tt
        JOIN task_records t ON t.id = tt.task_id
        JOIN workflow_edges e ON e.id = NEW.workflow_edge_id
        WHERE tt.id = NEW.task_transition_id
          AND e.workflow_id = t.workflow_id
    )
)
BEGIN
    SELECT RAISE(ABORT, 'task transition edge references must stay within one task workflow');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER task_transition_edges_runtime_update
BEFORE UPDATE OF task_transition_id, workflow_edge_id, target_node_id, target_placement_id ON task_transition_edges
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1
    FROM task_transitions tt
    WHERE tt.id = NEW.task_transition_id
)
OR (
    NEW.target_placement_id IS NOT NULL
    AND trim(NEW.target_placement_id) != ''
    AND NOT EXISTS (
        SELECT 1
        FROM task_transitions tt
        JOIN task_node_placements p ON p.id = NEW.target_placement_id
        WHERE tt.id = NEW.task_transition_id
          AND p.task_id = tt.task_id
          AND (
              NEW.target_node_id IS NULL
              OR trim(NEW.target_node_id) = ''
              OR p.node_id = NEW.target_node_id
          )
    )
)
OR (
    NEW.target_node_id IS NOT NULL
    AND trim(NEW.target_node_id) != ''
    AND NOT EXISTS (
        SELECT 1
        FROM task_transitions tt
        JOIN task_records t ON t.id = tt.task_id
        JOIN workflow_nodes n ON n.id = NEW.target_node_id
        WHERE tt.id = NEW.task_transition_id
          AND n.workflow_id = t.workflow_id
    )
)
OR (
    NEW.workflow_edge_id IS NOT NULL
    AND trim(NEW.workflow_edge_id) != ''
    AND NOT EXISTS (
        SELECT 1
        FROM task_transitions tt
        JOIN task_records t ON t.id = tt.task_id
        JOIN workflow_edges e ON e.id = NEW.workflow_edge_id
        WHERE tt.id = NEW.task_transition_id
          AND e.workflow_id = t.workflow_id
    )
)
BEGIN
    SELECT RAISE(ABORT, 'task transition edge references must stay within one task workflow');
END;
-- +goose StatementEnd

CREATE TEMP TABLE migration_transition_edge_fk_check_zero(value INTEGER NOT NULL CHECK (value = 0));

INSERT INTO migration_transition_edge_fk_check_zero(value)
SELECT 1
WHERE EXISTS (
    SELECT 1
    FROM pragma_foreign_key_check
);

DROP TABLE migration_transition_edge_fk_check_zero;
DROP TABLE migration_transition_edge_check_zero;

PRAGMA foreign_keys = ON;
