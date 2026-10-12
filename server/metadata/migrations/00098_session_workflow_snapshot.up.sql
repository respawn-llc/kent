-- +goose Up

UPDATE sessions
SET metadata_json = CASE
        WHEN json_type(locked_json, '$.workflow_completion_mode') IS NOT NULL
         AND json_type(locked_json, '$.workflow_completion_mode') != 'null'
        THEN json_set(
            metadata_json,
            '$.workflow_snapshot',
            json_object('completion_mode', json_extract(locked_json, '$.workflow_completion_mode'))
        )
        ELSE metadata_json
    END,
    locked_json = json_remove(locked_json, '$.workflow_completion_mode')
WHERE json_type(locked_json, '$.workflow_completion_mode') IS NOT NULL;
