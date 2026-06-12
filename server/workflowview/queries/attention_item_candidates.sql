WITH attention_candidates AS (
    SELECT
        'approval' AS kind,
        'approval:' || tt.id AS id,
        t.project_id,
        t.workflow_id,
        t.id AS task_id,
        t.short_id,
        t.title,
        '' AS run_id,
        '' AS session_id,
        '' AS ask_id,
        tt.id AS task_transition_id,
        '' AS interruption_reason,
        '' AS interruption_detail_json,
        tt.created_at_unix_ms AS occurred_at_unix_ms
    FROM task_transitions tt
    JOIN task_records t ON t.id = tt.task_id
    WHERE tt.state = 'pending_approval'
      AND t.canceled_at_unix_ms = 0
      AND (?1 = '' OR t.project_id = ?1)
      AND (?2 = '' OR t.id = ?2)
    UNION ALL
    SELECT
        'question' AS kind,
        'question:' || r.id || ':' || r.waiting_ask_id AS id,
        t.project_id,
        t.workflow_id,
        t.id AS task_id,
        t.short_id,
        t.title,
        r.id AS run_id,
        COALESCE(r.session_id, '') AS session_id,
        r.waiting_ask_id AS ask_id,
        '' AS task_transition_id,
        '' AS interruption_reason,
        '' AS interruption_detail_json,
        r.updated_at_unix_ms AS occurred_at_unix_ms
    FROM task_run_records r
    JOIN task_records t ON t.id = r.task_id
    WHERE trim(r.waiting_ask_id) != ''
      AND r.completed_at_unix_ms = 0
      AND r.interrupted_at_unix_ms = 0
      AND t.canceled_at_unix_ms = 0
      AND (?1 = '' OR t.project_id = ?1)
      AND (?2 = '' OR t.id = ?2)
    UNION ALL
    SELECT
        'interrupted_run' AS kind,
        'interrupted_run:' || r.id AS id,
        t.project_id,
        t.workflow_id,
        t.id AS task_id,
        t.short_id,
        t.title,
        r.id AS run_id,
        COALESCE(r.session_id, '') AS session_id,
        '' AS ask_id,
        '' AS task_transition_id,
        r.interruption_reason,
        r.interruption_detail_json,
        r.interrupted_at_unix_ms AS occurred_at_unix_ms
    FROM task_run_records r
    JOIN task_records t ON t.id = r.task_id
    JOIN task_node_placements p ON p.id = r.placement_id
    WHERE r.interrupted_at_unix_ms > 0
      AND r.completed_at_unix_ms = 0
      AND p.state IN ('active', 'waiting_approval')
      AND t.canceled_at_unix_ms = 0
      AND (?1 = '' OR t.project_id = ?1)
      AND (?2 = '' OR t.id = ?2)
    UNION ALL
    SELECT
        'validation_blocker' AS kind,
        'validation_blocker:' || project_id || ':' || workflow_id AS id,
        project_id,
        workflow_id,
        '' AS task_id,
        '' AS short_id,
        '' AS title,
        '' AS run_id,
        '' AS session_id,
        '' AS ask_id,
        '' AS task_transition_id,
        '' AS interruption_reason,
        '' AS interruption_detail_json,
        updated_at_unix_ms AS occurred_at_unix_ms
    FROM project_workflow_links
    WHERE (?1 = '' OR project_id = ?1)
      AND ?2 = ''
)
SELECT
    kind,
    id,
    project_id,
    workflow_id,
    task_id,
    short_id,
    title,
    run_id,
    session_id,
    ask_id,
    task_transition_id,
    interruption_reason,
    interruption_detail_json,
    occurred_at_unix_ms
FROM attention_candidates
WHERE CAST(?3 AS INTEGER) = 0
   OR occurred_at_unix_ms < ?4
   OR (
       occurred_at_unix_ms = ?4
       AND id < ?5
   )
ORDER BY occurred_at_unix_ms DESC, id DESC
LIMIT ?6
