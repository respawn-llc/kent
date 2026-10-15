package runtimeview

import (
	"testing"

	"core/server/runtime"
	"core/server/session"
	"core/shared/config"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/textutil"
	"core/shared/transcript"
	"google.golang.org/protobuf/proto"
)

func TestCacheWarningLivePersistedHydrationAndPageParity(t *testing.T) {
	for _, fixture := range []struct {
		name       string
		stepID     *string
		mode       config.CacheWarningMode
		visibility transcript.EntryVisibility
	}{
		{"conversation verbose", runtimeStepIDPointer(projectionStepID), config.CacheWarningModeVerbose, transcript.EntryVisibilityOngoing},
		{"conversation default", runtimeStepIDPointer(projectionStepID), config.CacheWarningModeDefault, transcript.EntryVisibilityDetail},
		{"outside step", nil, config.CacheWarningModeVerbose, transcript.EntryVisibilityOngoing},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			record, err := session.NewEventRecord(42, fixture.stepID, session.CacheWarningRecord{
				Scope: session.CacheScopeConversation, Reason: session.CacheWarningReasonReuseDropped,
				LostInputTokens: textutil.Value(173824),
			})
			if err != nil {
				t.Fatal(err)
			}
			live, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
				Kind: runtime.EventCacheWarning, StepID: fixture.stepID,
				CacheWarning: &transcript.CacheWarning{
					Scope: transcript.CacheWarningScopeConversation, Reason: transcript.CacheWarningReasonReuseDropped,
					LostInputTokens: textutil.Value(173824),
				},
				CacheWarningVisibility: fixture.visibility,
				CommittedProvenance:    &runtime.TranscriptCommittedRowProvenance{EventSequence: record.Seq()},
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			scan := runtime.NewPersistedTranscriptScan(runtime.PersistedTranscriptScanRequest{
				CacheWarningMode: fixture.mode,
			})
			if err := scan.ApplyPersistedEvent(record); err != nil {
				t.Fatal(err)
			}
			snapshot := scan.CollectedPageSnapshot()
			page, err := TranscriptPageFromSegment(
				"12345678-1234-4234-8234-123456789012", nil,
				runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED,
				runtime.TranscriptSegmentPage{Snapshot: snapshot}, nil,
			)
			if err != nil {
				t.Fatal(err)
			}
			hydration := mustTranscriptHydration(t, runtime.TranscriptHydrationSnapshot{
				CommittedRows: runtime.TranscriptCommittedRowFactsFromSnapshot(snapshot),
			}, nil)
			if len(live) != 1 || len(page.Entries) != 1 || len(hydration.TailSegment.Entries) != 1 {
				t.Fatalf("expected one warning: live=%d page=%d hydration=%d",
					len(live), len(page.Entries), len(hydration.TailSegment.Entries))
			}
			resident := live[0].GetCommittedRow()
			for _, incoming := range append(page.Entries, hydration.TailSegment.Entries...) {
				if err := protoapi.Validate(incoming); err != nil {
					t.Fatal(err)
				}
				if !proto.Equal(resident, incoming) {
					t.Fatalf("recurrent warning changed its committed payload:\nlive: %v\npersisted: %v", resident, incoming)
				}
				if !textutil.EqualOptional(incoming.GetNotice().StepId, fixture.stepID) {
					t.Fatalf("warning lost its owning Step: %v", incoming)
				}
			}
		})
	}
}
