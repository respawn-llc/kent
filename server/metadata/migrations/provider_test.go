package migrations_test

import (
	"database/sql"
	"testing"

	"core/server/metadata/migrations"

	_ "modernc.org/sqlite"
)

func TestProviderMigratesIndependentInMemoryDatabases(t *testing.T) {
	for range 2 {
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		t.Cleanup(func() {
			if err := db.Close(); err != nil {
				t.Errorf("close migration database: %v", err)
			}
		})
		provider, err := migrations.NewProvider(db, nil)
		if err != nil {
			t.Fatalf("create self-contained migration provider: %v", err)
		}
		if _, err := provider.Up(t.Context()); err != nil {
			t.Fatalf("migrate independent database: %v", err)
		}
		var integrity string
		if err := db.QueryRowContext(t.Context(), "PRAGMA integrity_check").Scan(&integrity); err != nil {
			t.Fatal(err)
		}
		if integrity != "ok" {
			t.Fatalf("database integrity = %q", integrity)
		}
	}
}
