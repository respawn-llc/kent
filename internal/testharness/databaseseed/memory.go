package databaseseed

import (
	"database/sql"
	_ "embed"
	"fmt"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"

	"core/server/metadata/sqlitegen"
)

//go:embed latest_schema.sql
var CurrentMetadataSchema string

var memoryDatabaseSequence atomic.Uint64

func OpenEmptyMetadataDatabase(t testing.TB) *sql.DB {
	t.Helper()
	return openMemoryDatabase(t, memoryDatabaseSequence.Add(1))
}

func openMemoryDatabase(t testing.TB, identity uint64) *sql.DB {
	t.Helper()
	if err := sqlitegen.RegisterSQLiteExtensions(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", (&url.URL{
		Scheme: "file",
		Opaque: fmt.Sprintf("kent-metadata-test-%d", identity),
		RawQuery: url.Values{
			"mode":  {"memory"},
			"cache": {"shared"},
			"_pragma": {"foreign_keys(1)", "synchronous(NORMAL)",
				"busy_timeout(" + strconv.Itoa(sqlitegen.BusyTimeoutMilliseconds) + ")"},
		}.Encode(),
	}).String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	return db
}

func OpenCurrentMetadataDatabase(t testing.TB) *sql.DB {
	t.Helper()
	db := OpenEmptyMetadataDatabase(t)
	initializeCurrentMetadataDatabase(t, db)
	return db
}

// CurrentMetadataDatabaseOpener retains a keeper so independently owned pools
// can close and reopen the same in-memory database during a test.
func CurrentMetadataDatabaseOpener(t testing.TB) func() *sql.DB {
	t.Helper()
	identity := memoryDatabaseSequence.Add(1)
	keeper := openMemoryDatabase(t, identity)
	initializeCurrentMetadataDatabase(t, keeper)
	t.Cleanup(func() {
		if err := keeper.Close(); err != nil {
			t.Errorf("close memory database keeper: %v", err)
		}
	})
	return func() *sql.DB {
		db := openMemoryDatabase(t, identity)
		db.SetMaxOpenConns(sqlitegen.ConnectionPoolSize)
		db.SetMaxIdleConns(sqlitegen.ConnectionPoolSize)
		return db
	}
}

func initializeCurrentMetadataDatabase(t testing.TB, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(CurrentMetadataSchema); err != nil {
		_ = db.Close()
		t.Fatalf("initialize current metadata schema: %v", err)
	}
	if _, err := db.Exec("INSERT INTO session_short_id_sequence (singleton, next_value) VALUES (1, 1)"); err != nil {
		_ = db.Close()
		t.Fatalf("initialize Session short ID sequence: %v", err)
	}
	db.SetMaxOpenConns(sqlitegen.ConnectionPoolSize)
	db.SetMaxIdleConns(sqlitegen.ConnectionPoolSize)
}
