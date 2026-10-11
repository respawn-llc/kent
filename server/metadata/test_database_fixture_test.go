package metadata

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"testing"

	"core/internal/testharness/databaseseed"
)

var latestMetadataTestSchema = []byte(databaseseed.CurrentMetadataSchema)

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
	db := databaseseed.OpenCurrentMetadataDatabase(t)
	return db
}

func openEmptyMetadataTestDatabase(t testing.TB) *sql.DB {
	t.Helper()
	return databaseseed.OpenEmptyMetadataDatabase(t)
}

func TestAllMetadataMigrationsMatchLatestInMemorySchema(t *testing.T) {
	migrated := openEmptyMetadataTestDatabase(t)
	t.Cleanup(func() { _ = migrated.Close() })
	if err := runMigrations(migrated); err != nil {
		t.Fatalf("run full metadata migration chain in memory: %v", err)
	}

	migratedSchema := metadataTestSchema(t, migrated)
	if !bytes.Equal(migratedSchema, latestMetadataTestSchema) {
		t.Fatalf(
			"full migration schema does not match the shared schema fixture; regenerate with just dump metadata-schema\nwant_sha256=%x\ngot_sha256=%x",
			sha256Bytes(latestMetadataTestSchema),
			sha256Bytes(migratedSchema),
		)
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
	if err := rows.Close(); err != nil {
		t.Fatalf("close migration foreign-key check: %v", err)
	}
}

func metadataTestSchema(t testing.TB, db *sql.DB) []byte {
	t.Helper()
	rows, err := db.Query(`
SELECT type, name, CAST(sql AS TEXT)
FROM sqlite_schema
WHERE sql IS NOT NULL
  AND name != 'sqlite_sequence'
  AND name NOT IN (SELECT name FROM pragma_table_list WHERE type = 'shadow')
ORDER BY
  CASE type
    WHEN 'table' THEN 0
    WHEN 'view' THEN 1
    WHEN 'index' THEN 2
    WHEN 'trigger' THEN 3
  END,
  name`)
	if err != nil {
		t.Fatalf("query metadata test schema: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var schema bytes.Buffer
	for rows.Next() {
		var kind, name, ddl string
		if err := rows.Scan(&kind, &name, &ddl); err != nil {
			t.Fatalf("scan metadata test schema: %v", err)
		}
		if schema.Len() > 0 {
			schema.WriteByte('\n')
		}
		schema.WriteString(ddl)
		schema.WriteString(";\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate metadata test schema: %v", err)
	}
	return schema.Bytes()
}

func sha256Bytes(value []byte) [32]byte {
	return sha256.Sum256(value)
}
