-- +goose Up
-- The metadata migration owner preserves old committed journals before
-- recording this completion marker.
SELECT 1;
