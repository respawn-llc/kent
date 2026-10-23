-- +goose Up
-- +goose NO TRANSACTION

PRAGMA legacy_alter_table = ON;
PRAGMA foreign_keys = OFF;

BEGIN IMMEDIATE;

CREATE TABLE workflow_node_groups_new (
    id TEXT PRIMARY KEY,
    workflow_id BLOB NOT NULL REFERENCES workflows(id) ON DELETE CASCADE
        CHECK (typeof(workflow_id) = 'blob' AND length(workflow_id) = 16),
    group_key TEXT NOT NULL CHECK (length(group_key) BETWEEN 1 AND 64),
    display_name TEXT CHECK (display_name IS NULL OR length(trim(display_name)) BETWEEN 1 AND 120),
    sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
    UNIQUE (workflow_id, id),
    UNIQUE (workflow_id, group_key)
);

INSERT INTO workflow_node_groups_new (rowid, id, workflow_id, group_key, display_name, sort_order)
SELECT rowid, id, workflow_id, group_key, display_name, sort_order
FROM workflow_node_groups ORDER BY rowid;

DROP TABLE workflow_node_groups;
ALTER TABLE workflow_node_groups_new RENAME TO workflow_node_groups;

CREATE INDEX workflow_node_groups_workflow_sort_idx
    ON workflow_node_groups(workflow_id, sort_order);

CREATE INDEX workflow_nodes_group_sort_idx
    ON workflow_nodes(group_id, sort_order);

PRAGMA legacy_alter_table = OFF;

CREATE TEMP TABLE migration_optional_group_names_fk_check_zero (
    value INTEGER NOT NULL CHECK (value = 0)
);
INSERT INTO migration_optional_group_names_fk_check_zero(value)
SELECT 1 WHERE EXISTS (SELECT 1 FROM pragma_foreign_key_check);
DROP TABLE migration_optional_group_names_fk_check_zero;

COMMIT;
PRAGMA foreign_keys = ON;
PRAGMA legacy_alter_table = OFF;
