-- +goose Up
DROP INDEX IF EXISTS runtime_leases_session_idx;
DROP INDEX IF EXISTS workspaces_project_idx;
DROP INDEX IF EXISTS workflow_transition_groups_source_transition_idx;
DROP INDEX IF EXISTS tasks_project_short_id_idx;

UPDATE workflow_nodes
SET metadata_json = json_remove(metadata_json, '$.archived_at_unix_ms')
WHERE json_valid(metadata_json) = 1
  AND json_type(metadata_json, '$.archived_at_unix_ms') IS NOT NULL;
