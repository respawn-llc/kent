package runtimeview

import (
	"testing"

	"core/server/runtime"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/textutil"
	"core/shared/transcript"

	"google.golang.org/protobuf/proto"
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
	page, err := TranscriptPageFromSegment("58e121b5-30f7-4d0f-a1fa-fb3e6695e39c", "name", runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, runtime.TranscriptSegmentPage{Snapshot: snapshot})
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
	live := events[0].GetCommittedRow()
	if !proto.Equal(live, page.Entries[0]) || !proto.Equal(live, hydration.TailSegment.Entries[0]) {
		t.Fatal("Thinking row changed across delivery paths")
	}
	if err := protoapi.Validate(live); err != nil {
		t.Fatal(err)
	}
	if notice := live.GetNotice(); notice == nil || notice.Reason != transcriptpb.NoticeReason_NOTICE_REASON_THINKING_UPDATE ||
		notice.ThinkingEffort == nil || *notice.ThinkingEffort != "high" ||
		live.Visibility != transcriptpb.EntryVisibility_ENTRY_VISIBILITY_DETAIL {
		t.Fatalf("Thinking row lost its typed effort or visibility: %+v", live)
	}
}
