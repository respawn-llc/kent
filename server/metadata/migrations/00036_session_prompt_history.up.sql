-- +goose Up

CREATE TABLE session_prompt_history_entries (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    source TEXT NOT NULL CHECK (
        source IN (
            'submit_user_message',
            'submit_user_turn',
            'queue_user_message',
            'record_prompt_history',
            'run_prompt'
        )
    ),
    source_id TEXT NOT NULL CHECK (trim(source_id) <> ''),
    client_request_id TEXT NOT NULL DEFAULT '',
    queue_item_id TEXT NOT NULL DEFAULT '',
    queue_state TEXT NOT NULL DEFAULT '' CHECK (
        (
            source = 'queue_user_message'
            AND queue_item_id <> ''
            AND queue_state IN ('recorded', 'pending', 'consumed', 'discarded')
        )
        OR (
            source <> 'queue_user_message'
            AND queue_item_id = ''
            AND queue_state = ''
        )
    ),
    text TEXT NOT NULL CHECK (trim(text) <> ''),
    created_at_unix_ms INTEGER NOT NULL
);

CREATE UNIQUE INDEX session_prompt_history_entries_source_idx
    ON session_prompt_history_entries(session_id, source, source_id);

CREATE UNIQUE INDEX session_prompt_history_entries_client_request_idx
    ON session_prompt_history_entries(session_id, source, client_request_id)
    WHERE client_request_id <> '';

CREATE INDEX session_prompt_history_entries_session_sequence_idx
    ON session_prompt_history_entries(session_id, sequence);
