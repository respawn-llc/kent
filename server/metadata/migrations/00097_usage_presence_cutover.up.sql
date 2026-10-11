-- +goose Up

-- Historical context baselines were input-measured. Preserve that baseline
-- beside its existing estimate anchor, without inventing completed-response
-- context measurements from billing output.

UPDATE sessions
SET usage_state_json = json_set(
    json_remove(usage_state_json, '$.has_cached_input_tokens'),
    '$.input_tokens', nullif(json_extract(usage_state_json, '$.input_tokens'), 0),
    '$.output_tokens', nullif(json_extract(usage_state_json, '$.output_tokens'), 0),
    '$.cached_input_tokens', nullif(json_extract(usage_state_json, '$.cached_input_tokens'), 0),
    '$.reported_context_tokens', nullif(json_extract(usage_state_json, '$.input_tokens'), 0)
)
WHERE usage_state_json IS NOT NULL
  AND json_type(usage_state_json) = 'object';
