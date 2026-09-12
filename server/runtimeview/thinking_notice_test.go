package runtimeview

import (
	"reflect"
	"testing"

	"core/server/runtime"
	"core/shared/clientui"
	"core/shared/textutil"
	"core/shared/transcript"
)

func TestThinkingNoticeLiveHydrationAndPageParity(t *testing.T) {
	stepID := runtimeStepIDPointer("22222222-2222-4222-8222-222222222222")
	provenance := &runtime.TranscriptCommittedRowProvenance{EventSequence: 5}
	entry := runtime.ChatEntry{
		StepID: stepID, Visibility: transcript.EntryVisibilityDetail, Role: string(transcript.EntryRoleSystem),
		ThinkingEffort: textutil.Value("high"), CommittedProvenance: provenance,
	}
	snapshot := runtime.ChatSnapshot{Entries: []runtime.ChatEntry{entry}}
	facts := runtime.TranscriptCommittedRowFactsFromSnapshot(snapshot)
	hydration := mustTranscriptHydration(t, runtime.TranscriptHydrationSnapshot{CommittedRows: facts})
	page, err := TranscriptPageFromSegment("session", "name", clientui.ConversationFreshnessEstablished, runtime.TranscriptSegmentPage{Snapshot: snapshot})
	if err != nil {
		t.Fatal(err)
	}
	events, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind: runtime.EventLocalEntryAdded, StepID: stepID,
		LocalEntry: &entry, LocalEntryProjected: true, CommittedProvenance: provenance,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || len(page.Entries) != 1 || len(hydration.TailSegment.Entries) != 1 {
		t.Fatal("Thinking row missing from a delivery path")
	}
	live := transcriptPayload[clientui.TranscriptCommittedRow](t, events[0])
	if !reflect.DeepEqual(live, page.Entries[0]) || !reflect.DeepEqual(live, hydration.TailSegment.Entries[0]) {
		t.Fatal("Thinking row changed across delivery paths")
	}
	if err := live.Validate(); err != nil {
		t.Fatal(err)
	}
	if live.Notice == nil || live.Notice.Reason != clientui.TranscriptNoticeThinkingUpdate ||
		live.Notice.ThinkingEffort == nil || *live.Notice.ThinkingEffort != "high" ||
		live.Visibility != transcript.EntryVisibilityDetail {
		t.Fatalf("Thinking row lost its typed effort or visibility: %+v", live)
	}
}
