-- +goose Up
ALTER TABLE sessions ADD COLUMN short_id INTEGER
    CHECK (short_id > 0 AND short_id <= 9007199254740991);

CREATE UNIQUE INDEX sessions_short_id ON sessions(short_id);

CREATE TABLE session_short_id_sequence (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    next_value INTEGER NOT NULL CHECK (next_value > 0 AND next_value <= 9007199254740992)
);

INSERT INTO session_short_id_sequence (singleton, next_value) VALUES (1, 1);
