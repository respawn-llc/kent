package migrations

import (
	"database/sql"
	"fmt"

	"core/server/metadata/sqlitegen"

	"github.com/pressly/goose/v3"
)

func NewProvider(db *sql.DB, logger goose.Logger) (*goose.Provider, error) {
	if db == nil {
		return nil, fmt.Errorf("metadata migration database is required")
	}
	// SQLite extensions must be registered before the pool opens its first connection.
	if err := sqlitegen.RegisterSQLiteExtensions(); err != nil {
		return nil, err
	}
	if err := registerMetadataSQLiteFunctions(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = goose.NopLogger()
	}
	provider, err := goose.NewProvider(
		goose.DialectSQLite3,
		db,
		FS,
		goose.WithLogger(logger),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return nil, fmt.Errorf("create metadata migration provider: %w", err)
	}
	return provider, nil
}
