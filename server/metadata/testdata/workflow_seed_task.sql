INSERT INTO tasks (id, project_workflow_link_id, workflow_revision_seen, task_seq, short_id, title, body, created_at_unix_ms, updated_at_unix_ms, metadata_json)
VALUES (?, ?, 1, ?, ?, 'Task', 'Body', ?, ?, '{}');
