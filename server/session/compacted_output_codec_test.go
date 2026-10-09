package session

import (
	"bytes"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCompactedOutputCodecSupportedVersions(t *testing.T) {
	for _, version := range []int{EventLogVersionV1, EventLogVersionV2} {
		for _, engine := range []CompactionEngine{CompactionEngineLocal, CompactionEngineRemote} {
			t.Run(eventLogVersionTestName(version)+"/"+string(engine), func(t *testing.T) {
				raw := []byte(`{ "type": "compaction", "encrypted_content": "unchanged-checkpoint" }`)
				item := ProviderHistoryItem{Type: ProviderHistoryItemTypeCompaction, Raw: raw}
				if engine == CompactionEngineLocal {
					raw = []byte(`{ "type": "message", "role": "user", "content": "summary" }`)
					item = ProviderHistoryItem{Type: ProviderHistoryItemTypeMessage, Raw: raw}
				}
				pending := HistoryReplacementRecord{
					Engine: string(engine), Mode: CompactionModeManual,
					CompactionNumber: intPointer(3), CommittedEntryStart: intPointer(19),
					CompactedOutput: &CompactedOutput{Summary: []ProviderHistoryItem{item}},
				}
				prepared := pending
				prepared.CompactedOutput = nil
				prepared.Items = []ProviderHistoryItem{item}
				continuationRaw := []byte(`{ "type": "message", "role": "developer", "content": "continued context" }`)
				prepared.Continuation = []ProviderHistoryItem{{Type: ProviderHistoryItemTypeMessage, Raw: continuationRaw}}
				for _, history := range []HistoryReplacementRecord{pending, prepared} {
					record, err := NewEventRecord(1, nil, history)
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
					if !reflect.DeepEqual(got, want) {
						t.Fatal("compaction must preserve provider bytes and boundary facts")
					}
					replacement := got.(HistoryReplacementRecord)
					items := replacement.Items
					if replacement.CompactedOutput != nil {
						items = replacement.CompactedOutput.Summary
					}
					if !bytes.Equal(items[0].Raw, raw) {
						t.Fatal("compaction changed opaque provider output")
					}
					if replacement.Continuation != nil && !bytes.Equal(replacement.Continuation[0].Raw, continuationRaw) {
						t.Fatal("generation preparation changed retained provider bytes")
					}
				}
			})
		}
	}
}

func TestCompactedOutputValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*HistoryReplacementRecord)
	}{
		{"engine", func(record *HistoryReplacementRecord) { record.Engine = "unknown" }},
		{"number", func(record *HistoryReplacementRecord) { record.CompactionNumber = intPointer(0) }},
		{"missing number", func(record *HistoryReplacementRecord) { record.CompactionNumber = nil }},
		{"baseline", func(record *HistoryReplacementRecord) { record.CommittedEntryStart = intPointer(-1) }},
		{"missing baseline", func(record *HistoryReplacementRecord) { record.CommittedEntryStart = nil }},
		{"summary", func(record *HistoryReplacementRecord) { record.CompactedOutput.Summary = nil }},
		{"prepared and pending", func(record *HistoryReplacementRecord) { record.Items = record.CompactedOutput.Summary }},
		{"empty prepared and pending", func(record *HistoryReplacementRecord) { record.Items = []ProviderHistoryItem{} }},
		{"continuation and pending", func(record *HistoryReplacementRecord) { record.Continuation = record.CompactedOutput.Summary }},
		{"provider item", func(record *HistoryReplacementRecord) {
			record.CompactedOutput.Summary = []ProviderHistoryItem{{Type: ProviderHistoryItemTypeCompaction}}
		}},
		{"shell role", func(record *HistoryReplacementRecord) {
			record.CompactedOutput.RunningShells = []MessageRecord{{Role: MessageRoleUser}}
		}},
		{"carryover role", func(record *HistoryReplacementRecord) {
			record.CompactedOutput.PreservedUserMessage = &MessageRecord{Role: MessageRoleDeveloper}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := HistoryReplacementRecord{
				Engine: string(CompactionEngineLocal), Mode: CompactionModeManual,
				CompactionNumber: intPointer(1), CommittedEntryStart: intPointer(0),
				CompactedOutput: &CompactedOutput{
					Summary: []ProviderHistoryItem{{Type: ProviderHistoryItemTypeMessage, Raw: []byte(`{"type":"message"}`)}},
				},
			}
			test.mutate(&record)
			if _, err := NewEventRecord(1, nil, record); err == nil {
				t.Fatal("invalid pending compaction was accepted")
			}
		})
	}
}

