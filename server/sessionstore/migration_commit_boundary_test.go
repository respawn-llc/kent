package session

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type conversionBoundaryPersistence struct {
	*testSessionMetadata
	beforeCommit error
	afterCommit  func() error
}

func (p conversionBoundaryPersistence) ObserveEventLogReconciliation(ctx context.Context, observation PersistedEventLogReconciliation) error {
	if p.beforeCommit != nil {
		return p.beforeCommit
	}
	if err := p.testSessionMetadata.ObserveEventLogReconciliation(ctx, observation); err != nil {
		return err
	}
	if p.afterCommit != nil {
		return p.afterCommit()
	}
	return nil
}

func TestLegacyConversionKeepsCanonicalInputUntilDatabaseCommit(t *testing.T) {
	for _, failure := range []string{"database", "publication"} {
		t.Run(failure, func(t *testing.T) {
			persistence := &testSessionMetadata{records: map[string]PersistedSessionRecord{}}
			gate := conversionBoundaryPersistence{testSessionMetadata: persistence}
			store, err := Create(t.TempDir(), "workspace", t.TempDir(), testSessionCategory, persistence.options()...)
			if err != nil {
				t.Fatal(err)
			}
			var legacy []legacyTestEvent
			for i := 0; i < 10; i++ {
				legacy = append(legacy, legacyTestEvent{
					Seq: int64(i%5 + 1), Timestamp: time.Now().UTC(),
					Kind:    string(EventKindMessage),
					Payload: mustFixtureJSON(t, map[string]any{"role": MessageRoleUser, "content": "committed legacy input"}),
				})
			}
			writeSessionFixtureEvents(t, store.Dir(), legacy)
			record, err := persistence.ResolvePersistedSession(t.Context(), store.Meta().SessionID)
			if err != nil {
				t.Fatal(err)
			}
			record.Meta.LastSequence = 5
			persistence.sharedStore().Put(record.Meta.SessionID, record)
			path := filepath.Join(store.Dir(), eventsFile)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if failure == "database" {
				gate.beforeCommit = errors.New("conversion database commit rejected")
			} else {
				movedStage := filepath.Join(t.TempDir(), "uninstalled-stage")
				gate.afterCommit = func() error {
					// Make the real atomic publication fail after its source has
					// been validated, leaving the canonical v0 input untouched.
					return os.Rename(filepath.Join(eventLogMigrationWorkspacePath(store.Dir()), eventLogMigrationStagedLogFile), movedStage)
				}
			}
			failing, err := Open(store.Dir(), WithPersistenceObserver(gate), WithPersistedSessionResolver(persistence))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := failing.MaterializeEventLog(); err == nil {
				t.Fatal("failed conversion reported success")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("failed conversion changed canonical v0 input: %v", err)
			}
			record, err = persistence.ResolvePersistedSession(t.Context(), store.Meta().SessionID)
			if err != nil {
				t.Fatal(err)
			}
			wantSequence := int64(5)
			if failure == "publication" {
				wantSequence = 10
			}
			if record.Meta.LastSequence != wantSequence {
				t.Fatalf("database boundary=%d, want %d", record.Meta.LastSequence, wantSequence)
			}
			reopened, err := Open(store.Dir(), persistence.options()...)
			if err != nil {
				t.Fatal(err)
			}
			log := mustMaterializeSessionTestEventLog(t, reopened)
			window, err := log.ReadRecentRecords(20)
			if err != nil {
				t.Fatal(err)
			}
			if len(window.Records) != 10 || reopened.Meta().LastSequence != 10 {
				t.Fatalf("ordinary conversion lost committed v0 input: records=%d sequence=%d", len(window.Records), reopened.Meta().LastSequence)
			}
			assertEventLogVersion(t, path, EventLogVersionV1)
		})
	}
}
