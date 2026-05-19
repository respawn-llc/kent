-- +goose Up

CREATE INDEX tasks_project_workflow_updated_idx
    ON tasks(project_id, workflow_id, updated_at_unix_ms DESC, id DESC);