func TestQuestionHistoryCompactedOutputBoundary(t *testing.T) {
	for _, version := range []int{EventLogVersionV1, EventLogVersionV2} {
		t.Run(eventLogVersionTestName(version), func(t *testing.T) {
			dir := t.TempDir()
			boundary, err := NewEventRecord(2, nil, HistoryReplacementRecord{
				Engine: string(CompactionEngineRemote), Mode: CompactionModeAuto,
				CompactionNumber: intPointer(1), CommittedEntryStart: intPointer(0),
				CompactedOutput: &CompactedOutput{
					Summary: []ProviderHistoryItem{{Type: ProviderHistoryItemTypeCompaction, Raw: []byte(`{"type":"compaction","encrypted_content":"checkpoint"}`)}},
				},
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

func TestHistoryReplacementUnionValidationAgreesAcrossReaders(t *testing.T) {
	const summary = `[{"type":"compaction","raw":{"type":"compaction","encrypted_content":"checkpoint"}}]`
	const pending = `"compacted_output":{"summary":` + summary + `}`
	for _, test := range []struct {
		name   string
		fields string
		valid  bool
	}{
		{"empty prepared", `"items":[]`, true},
		{"null output", `"compacted_output":null`, true},
		{"pending", pending, true},
		{"pending null segments", pending + `,"items":null,"continuation":null`, true},
		{"pending empty items", pending + `,"items":[]`, false},
		{"pending empty continuation", pending + `,"continuation":[]`, false},
		{"pending populated items", pending + `,"items":` + summary, false},
		{"pending populated continuation", pending + `,"continuation":` + summary, false},
		{"prepared with continuation", `"items":` + summary + `,"continuation":` + summary, true},
		{"empty output", `"compacted_output":{}`, false},
		{"empty summary", `"compacted_output":{"summary":[]}`, false},
		{"null summary", `"compacted_output":{"summary":null}`, false},
		{"cleared items", `"items":[],"items":null,` + pending, true},
		{"cleared continuation", `"continuation":[],"continuation":null,` + pending, true},
		{"cleared output", pending + `,"compacted_output":null,"items":[]`, true},
		{"replaced summary", pending + `,"compacted_output":{"summary":[]}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, version := range []int{EventLogVersionV1, EventLogVersionV2} {
				line := []byte(`{"seq":1,"kind":"history_replaced","payload":{"engine":"remote","mode":"manual","compaction_number":1,"committed_entry_start":0,` + test.fields + `}}`)
				_, decodeErr := decodeEventRecordForVersion(version, line)
				if (decodeErr == nil) != test.valid {
					t.Fatalf("v%d full decode = %v, want valid=%v", version, decodeErr, test.valid)
				}
				dir := t.TempDir()
				writeRawVersionedEventLog(t, filepath.Join(dir, eventsFile), version, [][]byte{line})
				cursor, err := OpenQuestionHistoryCursor(dir, 1)
				if err != nil {
					t.Fatal(err)
				}
				_, scanErr := cursor.Next(t.Context())
				if err := cursor.Close(); err != nil {
					t.Fatal(err)
				}
				if (scanErr == nil) != test.valid {
					t.Fatalf("v%d bounded scan = %v, want valid=%v", version, scanErr, test.valid)
				}
			}
		})
	}
}
