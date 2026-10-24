package metadata

import (
	"context"
	"database/sql"
	"fmt"

	"core/server/metadata/sqlitegen"
	"core/server/sessionstore"
)

func migrateAppendJournals(ctx context.Context, db *sql.DB, persistenceRoot string) error {
	// Borrow the migration owner's connection and the existing snapshot mapping;
	// do not construct/close a second database owner or run Store startup work.
	store := &Store{persistenceRoot: persistenceRoot, db: db, queries: sqlitegen.New(db)}
	var afterID sql.NullString
	for {
		ids, err := store.queries.ListAppendJournalCutoverSessionIDs(ctx, afterID)
		if err != nil {
			return fmt.Errorf("page append-journal Sessions: %w", err)
		}
		if len(ids) == 0 {
			return nil
		}
		for _, id := range ids {
			record, err := store.ResolvePersistedSession(ctx, id)
			if err != nil {
				return err
			}
			if err := session.MigrateAppendJournal(record, func(snapshot session.PersistedStoreSnapshot) error {
				return store.upsertSessionSnapshot(ctx, snapshot)
			}); err != nil {
				return fmt.Errorf("migrate Session %q append journal: %w", id, err)
			}
			afterID = sql.NullString{String: id, Valid: true}
		}
	}
}
