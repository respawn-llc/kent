SELECT activity_id, kind, source_id, occurred_at_unix_ms, updated_at_unix_ms, actor
FROM (
    SELECT
        'comment:' || c.id AS activity_id,
        'comment' AS kind,
        c.id AS source_id,
        c.updated_at_unix_ms AS occurred_at_unix_ms,
        c.updated_at_unix_ms AS updated_at_unix_ms,
        c.author_kind AS actor
    FROM task_comments c
    WHERE c.task_id = ?

    UNION ALL

    SELECT
        'transition:' || tt.id AS activity_id,
        'transition' AS kind,
        tt.id AS source_id,
        tt.created_at_unix_ms AS occurred_at_unix_ms,
        tt.applied_at_unix_ms AS updated_at_unix_ms,
        tt.actor AS actor
    FROM task_transitions tt
    WHERE tt.task_id = ?

    UNION ALL

    SELECT
        'run_started:' || r.id AS activity_id,
        'run_started' AS kind,
        r.id AS source_id,
        r.started_at_unix_ms AS occurred_at_unix_ms,
        r.updated_at_unix_ms AS updated_at_unix_ms,
        '' AS actor
    FROM task_run_records r
    WHERE r.task_id = ?
      AND r.started_at_unix_ms > 0

    UNION ALL

    SELECT
        'run_completed:' || r.id AS activity_id,
        'run_completed' AS kind,
        r.id AS source_id,
        r.completed_at_unix_ms AS occurred_at_unix_ms,
        r.updated_at_unix_ms AS updated_at_unix_ms,
        '' AS actor
    FROM task_run_records r
    WHERE r.task_id = ?
      AND r.completed_at_unix_ms > 0

    UNION ALL

    SELECT
        'run_interrupted:' || r.id AS activity_id,
        'run_interrupted' AS kind,
        r.id AS source_id,
        r.interrupted_at_unix_ms AS occurred_at_unix_ms,
        r.updated_at_unix_ms AS updated_at_unix_ms,
        '' AS actor
    FROM task_run_records r
    WHERE r.task_id = ?
      AND r.interrupted_at_unix_ms > 0

    UNION ALL

    SELECT
        'task_canceled:' || t.id AS activity_id,
        'task_canceled' AS kind,
        t.id AS source_id,
        t.canceled_at_unix_ms AS occurred_at_unix_ms,
        t.updated_at_unix_ms AS updated_at_unix_ms,
        '' AS actor
    FROM task_records t
    WHERE t.id = ?
      AND t.canceled_at_unix_ms > 0
) activity
WHERE (? = 0 OR occurred_at_unix_ms < ? OR (occurred_at_unix_ms = ? AND activity_id < ?))
ORDER BY occurred_at_unix_ms DESC, activity_id DESC
LIMIT ?
