package metadata

import (
	"database/sql"
	"testing"

	"core/internal/testharness/databaseseed"
)

func openInMemoryMetadataTestStore(t *testing.T, persistenceRoot string) *Store {
	t.Helper()
	db := openLatestMetadataTestDatabase(t)
	store, err := NewStore(persistenceRoot, db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("backfill in-memory metadata project keys: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close in-memory metadata store: %v", err)
		}
	})
	return store
}

func openLatestMetadataTestDatabase(t testing.TB) *sql.DB {
	t.Helper()
	return databaseseed.OpenCurrentMetadataDatabase(t)
}

func metadataTestStoreOpener(t *testing.T, persistenceRoot string) func() *Store {
	t.Helper()
	open := databaseseed.CurrentMetadataDatabaseOpener(t)
	return func() *Store {
		store, err := NewStore(persistenceRoot, open())
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		return store
	}
}

func openEmptyMetadataTestDatabase(t testing.TB) *sql.DB {
	t.Helper()
	return databaseseed.OpenEmptyMetadataDatabase(t)
}

func TestAllMetadataMigrationsPreserveDatabaseIntegrity(t *testing.T) {
	migrated := openEmptyMetadataTestDatabase(t)
	t.Cleanup(func() { _ = migrated.Close() })
	if err := runMigrations(migrated); err != nil {
		t.Fatalf("run full metadata migration chain in memory: %v", err)
	}

	var integrity string
	if err := migrated.QueryRowContext(t.Context(), "PRAGMA integrity_check").Scan(&integrity); err != nil {
		t.Fatalf("migration integrity check: %v", err)
	}
	if integrity != "ok" {
		t.Fatalf("migration integrity check = %q, want ok", integrity)
	}
	rows, err := migrated.QueryContext(t.Context(), "PRAGMA foreign_key_check")
	if err != nil {
		t.Fatalf("migration foreign-key check: %v", err)
	}
	if rows.Next() {
		_ = rows.Close()
		t.Fatal("migration foreign-key check reported violations")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate migration foreign-key check: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close migration foreign-key check: %v", err)
	}
}
