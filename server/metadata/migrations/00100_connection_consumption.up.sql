-- +goose Up
CREATE TABLE connection_consumption (
    connection_id TEXT NOT NULL CHECK (length(connection_id) > 0),
    hour_start_unix INTEGER NOT NULL,
    score INTEGER NOT NULL CHECK (typeof(score) = 'integer' AND score >= 0),
    PRIMARY KEY (connection_id, hour_start_unix)
);
