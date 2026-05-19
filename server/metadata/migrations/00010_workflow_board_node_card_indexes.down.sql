-- +goose Down

DROP INDEX IF EXISTS tasks_project_workflow_updated_idx;
