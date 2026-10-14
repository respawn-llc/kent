package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var metadataMigrationSnapshots = struct {
	sync.Mutex
	byVersion map[int64][]byte
}{byVersion: make(map[int64][]byte)}

type metadataSnapshotDriver interface {
	Serialize() ([]byte, error)
	Deserialize([]byte) error
}

func emptyMetadataSnapshotAtVersion(t *testing.T, version int64) ([]byte, error) {
	t.Helper()
	metadataMigrationSnapshots.Lock()
	defer metadataMigrationSnapshots.Unlock()
	if snapshot, ok := metadataMigrationSnapshots.byVersion[version]; ok {
		return snapshot, nil
	}

	db := openEmptyMetadataTestDatabase(t)
	provider, err := newMetadataMigrationProvider(db)
	if err == nil {
		_, err = provider.UpTo(t.Context(), version)
	}
	var snapshot []byte
	if err == nil {
		err = withMetadataSnapshotConnection(t.Context(), db, func(conn metadataSnapshotDriver) error {
			snapshot, err = conn.Serialize()
			return err
		})
	}
	if err = errors.Join(err, db.Close()); err != nil {
		return nil, err
	}
	// Only empty migration preconditions are cached. Deserialize copies these bytes
	// into each fixture; seeded facts and target migrations remain independent.
	metadataMigrationSnapshots.byVersion[version] = snapshot
	return snapshot, nil
}

func withMetadataSnapshotConnection(ctx context.Context, db *sql.DB, action func(metadataSnapshotDriver) error) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	err = conn.Raw(func(driverConn any) error {
		snapshotConn, ok := driverConn.(metadataSnapshotDriver)
		if !ok {
			return fmt.Errorf("metadata fixture driver %T does not support SQLite snapshots", driverConn)
		}
		return action(snapshotConn)
	})
	// The one-connection fixture pool retains this connection when it becomes idle:
	// deserialized databases are private to the native connection, not shared-cache peers.
	return errors.Join(err, conn.Close())
}

func TestMigrationSnapshotsKeepFixturesIndependent(t *testing.T) {
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db, err := openDatabaseAtVersionForTest(t, 93)
			if err != nil {
				t.Fatal(err)
			}
			execSeed(t, db, "independent snapshot project", `
INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms)
VALUES ('snapshot-project', ?, 1000, 1000)`, name)
			var got string
			if err := db.QueryRowContext(t.Context(), `
SELECT display_name FROM projects WHERE id = 'snapshot-project'`).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != name {
				t.Fatalf("fixture project = %q, want %q", got, name)
			}
			_, err = db.ExecContext(t.Context(), `
INSERT INTO sessions (id, project_id, artifact_relpath, created_at_unix_ms, updated_at_unix_ms)
VALUES ('snapshot-session', 'missing-project', 'sessions/snapshot-session', 1000, 1000)`)
			var constraint *sqlite.Error
			if !errors.As(err, &constraint) || constraint.Code() != sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY {
				t.Fatalf("snapshot foreign-key enforcement error = %v", err)
			}
		})
	}
}
