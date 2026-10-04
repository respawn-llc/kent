package runtimeview

import (
	"testing"

	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/rollbacktarget"
	"core/shared/textutil"
	"core/shared/transcript"
	"google.golang.org/protobuf/proto"
)

func mustProjectNewestPersistedTranscript(
	t *testing.T,
	store *session.Store,
	eventLog session.MaterializedEventLog,
) (*transcriptpb.Page, *transcriptpb.Hydration) {
	t.Helper()
	segment, err := runtime.TranscriptNewestSegmentPageFromEventLog(eventLog, "")
	if err != nil {
		t.Fatalf("scan newest transcript page: %v", err)
	}
	page, err := TranscriptPageFromSegment(
		store.Meta().SessionID,
		"",
		runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED,
		segment,
	)
	if err != nil {
		t.Fatalf("project persisted page: %v", err)
	}
	hydration := mustTranscriptHydration(t, runtime.TranscriptHydrationSnapshot{
		CommittedRows: runtime.TranscriptCommittedRowFactsFromSnapshot(segment.Snapshot),
	})
	return page, hydration
}

func TestPersistedUserRollbackTargetMatchesLiveDelivery(t *testing.T) {
	const text = "persisted user message"

	store := newRuntimeViewStore(t)
	eventLog, err := store.MaterializeEventLog()
	if err != nil {
		t.Fatalf("materialize event log: %v", err)
	}
	stepID := transcriptProjectionStepID
	record, _, err := eventLog.AppendRecord(&stepID, session.MessageRecord{
		Role:    session.MessageRoleUser,
		Content: textutil.Value(text),
	})
	if err != nil {
		t.Fatalf("append user message: %v", err)
	}

	projectedPage, hydration := mustProjectNewestPersistedTranscript(t, store, eventLog)
	if len(projectedPage.Entries) != 1 || projectedPage.Entries[0].GetUser() == nil ||
		len(hydration.TailSegment.Entries) != 1 {
		t.Fatalf("persisted rows: page=%d hydration=%d, want one each", len(projectedPage.Entries), len(hydration.TailSegment.Entries))
	}

	provenance := &runtime.TranscriptCommittedRowProvenance{
		EventSequence:     record.Seq(),
		CommittedAtUnixMs: record.CommittedAtUnixMs(),
	}
	targetID := rollbacktarget.EncodeUserMessageSeq(record.Seq())
	if got := projectedPage.Entries[0].GetUser().RollbackTargetId; got == nil || *got != string(targetID) {
		t.Fatalf("persisted rollback target = %v, want %q", got, targetID)
	}

	liveEvents := []struct {
		name  string
		event runtime.Event
	}{
		{
			name: "flushed",
			event: runtime.Event{
				Kind:                runtime.EventUserMessageFlushed,
				UserMessage:         text,
				StepID:              textutil.Value(stepID),
				CommittedProvenance: provenance,
			},
		},
		{
			name: "message",
			event: runtime.Event{
				Kind:   runtime.EventConversationUpdated,
				StepID: textutil.Value(stepID),
				Message: llm.Message{
					Role:    llm.RoleUser,
					Content: textutil.Value(text),
				},
				CommittedProvenance: provenance,
			},
		},
	}
	for _, fixture := range liveEvents {
		t.Run(fixture.name, func(t *testing.T) {
			liveMessages, err := TranscriptMessagesFromRuntimeEventChecked(fixture.event)
			if err != nil {
				t.Fatalf("project live event: %v", err)
			}
			if len(liveMessages) != 1 || liveMessages[0].GetCommittedRow() == nil {
				t.Fatalf("live transcript messages = %+v, want one committed row", liveMessages)
			}
			liveRow := liveMessages[0].GetCommittedRow()
			if !proto.Equal(liveRow, projectedPage.Entries[0]) {
				t.Fatalf("live row differs from persisted page:\nlive: %+v\npage: %+v", liveRow, projectedPage.Entries[0])
			}
			if !proto.Equal(liveRow, hydration.TailSegment.Entries[0]) {
				t.Fatalf("live row differs from hydration:\nlive: %+v\nhydration: %+v", liveRow, hydration.TailSegment.Entries[0])
			}
		})
	}
}

func TestPersistedDeveloperNoticeDiagnosticCodeMatchesLiveDelivery(t *testing.T) {
	const text = "workspace environment details"

	store := newRuntimeViewStore(t)
	eventLog, err := store.MaterializeEventLog()
	if err != nil {
		t.Fatalf("materialize event log: %v", err)
	}
	stepID := transcriptProjectionStepID
	messageType := session.MessageTypeEnvironment
	record, _, err := eventLog.AppendRecord(&stepID, session.MessageRecord{
		Role:        session.MessageRoleDeveloper,
		MessageType: &messageType,
		Content:     textutil.Value(text),
	})
	if err != nil {
		t.Fatalf("append developer message: %v", err)
	}

	projectedPage, hydration := mustProjectNewestPersistedTranscript(t, store, eventLog)
	if len(projectedPage.Entries) != 1 || len(hydration.TailSegment.Entries) != 1 {
		t.Fatalf("persisted rows: page=%d hydration=%d, want one each", len(projectedPage.Entries), len(hydration.TailSegment.Entries))
	}

	provenance := &runtime.TranscriptCommittedRowProvenance{
		EventSequence:     record.Seq(),
		CommittedAtUnixMs: record.CommittedAtUnixMs(),
	}
	liveMessages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind:   runtime.EventConversationUpdated,
		StepID: textutil.Value(stepID),
		Message: llm.Message{
			Role:        llm.RoleDeveloper,
			MessageType: textutil.Value(llm.MessageType(messageType)),
			Content:     textutil.Value(text),
		},
		CommittedProvenance: provenance,
	})
	if err != nil {
		t.Fatalf("project live developer message: %v", err)
	}
	if len(liveMessages) != 1 || liveMessages[0].GetCommittedRow() == nil {
		t.Fatalf("live transcript messages = %+v, want one committed row", liveMessages)
	}
	liveRow := liveMessages[0].GetCommittedRow()
	if notice := liveRow.GetNotice(); notice == nil ||
		notice.Diagnostic == nil ||
		notice.Diagnostic.Code != string(transcript.EntryRoleDeveloperContext) {
		t.Fatalf("live developer notice diagnostic = %+v, want canonical developer-context code", notice)
	}
	for name, persistedRow := range map[string]*transcriptpb.CommittedRow{
		"page":      projectedPage.Entries[0],
		"hydration": hydration.TailSegment.Entries[0],
	} {
		if !proto.Equal(liveRow, persistedRow) {
			t.Fatalf("live row differs from %s:\nlive: %+v\n%s: %+v", name, liveRow, name, persistedRow)
		}
	}
}
