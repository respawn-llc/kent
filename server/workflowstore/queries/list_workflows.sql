WITH workflow_list AS (
    SELECT
        id,
        name,
        description,
        version,
        created_at_unix_ms,
        updated_at_unix_ms,
        MAX(
            updated_at_unix_ms,
            COALESCE((
                SELECT MAX(task_records.updated_at_unix_ms)
                FROM task_records
                WHERE task_records.workflow_id = workflows.id
            ), 0)
        ) AS activity_at_unix_ms
    FROM workflows
    {{clause}}
)
SELECT
    id,
    name,
    description,
    version,
    created_at_unix_ms,
    updated_at_unix_ms,
    activity_at_unix_ms
FROM workflow_list
{{cursor_clause}}
ORDER BY activity_at_unix_ms DESC, id DESC
LIMIT ?
