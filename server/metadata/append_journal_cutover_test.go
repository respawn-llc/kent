package metadata

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"core/server/session"
)

func TestAppendJournalCutoverPreservesOnlyOldCommittedWork(t *testing.T) {
	for _, phase := range []string{"committed", "prepared", "invalid committed", "metadata only"} {
		t.Run(phase, func(t *testing.T) {
			store, cfg, binding := newMetadataTestStore(t)
			chat := createMetadataTestSession(t, store, cfg, binding)
			log, err := chat.MaterializeEventLog()
			if err != nil {
				t.Fatal(err)
			}
			content := "previous committed content"
			if _, _, err := log.AppendRecord(nil, session.MessageRecord{Role: session.MessageRoleUser, Content: &content}); err != nil {
				t.Fatal(err)
			}
			pre := chat.Meta()
			eventsPath := filepath.Join(chat.Dir(), "events.jsonl")
			prefix, err := os.ReadFile(eventsPath)
			if err != nil {
				t.Fatal(err)
			}
			content = "old journal content"
			if phase != "metadata only" {
				if _, _, err := log.AppendRecord(nil, session.MessageRecord{Role: session.MessageRoleAssistant, Content: &content}); err != nil {
					t.Fatal(err)
				}
			}
			post := chat.Meta()
			post.InputDraft = "authored draft committed by the old journal"
			full, err := os.ReadFile(eventsPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.ImportSessionSnapshot(t.Context(), session.PersistedStoreSnapshot{SessionDir: chat.Dir(), Meta: pre}); err != nil {
				t.Fatal(err)
			}
			journalPhase := phase
			if phase == "metadata only" {
				journalPhase = "committed"
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(full[len(prefix):]))
			if phase == "invalid committed" {
				journalPhase = "committed"
				digest = fmt.Sprintf("%x", sha256.Sum256([]byte("different content")))
			}
			journal := map[string]any{
				"version": 1,
				"phase":   journalPhase,
				"pre":     oldAppendJournalSnapshot(t, pre),
				"post":    oldAppendJournalSnapshot(t, post),
				"events": map[string]any{
					"start_offset": len(prefix), "end_offset": len(full),
					"event_count": 1, "first_sequence": pre.LastSequence + 1,
					"last_sequence": post.LastSequence, "sha256": digest,
				},
			}
			if phase == "metadata only" {
				delete(journal, "events")
			}
			encoded, err := json.Marshal(journal)
			if err != nil {
				t.Fatal(err)
			}
			journalPath := filepath.Join(chat.Dir(), "append-recovery.json")
			if err := os.WriteFile(journalPath, encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			migrationErr := migrateAppendJournals(t.Context(), store.db, cfg.PersistenceRoot)
			record, err := store.ResolvePersistedSession(t.Context(), pre.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(eventsPath)
			if err != nil {
				t.Fatal(err)
			}
			if phase == "invalid committed" {
				if migrationErr == nil || record.Meta.LastSequence != pre.LastSequence || !bytes.Equal(after, full) {
					t.Fatalf("invalid committed journal changed authoritative state: sequence=%d error=%v", record.Meta.LastSequence, migrationErr)
				}
				retained, err := os.ReadFile(journalPath)
				if err != nil || !bytes.Equal(retained, encoded) {
					t.Fatalf("invalid committed journal was not retained: %v", err)
				}
				return
			}
			if migrationErr != nil {
				t.Fatal(migrationErr)
			}
			wantSequence, wantFile := pre.LastSequence, prefix
			if phase == "committed" {
				wantSequence, wantFile = post.LastSequence, full
			}
			if record.Meta.LastSequence != wantSequence || !bytes.Equal(after, wantFile) {
				t.Fatalf("cutover state: sequence=%d, want %d; file preserved=%v", record.Meta.LastSequence, wantSequence, bytes.Equal(after, wantFile))
			}
			wantDraft := pre.InputDraft
			if journalPhase == "committed" {
				wantDraft = post.InputDraft
			}
			if record.Meta.InputDraft != wantDraft {
				t.Fatalf("authored draft=%q, want %q", record.Meta.InputDraft, wantDraft)
			}
			if _, err := os.Stat(journalPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("completed cutover retained journal: %v", err)
			}
			if phase == "committed" {
				// Model interruption after the database commit but before unlink.
				if err := os.WriteFile(journalPath, encoded, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := migrateAppendJournals(t.Context(), store.db, cfg.PersistenceRoot); err != nil {
					t.Fatal(err)
				}
				repeated, err := os.ReadFile(eventsPath)
				if err != nil || !bytes.Equal(repeated, full) {
					t.Fatalf("repeated cutover rewrote committed content: %v", err)
				}
			}
		})
	}
}

func oldAppendJournalSnapshot(t *testing.T, meta session.Meta) map[string]any {
	t.Helper()
	meta.CreatedAt = meta.CreatedAt.UTC().Truncate(time.Millisecond)
	meta.UpdatedAt = meta.UpdatedAt.UTC().Truncate(time.Millisecond)
	encoded, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"meta": meta, "sha256": fmt.Sprintf("%x", sha256.Sum256(encoded))}
}
