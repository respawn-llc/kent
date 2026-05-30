SELECT
    id,
    name,
    description,
    version,
    created_at_unix_ms,
    updated_at_unix_ms
FROM workflows
{{clause}}
ORDER BY lower(name) ASC, id ASC
LIMIT ? OFFSET ?
