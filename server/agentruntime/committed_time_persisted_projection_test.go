package runtime

import (
	"testing"

	"core/server/llm"
	"core/server/sessionstore"
	"core/shared/textutil"
	"core/shared/transcriptmodel"
)

func TestOrdinaryCommittedTimePersistsAcrossPageRestartAndLiveProjection(t *testing.T) {
	store := mustCreateTestSession(t)
	userRecord := mustAppendTestEvent(t, store, "step", llm.Message{
		Role:    llm.RoleUser,
		Content: textutil.Value("persisted user"),
	})
	assistantRecord := mustAppendTestEvent(t, store, "step", llm.Message{
		Role:    llm.RoleAssistant,
		Content: textutil.Value("persisted assistant"),
		Phase:   textutil.Value(llm.MessagePhaseFinal),
	})

	assertProjection := func(t *testing.T, target *session.Store) {
		t.Helper()
		window, err := mustMaterializeTestEventLog(t, target).ReadRecentRecords(16)
		if err != nil {
			t.Fatalf("read persisted records: %v", err)
		}
		scan := newStreamingTranscriptScan("")
		for _, record := range window.Records {
			if err := scan.ApplyPersistedEvent(record); err != nil {
				t.Fatalf("scan persisted record: %v", err)
			}
		}
		facts := TranscriptCommittedRowFactsFromSnapshot(scan.PageSnapshot())
		if len(facts) != 2 {
			t.Fatalf("persisted page facts = %d, want 2", len(facts))
		}
		assertOrdinaryMessageCommittedTime(t, facts[0], userRecord.CommittedAtUnixMs())
		assertOrdinaryMessageCommittedTime(t, facts[1], assistantRecord.CommittedAtUnixMs())
	}

	assertProjection(t, store)
	reopened := mustOpenTestSession(t, store.Dir())
	assertProjection(t, reopened)

	for _, fixture := range []struct {
		name   string
		record session.EventRecord
		kind   EventKind
		text   string
	}{
		{name: "user", record: userRecord, kind: EventConversationUpdated, text: "persisted user"},
		{name: "assistant", record: assistantRecord, kind: EventAssistantMessage, text: "persisted assistant"},
	} {
		provenance, err := transcriptProvenanceFromRecord(fixture.record)
		if err != nil {
			t.Fatalf("%s live provenance: %v", fixture.name, err)
		}
		message := llm.Message{Content: textutil.Value(fixture.text)}
		if fixture.kind == EventConversationUpdated {
			message.Role = llm.RoleUser
		} else {
			message.Role = llm.RoleAssistant
			message.Phase = textutil.Value(llm.MessagePhaseFinal)
		}
		facts := TranscriptCommittedRowFactsFromEvent(Event{
			Kind:                fixture.kind,
			StepID:              textutil.Value("step"),
			Message:             message,
			CommittedProvenance: &provenance,
		})
		if len(facts) != 1 {
			t.Fatalf("%s live facts = %d, want 1", fixture.name, len(facts))
		}
		assertOrdinaryMessageCommittedTime(t, facts[0], fixture.record.CommittedAtUnixMs())
	}
}

func assertOrdinaryMessageCommittedTime(
	t *testing.T,
	fact TranscriptCommittedRowFact,
	want *transcript.CommittedAtUnixMs,
) {
	t.Helper()
	if want == nil {
		t.Fatal("ordinary message fixture has no persisted committed time")
	}
	if fact.Provenance == nil || fact.Provenance.CommittedAtUnixMs == nil ||
		fact.Provenance.CommittedAtUnixMs.UnixMs() != want.UnixMs() {
		t.Fatalf("committed provenance time = %+v, want %d", fact.Provenance, want.UnixMs())
	}
	if fact.User == nil && fact.Assistant == nil {
		t.Fatalf("ordinary message fact has no message payload: %+v", fact)
	}
}
