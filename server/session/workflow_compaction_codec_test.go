package session

import (
	"bytes"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWorkflowCompactionCodecSupportedVersions(t *testing.T) {
	for _, version := range []int{EventLogVersionV1, EventLogVersionV2} {
		for _, engine := range []CompactionEngine{CompactionEngineLocal, CompactionEngineRemote} {
			t.Run(eventLogVersionTestName(version)+"/"+string(engine), func(t *testing.T) {
				raw := []byte(`{ "type": "compaction", "encrypted_content": "unchanged-checkpoint" }`)
				item := ProviderHistoryItem{Type: ProviderHistoryItemTypeCompaction, Raw: raw}
				if engine == CompactionEngineLocal {
					raw = []byte(`{ "type": "message", "role": "user", "content": "summary" }`)
					item = ProviderHistoryItem{Type: ProviderHistoryItemTypeMessage, Raw: raw}
				}
				record, err := NewEventRecord(1, nil, WorkflowCompactionRecord{
					Engine: engine, CompactionNumber: 3, CommittedEntryStart: 19,
					Summary: []ProviderHistoryItem{item},
				})
				if err != nil {
					t.Fatal(err)
				}
				line, err := encodeEventRecordForVersion(version, record)
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := decodeEventRecordForVersion(version, line)
				if err != nil {
					t.Fatal(err)
				}
				got, err := decoded.Payload()
				if err != nil {
					t.Fatal(err)
				}
				want, err := record.Payload()
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) || !bytes.Equal(got.(WorkflowCompactionRecord).Summary[0].Raw, raw) {
					t.Fatal("pending compaction must preserve provider bytes and boundary facts")
				}
			})
		}
	}
}

func TestWorkflowCompactionValidation(t *testing.T) {
	valid := WorkflowCompactionRecord{
		Engine: CompactionEngineLocal, CompactionNumber: 1,
		Summary: []ProviderHistoryItem{{Type: ProviderHistoryItemTypeMessage, Raw: []byte(`{"type":"message"}`)}},
	}
	for _, test := range []struct {
		name   string
		mutate func(*WorkflowCompactionRecord)
	}{
		{"engine", func(record *WorkflowCompactionRecord) { record.Engine = "unknown" }},
		{"number", func(record *WorkflowCompactionRecord) { record.CompactionNumber = 0 }},
		{"baseline", func(record *WorkflowCompactionRecord) { record.CommittedEntryStart = -1 }},
		{"summary", func(record *WorkflowCompactionRecord) { record.Summary = nil }},
		{"provider item", func(record *WorkflowCompactionRecord) {
			record.Summary = []ProviderHistoryItem{{Type: ProviderHistoryItemTypeCompaction}}
		}},
		{"shell role", func(record *WorkflowCompactionRecord) {
			record.RunningShells = []MessageRecord{{Role: MessageRoleUser}}
		}},
		{"carryover role", func(record *WorkflowCompactionRecord) {
			record.PreservedUserMessage = &MessageRecord{Role: MessageRoleDeveloper}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := valid
			test.mutate(&record)
			if _, err := NewEventRecord(1, nil, record); err == nil {
				t.Fatal("invalid pending compaction was accepted")
			}
		})
	}
}

func TestQuestionHistoryWorkflowCompactionBoundary(t *testing.T) {
	for _, version := range []int{EventLogVersionV1, EventLogVersionV2} {
		t.Run(eventLogVersionTestName(version), func(t *testing.T) {
			dir := t.TempDir()
			boundary, err := NewEventRecord(2, nil, WorkflowCompactionRecord{
				Engine: CompactionEngineRemote, CompactionNumber: 1,
				Summary: []ProviderHistoryItem{{Type: ProviderHistoryItemTypeCompaction, Raw: []byte(`{"type":"compaction","encrypted_content":"checkpoint"}`)}},
			})
			if err != nil {
				t.Fatal(err)
			}
			questionRecord := mustQuestionHistoryCursorRecord
			if version == EventLogVersionV1 {
				questionRecord = mustQuestionHistoryCursorRecordV1
			}
			writeQuestionHistoryCursorLog(t, filepath.Join(dir, eventsFile), version, []EventRecord{
				questionRecord(t, 1, "older"), boundary,
				questionRecord(t, 3, "newest"),
			})
			cursor, err := OpenQuestionHistoryCursor(dir, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cursor.Close() }()
			newest, err := cursor.Next(t.Context())
			if err != nil || newest == nil || newest.Seq() != 3 {
				t.Fatalf("newest question: %v %v", newest, err)
			}
			if older, err := cursor.Next(t.Context()); err != nil || older != nil || !cursor.HistoryOmitted() {
				t.Fatalf("pending summary did not bound older questions: %v %v", older, err)
			}
		})
	}
}
