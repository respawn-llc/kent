package session

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCompactedOutputCodecSupportedVersions(t *testing.T) {
	for _, version := range []int{EventLogVersionV1, EventLogVersionV2} {
		for _, engine := range []CompactionEngine{CompactionEngineLocal, CompactionEngineRemote} {
			t.Run(eventLogVersionTestName(version)+"/"+string(engine), func(t *testing.T) {
				raw := []byte(`{ "type": "compaction", "encrypted_content": "<unchanged>&\u0061", "provider_metadata": {"number":1.2300,"future":[true,null]} }`)
				item := ProviderHistoryItem{Type: ProviderHistoryItemTypeCompaction, Raw: raw}
				if engine == CompactionEngineLocal {
					raw = []byte(`{ "type": "message", "role": "user", "content": "summary" }`)
					item = ProviderHistoryItem{Type: ProviderHistoryItemTypeMessage, Raw: raw}
				}
				sourceRaw := bytes.Clone(raw)
				pending := HistoryReplacementRecord{
					Engine: string(engine), Mode: CompactionModeManual,
					CompactionNumber: intPointer(3), CommittedEntryStart: intPointer(19),
					CompactedOutput: &CompactedOutput{Summary: []ProviderHistoryItem{item}},
				}
				prepared := pending
				prepared.CompactedOutput = nil
				prepared.Items = []ProviderHistoryItem{item}
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
					assertCompactionJSONContentEqual(t, got, want)
					replacement := got.(HistoryReplacementRecord)
					items := replacement.Items
					if replacement.CompactedOutput != nil {
						items = replacement.CompactedOutput.Summary
					}
					assertCompactionJSONContentEqual(t, items[0].Raw, json.RawMessage(raw))
					if !bytes.Equal(item.Raw, sourceRaw) {
						t.Fatal("serialization mutated the source provider output")
					}
				}
			})
		}
	}
}

func assertCompactionJSONContentEqual(t *testing.T, got, want any) {
	t.Helper()
	decode := func(value any) any {
		t.Helper()
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.UseNumber()
		var result any
		if err := decoder.Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if !reflect.DeepEqual(decode(got), decode(want)) {
		t.Fatalf("compaction JSON content changed: got %v, want %v", got, want)
	}
}

func TestCompactedOutputValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*HistoryReplacementRecord)
	}{
		{"engine", func(record *HistoryReplacementRecord) { record.Engine = "unknown" }},
		{"padded engine", func(record *HistoryReplacementRecord) { record.Engine = " local " }},
		{"number", func(record *HistoryReplacementRecord) { record.CompactionNumber = intPointer(0) }},
		{"missing number", func(record *HistoryReplacementRecord) { record.CompactionNumber = nil }},
		{"baseline", func(record *HistoryReplacementRecord) { record.CommittedEntryStart = intPointer(-1) }},
		{"missing baseline", func(record *HistoryReplacementRecord) { record.CommittedEntryStart = nil }},
		{"summary", func(record *HistoryReplacementRecord) { record.CompactedOutput.Summary = nil }},
		{"prepared and pending", func(record *HistoryReplacementRecord) { record.Items = record.CompactedOutput.Summary }},
		{"empty prepared and pending", func(record *HistoryReplacementRecord) { record.Items = []ProviderHistoryItem{} }},
		{"provider item", func(record *HistoryReplacementRecord) {
			record.CompactedOutput.Summary = []ProviderHistoryItem{{Type: ProviderHistoryItemTypeCompaction}}
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
		{"pending null items", pending + `,"items":null`, true},
		{"pending empty items", pending + `,"items":[]`, false},
		{"pending populated items", pending + `,"items":` + summary, false},
		{"empty output", `"compacted_output":{}`, false},
		{"empty summary", `"compacted_output":{"summary":[]}`, false},
		{"null summary", `"compacted_output":{"summary":null}`, false},
		{"cleared items", `"items":[],"items":null,` + pending, true},
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
