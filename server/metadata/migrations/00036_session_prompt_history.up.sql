-- +goose Up

CREATE TABLE session_prompt_history_entries (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    source_id TEXT NOT NULL CHECK (trim(source_id) <> ''),
    text TEXT NOT NULL CHECK (trim(text) <> ''),
    created_at_unix_ms INTEGER NOT NULL
);

CREATE UNIQUE INDEX session_prompt_history_entries_source_idx
    ON session_prompt_history_entries(session_id, source_id);

CREATE INDEX session_prompt_history_entries_session_sequence_idx
    ON session_prompt_history_entries(session_id, sequence);
