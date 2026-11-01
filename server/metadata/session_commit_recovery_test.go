package metadata_test

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"testing"

	"core/internal/testharness/databaseseed"
	"core/server/metadata"
	"core/server/sessionstore"
	"core/shared/sessioncontract"

	"github.com/google/uuid"
)

type commitFault struct {
	err       error
	committed bool
}

type commitFaultDriver struct {
	driver driver.Driver
	fault  *commitFault
}

func (c commitFaultDriver) Open(dsn string) (driver.Conn, error) {
	conn, err := c.driver.Open(dsn)
	if err != nil {
		return nil, err
	}
	return &commitFaultConnection{Conn: conn, fault: c.fault}, nil
}

type commitFaultConnection struct {
	driver.Conn
	fault *commitFault
}

func (c *commitFaultConnection) Begin() (driver.Tx, error) {
	tx, err := c.Conn.Begin()
	if err != nil {
		return nil, err
	}
	return commitFaultTransaction{Tx: tx, fault: c.fault}, nil
}

type commitFaultTransaction struct {
	driver.Tx
	fault *commitFault
}

func (tx commitFaultTransaction) Commit() error {
	if tx.fault.err == nil {
		return tx.Tx.Commit()
	}
	err := tx.fault.err
	tx.fault.err = nil
	if tx.fault.committed {
		if commitErr := tx.Tx.Commit(); commitErr != nil {
			return commitErr
		}
	} else if rollbackErr := tx.Tx.Rollback(); rollbackErr != nil {
		return rollbackErr
	}
	return err
}

func TestSessionCommitFailureRecoversFromDatabaseOutcome(t *testing.T) {
	t.Setenv("KENT_DEBUG", "0")
	t.Setenv("KENT_INVARIANT_MODE", "diagnostic")
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "rolled back", true: "committed"}[committed], func(t *testing.T) {
			seed := databaseseed.OpenCurrentMetadataDatabase(t)
			t.Cleanup(func() { _ = seed.Close() })
			fault := &commitFault{}
			driverName := uuid.NewString()
			sql.Register(driverName, commitFaultDriver{driver: seed.Driver(), fault: fault})
			db, err := sql.Open(driverName, ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			if _, err := db.Exec(databaseseed.CurrentMetadataSchema); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("INSERT INTO session_short_id_sequence (singleton, next_value) VALUES (1, 1)"); err != nil {
				t.Fatal(err)
			}
			root, workspace := t.TempDir(), t.TempDir()
			store, err := metadata.NewStore(root, db)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			if _, err := store.RegisterWorkspaceBinding(t.Context(), workspace); err != nil {
				t.Fatal(err)
			}
			chat, err := session.Create(filepath.Join(root, "sessions"), "sessions", workspace,
				sessioncontract.SessionCategoryMain, store.AuthoritativeSessionStoreOptions()...)
			if err != nil {
				t.Fatal(err)
			}
			fault.err, fault.committed = errors.New("commit acknowledgement failed"), committed
			name := "changed"
			err = chat.SetName(&name)
			if (err == nil) != committed {
				t.Fatalf("committed=%v mutation error=%v", committed, err)
			}
			next := "next change"
			if err := chat.SetName(&next); err != nil {
				t.Fatalf("Session still unusable after recovered commit: %v", err)
			}
			record, err := store.ResolvePersistedSession(t.Context(), chat.Meta().SessionID)
			if err != nil || record.Meta.Name == nil || *record.Meta.Name != next {
				t.Fatalf("next mutation was not persisted: record=%+v err=%v", record, err)
			}
			log, err := chat.MaterializeEventLog()
			if err != nil {
				t.Fatal(err)
			}
			fault.err, fault.committed = errors.New("append commit acknowledgement failed"), committed
			content := "first result"
			_, receipt, err := log.AppendRecord(nil, session.MessageRecord{Role: session.MessageRoleAssistant, Content: &content})
			if receipt.Committed != committed || (err == nil) != committed {
				t.Fatalf("append committed=%v receipt=%+v error=%v", committed, receipt, err)
			}
			content = "next result"
			if _, receipt, err := log.AppendRecord(nil, session.MessageRecord{Role: session.MessageRoleAssistant, Content: &content}); err != nil || !receipt.Committed {
				t.Fatalf("next append failed: receipt=%+v error=%v", receipt, err)
			}
			wantSequence := int64(1)
			if committed {
				wantSequence++
			}
			if chat.Meta().LastSequence != wantSequence {
				t.Fatalf("sequence=%d, want %d without replay", chat.Meta().LastSequence, wantSequence)
			}
		})
	}
}
