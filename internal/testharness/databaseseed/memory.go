package databaseseed

import (
	"database/sql"
	_ "embed"
	"fmt"
	"sync/atomic"
	"testing"

	"core/server/metadata/sqlitegen"
)

//go:embed latest_schema.sql
var CurrentMetadataSchema string

var memoryDatabaseSequence atomic.Uint64

func OpenEmptyMetadataDatabase(t testing.TB) *sql.DB {
	t.Helper()
	if err := sqlitegen.RegisterSQLiteExtensions(); err != nil {
		t.Fatal(err)
	}
	dsn := fmt.Sprintf("file:kent-metadata-test-%d?mode=memory&cache=shared&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(%d)", memoryDatabaseSequence.Add(1), sqlitegen.BusyTimeoutMilliseconds)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	return db
}

func OpenCurrentMetadataDatabase(t testing.TB) *sql.DB {
	t.Helper()
	db := OpenEmptyMetadataDatabase(t)
	if _, err := db.Exec(CurrentMetadataSchema); err != nil {
		_ = db.Close()
		t.Fatalf("initialize current metadata schema: %v", err)
	}
	db.SetMaxOpenConns(sqlitegen.ConnectionPoolSize)
	db.SetMaxIdleConns(sqlitegen.ConnectionPoolSize)
	return db
}
