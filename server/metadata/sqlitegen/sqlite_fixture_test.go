package sqlitegen

import (
	"database/sql"
	"testing"
)

func openSQLiteFixture(t *testing.T) *sql.DB {
	t.Helper()
	if err := RegisterSQLiteExtensions(); err != nil {
		t.Fatalf("register metadata SQLite extensions: %v", err)
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

func createCurrentNodeStatusFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`
CREATE TABLE workflow_nodes (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL
);
CREATE TABLE task_current_nodes (
    task_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    scheduling_state TEXT
);`); err != nil {
		t.Fatal(err)
	}
}
