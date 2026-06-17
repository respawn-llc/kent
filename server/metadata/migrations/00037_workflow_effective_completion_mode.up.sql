-- +goose Up
-- +goose NO TRANSACTION

PRAGMA legacy_alter_table = ON;
PRAGMA foreign_keys = OFF;

DROP VIEW IF EXISTS task_run_records;

CREATE TABLE task_runs_new (
    id TEXT PRIMARY KEY,
    placement_id TEXT NOT NULL REFERENCES task_node_placements(id) ON DELETE CASCADE,
    session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    run_generation INTEGER NOT NULL DEFAULT 0 CHECK (run_generation >= 0),
    workflow_revision_seen INTEGER NOT NULL CHECK (workflow_revision_seen >= 1),
    automation_requested_at_unix_ms INTEGER NOT NULL DEFAULT 0 CHECK (automation_requested_at_unix_ms >= 0),
    created_at_unix_ms INTEGER NOT NULL CHECK (created_at_unix_ms >= 0),
    updated_at_unix_ms INTEGER NOT NULL CHECK (updated_at_unix_ms >= 0),
    started_at_unix_ms INTEGER NOT NULL DEFAULT 0 CHECK (started_at_unix_ms >= 0),
    completed_at_unix_ms INTEGER NOT NULL DEFAULT 0 CHECK (completed_at_unix_ms >= 0),
    interrupted_at_unix_ms INTEGER NOT NULL DEFAULT 0 CHECK (interrupted_at_unix_ms >= 0),
    interruption_reason TEXT NOT NULL DEFAULT '',
    interruption_detail_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(interruption_detail_json)),
    waiting_ask_id TEXT NOT NULL DEFAULT '',
    effective_completion_mode TEXT NOT NULL DEFAULT '' CHECK (effective_completion_mode IN ('', 'structured_output', 'tool', 'shell_command', 'unstructured_output')),
    invalid_completion_count INTEGER NOT NULL DEFAULT 0 CHECK (invalid_completion_count >= 0),
    run_start_snapshot_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(run_start_snapshot_json)),
    metadata_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json))
);

INSERT INTO task_runs_new (
    id,
    placement_id,
    session_id,
    run_generation,
    workflow_revision_seen,
    automation_requested_at_unix_ms,
    created_at_unix_ms,
    updated_at_unix_ms,
    started_at_unix_ms,
    completed_at_unix_ms,
    interrupted_at_unix_ms,
    interruption_reason,
    interruption_detail_json,
    waiting_ask_id,
    effective_completion_mode,
    invalid_completion_count,
    run_start_snapshot_json,
    metadata_json
)
SELECT
    id,
    placement_id,
    session_id,
    run_generation,
    workflow_revision_seen,
    automation_requested_at_unix_ms,
    created_at_unix_ms,
    updated_at_unix_ms,
    started_at_unix_ms,
    completed_at_unix_ms,
    interrupted_at_unix_ms,
    interruption_reason,
    interruption_detail_json,
    waiting_ask_id,
    '' AS effective_completion_mode,
    invalid_completion_count,
    run_start_snapshot_json,
    metadata_json
FROM task_runs;

DROP TABLE task_runs;
ALTER TABLE task_runs_new RENAME TO task_runs;

CREATE INDEX task_runs_placement_idx
    ON task_runs(placement_id);

CREATE INDEX task_runs_session_idx
    ON task_runs(session_id);

CREATE INDEX task_runs_runnable_idx
    ON task_runs(automation_requested_at_unix_ms, id)
    WHERE automation_requested_at_unix_ms > 0 AND completed_at_unix_ms = 0 AND interrupted_at_unix_ms = 0;

CREATE INDEX task_runs_outcome_idx
    ON task_runs(started_at_unix_ms, completed_at_unix_ms, interrupted_at_unix_ms);

CREATE INDEX task_runs_placement_created_idx
    ON task_runs(placement_id, created_at_unix_ms DESC);

CREATE VIEW task_run_records AS
SELECT
    r.id,
    p.task_id,
    r.placement_id,
    p.node_id,
    r.session_id,
    r.run_generation,
    r.workflow_revision_seen,
    r.automation_requested_at_unix_ms,
    r.created_at_unix_ms,
    r.updated_at_unix_ms,
    r.started_at_unix_ms,
    r.completed_at_unix_ms,
    r.interrupted_at_unix_ms,
    r.interruption_reason,
    r.interruption_detail_json,
    r.waiting_ask_id,
    r.effective_completion_mode,
    r.invalid_completion_count,
    r.run_start_snapshot_json,
    r.metadata_json
FROM task_runs r
JOIN task_node_placements p ON p.id = r.placement_id;

PRAGMA foreign_keys = ON;
PRAGMA legacy_alter_table = OFF;
