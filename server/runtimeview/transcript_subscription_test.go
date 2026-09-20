package runtimeview

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"core/internal/testharness/scriptedllm"
	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	"core/server/tools"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/rollbacktarget"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/textutil"
	"core/shared/toolspec"
	"core/shared/transcript"
	patchformat "core/shared/transcript/patchformat"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

func assertTranscriptRoundTrip(t *testing.T, message *transcriptpb.Message) {
	t.Helper()
	if err := protoapi.Validate(message); err != nil {
		t.Fatalf("validate transcript: %v", err)
	}
	encoded, err := proto.Marshal(message)
	if err != nil {
		t.Fatalf("marshal transcript: %v", err)
	}
	decoded := new(transcriptpb.Message)
	if err := proto.Unmarshal(encoded, decoded); err != nil {
		t.Fatalf("unmarshal transcript: %v", err)
	}
	if err := protoapi.Validate(decoded); err != nil {
		t.Fatalf("validate decoded transcript: %v", err)
	}
	if !proto.Equal(message, decoded) {
		t.Fatalf("transcript binary round trip differs: got %v want %v", decoded, message)
	}
}

func TestTranscriptProjectionOwnsNestedDeletionPresentation(t *testing.T) {
	id := patchformat.WholeFileDeletionOperationID{HunkOrdinal: 0}
	removed := 3
	source := &transcript.ToolCallMeta{
		ToolName: "patch",
		PatchPresentation: &patchformat.Presentation{
			Variant: patchformat.PresentationVariantChanges,
			Changes: &patchformat.Changes{
				Files: []patchformat.FileChange{
					{
						Path:    patchformat.Path{Absolute: "/workspace/target.txt", Relative: "target.txt"},
						Removed: &removed,
						Operations: []patchformat.FileOperation{
							{
								Kind: patchformat.FileOperationDelete,
								Deletion: &patchformat.WholeFileDeletionOperation{
									ID: id,
									Disposition: &patchformat.WholeFileDeletionDisposition{
										PhysicalGroup: patchformat.WholeFileDeletionGroupID{FirstOperation: id},
										Removed:       3,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	projected, err := ToolPresentationToProto(source)
	if err != nil {
		t.Fatalf("project tool presentation: %v", err)
	}
	deletion := projected.PatchPresentation.GetChanges().Files[0].Operations[0].GetDelete()
	deletion.Disposition.Removed = 9
	deletion.Disposition.PhysicalGroup.FirstOperation.HunkOrdinal = 4

	disposition := source.PatchPresentation.Changes.Files[0].Operations[0].Deletion.Disposition
	if disposition == nil || disposition.Removed != 3 ||
		disposition.PhysicalGroup.FirstOperation.HunkOrdinal != 0 {
		t.Fatalf("runtimeview clone aliased nested deletion metadata: %+v", disposition)
	}
}

func TestTranscriptCacheWarningProjectionPreservesAbsentTokenLoss(t *testing.T) {
	notice, err := transcriptNoticeFromFact(nil, &runtime.TranscriptNoticeRowFact{
		Reason:   transcript.NoticeReasonCacheWarning,
		Severity: transcript.NoticeSeverityWarning,
		CacheWarning: &runtime.TranscriptCacheWarningFact{
			Scope:      string(transcript.CacheWarningScopeConversation),
			Reason:     string(transcript.CacheWarningReasonCompaction),
			Visibility: transcript.EntryVisibilityOngoing,
		},
	})
	if err != nil {
		t.Fatalf("project cache warning: %v", err)
	}
	if notice.CacheWarning == nil {
		t.Fatal("cache-warning projection is absent")
	}
	if notice.CacheWarning.LostInputTokens != nil {
		t.Fatalf("projected absent token loss = %v, want nil", *notice.CacheWarning.LostInputTokens)
	}
}

func TestTranscriptProviderModelMismatchProjectionPreservesTypedFacts(t *testing.T) {
	mismatch := transcript.ProviderModelMismatchNotice{
		RequestedModel: "requested-model",
		ServedModel:    "served-model",
	}
	notice, err := transcriptNoticeFromFact(nil, &runtime.TranscriptNoticeRowFact{
		Reason:                transcript.NoticeReasonProviderModelMismatch,
		Severity:              transcript.NoticeSeverityWarning,
		ProviderModelMismatch: &mismatch,
	})
	if err != nil {
		t.Fatalf("project model mismatch: %v", err)
	}
	if notice.ProviderModelMismatch == nil ||
		notice.ProviderModelMismatch.RequestedModel != mismatch.RequestedModel ||
		notice.ProviderModelMismatch.ServedModel != mismatch.ServedModel {
		t.Fatalf("provider-model mismatch projection = %+v, want %+v", notice.ProviderModelMismatch, mismatch)
	}
}

func TestTranscriptCompactionProjectionCarriesTypedFactsWithoutServerPresentation(t *testing.T) {
	count := 2
	detail := "provider summary"
	notice, err := transcriptNoticeFromFact(nil, &runtime.TranscriptNoticeRowFact{
		Reason:      transcript.NoticeReasonCompaction,
		Severity:    transcript.NoticeSeverityInfo,
		MessageType: llm.MessageTypeCompactionSummary,
		Compaction: &runtime.TranscriptCompactionNoticeFact{
			Count:  &count,
			Detail: &detail,
		},
	})
	if err != nil {
		t.Fatalf("project compaction: %v", err)
	}
	if notice.Compaction == nil ||
		notice.Compaction.Count == nil ||
		*notice.Compaction.Count != int32(count) ||
		notice.Compaction.Detail == nil ||
		*notice.Compaction.Detail != detail {
		t.Fatalf("compaction projection = %+v", notice.Compaction)
	}
	if notice.CompactLabel != nil || notice.CondensedText != nil || notice.Diagnostic != nil {
		t.Fatalf("server-authored compaction presentation leaked into client contract: %+v", notice)
	}
}

func TestTranscriptCompactionStatusPreservesInitiatingRequestIdentity(t *testing.T) {
	requestID := runtimeids.NewCompactionRequestID()
	messages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind:   runtime.EventCompactionCompleted,
		StepID: runtimeStepIDPointer(transcriptProjectionStepID),
		Compaction: &runtime.CompactionStatus{
			Mode:      "manual",
			RequestID: &requestID,
			Count:     1,
		},
	})
	if err != nil {
		t.Fatalf("project compaction status: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("compaction messages = %+v, want one", messages)
	}
	status := messages[0].GetCompactionStatus()
	if status.RequestId == nil || *status.RequestId != requestID.String() {
		t.Fatalf("projected request identity = %v, want %s", status.RequestId, requestID.String())
	}
}

func TestTranscriptPendingWorkTechnicalRestorationProjection(t *testing.T) {
	itemID := runtimeids.NewQueueItemID()
	restoration := runtimeinput.PendingWorkTechnicalRestoration{
		ItemID:         itemID,
		Kind:           runtimeinput.PendingWorkItemKindWorktreeTransition,
		CanonicalInput: "/wt leave",
	}
	messages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind:                   runtime.EventPendingWorkRestored,
		PendingWorkRestoration: &restoration,
	})
	if err != nil {
		t.Fatalf("project pending work restoration: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("technical restoration messages = %+v, want one", messages)
	}
	projected := messages[0].GetPendingWorkRestored()
	want := &transcriptpb.PendingWorkTechnicalRestoration{
		ItemId:         itemID.String(),
		Kind:           runtimepb.PendingWorkItemKind_PENDING_WORK_ITEM_KIND_WORKTREE_TRANSITION,
		CanonicalInput: restoration.CanonicalInput,
	}
	if !proto.Equal(projected.Restoration, want) {
		t.Fatalf("projected technical restoration = %+v, want %+v", projected.Restoration, restoration)
	}
}

func TestTranscriptPendingWorkChangedProjectionCarriesNoCollection(t *testing.T) {
	messages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{Kind: runtime.EventPendingWorkChanged})
	if err != nil {
		t.Fatalf("project pending work change: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("Pending Work Changed messages = %+v, want one", messages)
	}
	if messages[0].GetPendingWorkChanged() == nil {
		t.Fatalf("Pending Work Changed event = %v", messages[0])
	}
}

func TestTranscriptHydrationPreservesDeletionDispositionPresence(t *testing.T) {
	id := patchformat.WholeFileDeletionOperationID{HunkOrdinal: 0}
	tests := []struct {
		name        string
		disposition *patchformat.WholeFileDeletionDisposition
		wantRemoved *int
	}{
		{name: "absent"},
		{
			name: "present zero",
			disposition: &patchformat.WholeFileDeletionDisposition{
				PhysicalGroup: patchformat.WholeFileDeletionGroupID{FirstOperation: id},
				Removed:       0,
			},
			wantRemoved: textutil.Value(0),
		},
		{
			name: "present positive",
			disposition: &patchformat.WholeFileDeletionDisposition{
				PhysicalGroup: patchformat.WholeFileDeletionGroupID{FirstOperation: id},
				Removed:       4,
			},
			wantRemoved: textutil.Value(4),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var removed *int
			if test.disposition != nil {
				removed = textutil.Value(test.disposition.Removed)
			}
			hydration := mustTranscriptHydration(t, runtime.TranscriptHydrationSnapshot{
				CommittedRows: []runtime.TranscriptCommittedRowFact{{
					StepID:     runtimeStepIDPointer(transcriptProjectionStepID),
					Visibility: transcript.EntryVisibilityOngoingCollapsed,
					Integrity:  transcript.RowIntegrityValid,
					Kind:       runtime.TranscriptCommittedRowFactTool,
					Locator:    transcript.CommittedRowLocator{EventSequence: 1, RowOrdinal: 1},
					Provenance: &runtime.TranscriptCommittedRowProvenance{EventSequence: 1},
					Tool: &runtime.TranscriptToolRowFact{
						ToolCallID: "call-delete",
						ToolName:   "patch",
						Presentation: &transcript.ToolCallMeta{
							ToolName:       "patch",
							Presentation:   transcript.ToolPresentationDefault,
							RenderBehavior: transcript.ToolCallRenderBehaviorDefault,
							PatchPresentation: &patchformat.Presentation{
								Variant: patchformat.PresentationVariantChanges,
								Changes: &patchformat.Changes{
									Files: []patchformat.FileChange{
										{
											Path:    patchformat.Path{Absolute: "/workspace/target.txt", Relative: "target.txt"},
											Removed: removed,
											Operations: []patchformat.FileOperation{
												{
													Kind: patchformat.FileOperationDelete,
													Deletion: &patchformat.WholeFileDeletionOperation{
														ID:          id,
														Disposition: test.disposition,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				}},
			})
			if len(hydration.TailSegment.Entries) != 1 ||
				hydration.TailSegment.Entries[0].GetTool() == nil ||
				hydration.TailSegment.Entries[0].GetTool().Presentation == nil ||
				hydration.TailSegment.Entries[0].GetTool().Presentation.PatchPresentation == nil ||
				hydration.TailSegment.Entries[0].GetTool().Presentation.PatchPresentation.GetChanges() == nil {
				t.Fatalf("projected deletion row = %+v", hydration.TailSegment.Entries)
			}
			file := hydration.TailSegment.Entries[0].GetTool().Presentation.PatchPresentation.GetChanges().Files[0]
			projectedRemoved := file.Removed
			disposition := file.Operations[0].GetDelete().Disposition
			if test.wantRemoved == nil {
				if projectedRemoved != nil || disposition != nil {
					t.Fatalf("projected deletion = %v, want absent count and disposition", file)
				}
				return
			}
			if projectedRemoved == nil || *projectedRemoved != int32(*test.wantRemoved) ||
				disposition == nil || disposition.Removed != int32(*test.wantRemoved) {
				t.Fatalf("projected deletion = %v, want removed %d", file, *test.wantRemoved)
			}
		})
	}
}

const (
	transcriptProjectionRunID  = "10000000-0000-4000-8000-000000000011"
	transcriptProjectionStepID = "10000000-0000-4000-8000-000000000012"
)

func mustTranscriptHydration(t *testing.T, snapshot runtime.TranscriptHydrationSnapshot) *transcriptpb.Hydration {
	t.Helper()
	tailSegment, err := transcriptTailSegmentFromFactsChecked(snapshot.CommittedRows, nil, false)
	if err != nil {
		t.Fatalf("project transcript hydration tail: %v", err)
	}
	hydration, err := TranscriptHydrationFromSnapshotChecked(snapshot, tailSegment)
	if err != nil {
		t.Fatalf("TranscriptHydrationFromSnapshot: %v", err)
	}
	return hydration
}

func TestTranscriptHydrationCarriesRuntimeNativeAssistantStreamIdentity(t *testing.T) {
	streamID := uuid.MustParse("f84c7d21-4c94-4a54-87fd-b41f5bd01d38")
	hydration := mustTranscriptHydration(t, runtime.TranscriptHydrationSnapshot{
		ActiveAssistantText:     "hello",
		ActiveAssistantMetadata: &runtime.AssistantStreamMetadata{StepID: transcriptProjectionStepID},
		ActiveAssistantStreamID: &streamID,
		ActiveAssistantPhase:    llm.MessagePhaseFinal,
	})
	if hydration.ActiveAssistant == nil {
		t.Fatal("expected active assistant stream in hydration")
	}
	if got := hydration.ActiveAssistant.StreamId; got != streamID.String() {
		t.Fatalf("active assistant stream id = %q, want %q", got, streamID.String())
	}
	if hydration.ActiveAssistant.Text != "hello" {
		t.Fatalf("active assistant stream text = %q, want hello", hydration.ActiveAssistant.Text)
	}
	if hydration.ActiveAssistant.Phase != transcriptpb.AssistantPhase_ASSISTANT_PHASE_FINAL {
		t.Fatalf("active assistant stream phase = %q, want final", hydration.ActiveAssistant.Phase)
	}
}

func TestTranscriptHydrationProjectsRuntimeOwnedFacts(t *testing.T) {
	goalTime := time.Date(2026, time.July, 20, 10, 0, 0, 0, time.UTC)
	hydration := mustTranscriptHydration(t, runtime.TranscriptHydrationSnapshot{
		ActiveThinkingStatus: &runtime.TranscriptThinkingStatusState{
			StepID: transcriptProjectionStepID, Text: "Planning",
		},
		ActiveReasoningTraces: []runtime.TranscriptReasoningTraceState{{
			StepID: transcriptProjectionStepID,
			Identity: runtime.TranscriptReasoningTraceIdentity{Kent: func() *runtimeids.ReasoningTraceID {
				id := runtimeids.NewReasoningTraceID()
				return &id
			}()},
			Text: "inspect",
		}},
		InFlightTools:    []runtime.TranscriptLiveToolStart{{StepID: transcriptProjectionStepID, ToolCallID: "call-1", ToolName: "shell"}},
		ActiveCompaction: &runtime.TranscriptCompactionState{StepID: transcriptProjectionStepID, Mode: "auto", Count: 3},
		ContextUsage:     &runtime.ContextUsage{UsedTokens: 123, WindowTokens: 4000, CacheHitPercent: 25, HasCacheHitPercentage: true},
		Goal:             &session.GoalState{ID: "goal-1", Objective: "ship", Status: session.GoalStatusActive, CreatedAt: goalTime, UpdatedAt: goalTime},
		GoalSuspended:    true,
	})
	if hydration.ActiveThinkingStatus == nil || hydration.ActiveThinkingStatus.Text != "Planning" ||
		len(hydration.ActiveReasoningTraces) != 1 || hydration.ActiveReasoningTraces[0].Text != "inspect" {
		t.Fatalf("reasoning = status %+v traces %+v", hydration.ActiveThinkingStatus, hydration.ActiveReasoningTraces)
	}
	if len(hydration.InFlightTools) != 1 || hydration.InFlightTools[0].ToolCallId != "call-1" {
		t.Fatalf("tools = %+v", hydration.InFlightTools)
	}
	if hydration.ActiveCompaction == nil || hydration.ActiveCompaction.Count != 3 {
		t.Fatalf("compaction = %+v", hydration.ActiveCompaction)
	}
	if hydration.ContextUsage == nil || hydration.ContextUsage.CacheHitPercent == nil ||
		*hydration.ContextUsage.CacheHitPercent != 25 || hydration.GoalStatus == nil ||
		hydration.GoalStatus.Goal == nil || !hydration.GoalStatus.Suspended {
		t.Fatalf("usage/goal = usage %+v goal %+v", hydration.ContextUsage, hydration.GoalStatus)
	}
}

func TestTranscriptReasoningHydrationAndLivePreserveOrderedIdentities(t *testing.T) {
	firstIndex, secondIndex := int64(0), int64(1)
	firstID := runtimeids.NewReasoningTraceID()
	hydration := mustTranscriptHydration(t, runtime.TranscriptHydrationSnapshot{
		ActiveReasoningTraces: []runtime.TranscriptReasoningTraceState{
			{StepID: transcriptProjectionStepID, Identity: runtime.TranscriptReasoningTraceIdentity{Kent: &firstID}, Text: "first"},
			{StepID: transcriptProjectionStepID, Identity: runtime.TranscriptReasoningTraceIdentity{Provider: &llm.ReasoningItemIdentity{ItemID: "second", PartIndex: &secondIndex}}, Text: "second"},
		},
	})
	if len(hydration.ActiveReasoningTraces) != 2 || hydration.ActiveReasoningTraces[0].Identity.GetKentTraceId() != firstID.String() ||
		hydration.ActiveReasoningTraces[1].Identity.GetProvider() == nil {
		t.Fatalf("hydrated reasoning order = %+v", hydration.ActiveReasoningTraces)
	}
	output := int64(0)
	firstLive, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind: runtime.EventReasoningDelta, StepID: runtimeStepIDPointer(transcriptProjectionStepID),
		ReasoningDelta: &llm.ReasoningSummaryDelta{
			SourceCoordinate: &llm.ReasoningSourceCoordinate{OutputIndex: &output, PartIndex: &firstIndex},
			Text:             "first",
		},
		ReasoningTraceIdentity: &runtime.TranscriptReasoningTraceIdentity{Kent: &firstID},
	})
	if err != nil {
		t.Fatalf("project first live reasoning event: %v", err)
	}
	secondLive, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind: runtime.EventReasoningDelta, StepID: runtimeStepIDPointer(transcriptProjectionStepID),
		ReasoningDelta: &llm.ReasoningSummaryDelta{
			SourceCoordinate: &llm.ReasoningSourceCoordinate{OutputIndex: &output, PartIndex: &secondIndex},
			Text:             "second",
		},
		ReasoningTraceIdentity: &runtime.TranscriptReasoningTraceIdentity{
			Provider: &llm.ReasoningItemIdentity{ItemID: "second", PartIndex: &secondIndex},
		},
	})
	if err != nil {
		t.Fatalf("project second live reasoning event: %v", err)
	}
	live := append(firstLive, secondLive...)
	if len(live) != 2 {
		t.Fatalf("live reasoning messages = %+v", live)
	}
	for index, event := range live {
		update := event.GetReasoningTraceUpdate()
		hydrated := hydration.ActiveReasoningTraces[index]
		if !proto.Equal(update.Identity, hydrated.Identity) ||
			update.Text != hydrated.Text {
			t.Fatalf("live/hydration reasoning mismatch at %d: live=%+v hydration=%+v", index, update, hydrated)
		}
	}
}

func TestTranscriptCommittedRowsPreserveRuntimeVisibility(t *testing.T) {
	messages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind:                runtime.EventLocalEntryAdded,
		StepID:              runtimeStepIDPointer(transcriptProjectionStepID),
		LocalEntryProjected: true,
		LocalEntry: &runtime.ChatEntry{
			Visibility: transcript.EntryVisibilityDetail,
			Role:       "user",
			Text:       "detail-only row",
			CommittedProvenance: &runtime.TranscriptCommittedRowProvenance{
				EventSequence: 1,
			},
		},
	})
	if err != nil {
		t.Fatalf("project runtime visibility row: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %+v, want one committed row", messages)
	}
	row := messages[0].GetCommittedRow()
	if got := row.Visibility; got != transcriptpb.EntryVisibility_ENTRY_VISIBILITY_DETAIL {
		t.Fatalf("committed row visibility = %q, want detail", got)
	}

	hydration := mustTranscriptHydration(t, runtime.TranscriptHydrationSnapshot{
		CommittedRows: []runtime.TranscriptCommittedRowFact{{
			StepID:     runtimeStepIDPointer(transcriptProjectionStepID),
			Visibility: transcript.EntryVisibilityHidden,
			Kind:       runtime.TranscriptCommittedRowFactUser,
			Locator:    transcript.CommittedRowLocator{EventSequence: 2, RowOrdinal: 1},
			User:       &runtime.TranscriptUserRowFact{Text: "hidden row"},
			Provenance: &runtime.TranscriptCommittedRowProvenance{
				EventSequence: 2,
			},
		}},
	})
	if len(hydration.TailSegment.Entries) != 1 {
		t.Fatalf("hydration rows = %+v, want one committed row", hydration.TailSegment.Entries)
	}
	if got := hydration.TailSegment.Entries[0].Visibility; got != transcriptpb.EntryVisibility_ENTRY_VISIBILITY_HIDDEN {
		t.Fatalf("hydration visibility = %q, want hidden", got)
	}
}

func TestDeveloperMessageProjectsAsRegularNotice(t *testing.T) {
	entry := runtime.ChatEntry{
		Visibility: transcript.EntryVisibilityOngoing,
		Role:       string(transcript.EntryRoleDeveloperContext),
		Text:       "continuation context",
		CommittedProvenance: &runtime.TranscriptCommittedRowProvenance{
			EventSequence: 1,
		},
	}
	messages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind:                runtime.EventLocalEntryAdded,
		LocalEntryProjected: true,
		LocalEntry:          &entry,
	})
	if err != nil {
		t.Fatalf("project developer context row: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("native reminder events = %d, want one", len(messages))
	}
	live := messages[0].GetCommittedRow()
	hydration := mustTranscriptHydration(t, runtime.TranscriptHydrationSnapshot{
		CommittedRows: runtime.TranscriptCommittedRowFactsFromSnapshot(runtime.ChatSnapshot{
			Entries: []runtime.ChatEntry{entry},
		}),
	})
	if len(hydration.TailSegment.Entries) != 1 {
		t.Fatalf("hydrated native reminder rows = %d, want one", len(hydration.TailSegment.Entries))
	}
	for _, row := range []*transcriptpb.CommittedRow{live, hydration.TailSegment.Entries[0]} {
		if row.Visibility != transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING ||
			row.GetNotice() == nil ||
			row.GetNotice().MessageType != nil {
			t.Fatalf("developer context must use the ordinary notice contract: %+v", row)
		}
		if err := protoapi.Validate(row); err != nil {
			t.Fatalf("native reminder notice contract: %v", err)
		}
	}
}

func TestTranscriptCommittedRowsProjectCommitTime(t *testing.T) {
	committedAt := transcript.CommittedAtUnixMs(123)
	stepID := runtimeStepIDPointer(transcriptProjectionStepID)
	for _, fact := range []runtime.TranscriptCommittedRowFact{
		{
			Visibility: transcript.EntryVisibilityOngoing,
			Integrity:  transcript.RowIntegrityValid,
			Kind:       runtime.TranscriptCommittedRowFactUser,
			User: &runtime.TranscriptUserRowFact{
				Text:              "user",
				CommittedAtUnixMs: &committedAt,
			},
		},
		{
			StepID:     stepID,
			Visibility: transcript.EntryVisibilityOngoing,
			Integrity:  transcript.RowIntegrityValid,
			Kind:       runtime.TranscriptCommittedRowFactAssistant,
			Assistant: &runtime.TranscriptAssistantRowFact{
				Text:              "assistant",
				Phase:             llm.MessagePhaseFinal,
				CommittedAtUnixMs: &committedAt,
			},
		},
	} {
		row, err := transcriptRowFromFact(fact)
		if err != nil {
			t.Fatalf("project committed time: %v", err)
		}
		switch {
		case row.GetUser() != nil:
			if row.GetUser().CommittedAt == nil || row.GetUser().CommittedAt.AsTime().UnixMilli() != committedAt.UnixMs() {
				t.Fatalf("user committed time = %v, want %d", row.GetUser().CommittedAt, committedAt.UnixMs())
			}
		case row.GetAssistant() != nil:
			if row.GetAssistant().CommittedAt == nil || row.GetAssistant().CommittedAt.AsTime().UnixMilli() != committedAt.UnixMs() {
				t.Fatalf("assistant committed time = %v, want %d", row.GetAssistant().CommittedAt, committedAt.UnixMs())
			}
		default:
			t.Fatalf("projected row = %+v, want user or assistant", row)
		}
	}
}

func TestRuntimeScopedUserFlushProjectsWithoutExactStep(t *testing.T) {
	messages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind:             runtime.EventUserMessageFlushed,
		UserMessage:      "idle input",
		UserMessageBatch: []string{"idle input"},
		UserMessageBatchQueuedItems: []runtime.QueuedUserMessageIdentity{{
			QueueItemID: "10000000-0000-4000-8000-000000000020",
		}},
		CommittedProvenance: &runtime.TranscriptCommittedRowProvenance{EventSequence: 1},
	})
	if err != nil {
		t.Fatalf("project Runtime user flush: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("Runtime user flush messages = %+v, want committed row and flush", messages)
	}
	var userRow *transcriptpb.UserRow
	var flushed *transcriptpb.UserMessageFlushed
	for index := range messages {
		assertTranscriptRoundTrip(t, &transcriptpb.Message{Sequence: uint64(index + 2), Event: messages[index]})
		switch payload := messages[index].Payload.(type) {
		case *transcriptpb.Event_CommittedRow:
			userRow = payload.CommittedRow.GetUser()
		case *transcriptpb.Event_UserMessageFlushed:
			flushed = payload.UserMessageFlushed
		}
	}
	if userRow == nil || userRow.StepId != nil || flushed == nil || flushed.StepId != nil {
		t.Fatalf("Runtime user/flush Step provenance = user:%+v flush:%+v, want absent", userRow, flushed)
	}
}

func TestRuntimeScopedToolCompletionProjectsLiveAndHydratedWithoutExactStep(t *testing.T) {
	result := tools.Result{
		CallID: "call-runtime",
		Name:   toolspec.ToolExecCommand,
		Output: json.RawMessage(`{"output":"done"}`),
	}
	messages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind:                runtime.EventToolCallCompleted,
		ToolResult:          &result,
		CommittedProvenance: &runtime.TranscriptCommittedRowProvenance{EventSequence: 2},
	})
	if err != nil {
		t.Fatalf("project Runtime tool completion: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("Runtime tool completion messages = %+v, want one committed row", messages)
	}
	assertTranscriptRoundTrip(t, &transcriptpb.Message{Sequence: 2, Event: messages[0]})
	liveRow := messages[0].GetCommittedRow()
	if liveRow.GetTool() == nil || liveRow.GetTool().StepId != nil {
		t.Fatalf("Runtime live tool Step provenance = %+v, want absent", liveRow.GetTool())
	}

	hydration := mustTranscriptHydration(t, runtime.TranscriptHydrationSnapshot{
		CommittedRows: []runtime.TranscriptCommittedRowFact{
			{
				Visibility: transcript.EntryVisibilityOngoing,
				Integrity:  transcript.RowIntegrityValid,
				Kind:       runtime.TranscriptCommittedRowFactUser,
				Locator:    transcript.CommittedRowLocator{EventSequence: 3, RowOrdinal: 1},
				Provenance: &runtime.TranscriptCommittedRowProvenance{EventSequence: 3},
				User:       &runtime.TranscriptUserRowFact{Text: "idle input"},
			},
			{
				Visibility: transcript.EntryVisibilityOngoingCollapsed,
				Integrity:  transcript.RowIntegrityValid,
				Kind:       runtime.TranscriptCommittedRowFactTool,
				Locator:    transcript.CommittedRowLocator{EventSequence: 4, RowOrdinal: 1},
				Provenance: &runtime.TranscriptCommittedRowProvenance{EventSequence: 4},
				Tool: &runtime.TranscriptToolRowFact{
					ToolCallID: "call-runtime",
					ToolName:   string(toolspec.ToolExecCommand),
					Text:       "done",
				},
			},
		},
	})
	if len(hydration.TailSegment.Entries) != 2 {
		t.Fatalf("Runtime hydration rows = %+v, want user and tool", hydration.TailSegment.Entries)
	}
	hydration.SessionIdentity = &transcriptpb.SessionIdentity{
		SessionId:             projectionWorkspaceID,
		ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED,
	}
	hydration.SessionStatus = &transcriptpb.SessionStatus{
		ReviewerFrequency: "off",
		ThinkingLevel:     "medium",
		CompactionMode:    "local",
	}
	hydration.RuntimeReadModelUpdate = &runtimepb.ReadModelUpdate{
		Version: &runtimepb.ReadModelVersion{Epoch: "runtime-scoped-hydration", Generation: 1, Sequence: 1},
		Activity: &runtimepb.Activity{
			State:          runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE,
			Reviewer:       runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE,
			QueueAccepting: true,
		},
	}
	assertTranscriptRoundTrip(t, &transcriptpb.Message{
		Sequence: 1,
		Event:    &transcriptpb.Event{Payload: &transcriptpb.Event_Hydration{Hydration: hydration}},
	})
	for index := range hydration.TailSegment.Entries {
		if err := protoapi.Validate(hydration.TailSegment.Entries[index]); err != nil {
			t.Fatalf("Runtime hydration row %d failed validation: %v", index, err)
		}
	}
	if hydration.TailSegment.Entries[0].GetUser() == nil || hydration.TailSegment.Entries[0].GetUser().StepId != nil ||
		hydration.TailSegment.Entries[1].GetTool() == nil || hydration.TailSegment.Entries[1].GetTool().StepId != nil {
		t.Fatalf("Runtime hydration Step provenance = %+v, want absent", hydration.TailSegment.Entries)
	}
}

func TestTranscriptCommittedReasoningEventCarriesDedicatedPayload(t *testing.T) {
	part := int64(0)
	durationMs := int64(321)
	messages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind:   runtime.EventLocalEntryAdded,
		StepID: runtimeStepIDPointer(transcriptProjectionStepID),
		LocalEntry: &runtime.ChatEntry{
			Visibility: transcript.EntryVisibilityDetail,
			Role:       string(transcript.EntryRoleReasoning),
			Text:       "**Planning\nDetails**",
			DurationMs: &durationMs,
		},
		ReasoningTraceIdentity: &runtime.TranscriptReasoningTraceIdentity{
			Provider: &llm.ReasoningItemIdentity{ItemID: "reason_1", PartIndex: &part},
		},
		CommittedProvenance: &runtime.TranscriptCommittedRowProvenance{EventSequence: 1},
	})
	if err != nil {
		t.Fatalf("project reasoning row: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("reasoning messages = %+v, want one committed row", messages)
	}
	row := messages[0].GetCommittedRow()
	if row.GetReasoningTrace() == nil ||
		row.GetReasoningTrace().Text != "Planning\nDetails" ||
		row.GetReasoningTrace().CompactText != "Planning" ||
		row.GetReasoningTrace().Duration == nil ||
		row.GetReasoningTrace().Duration.AsDuration().Milliseconds() != durationMs ||
		row.GetReasoningTrace().ProvisionalIdentity == nil {
		t.Fatalf("projected reasoning row = %+v", row)
	}
	if err := protoapi.Validate(row); err != nil {
		t.Fatalf("projected reasoning row failed validation: %v", err)
	}
}

func TestTranscriptReasoningDurationProjectsHydrationAndBoundedPage(t *testing.T) {
	durationMs := int64(321)
	fact := runtime.TranscriptCommittedRowFact{
		Visibility: transcript.EntryVisibilityOngoing,
		Integrity:  transcript.RowIntegrityValid,
		StepID:     runtimeStepIDPointer(transcriptProjectionStepID),
		Kind:       runtime.TranscriptCommittedRowFactReasoningTrace,
		Locator:    transcript.CommittedRowLocator{EventSequence: 1, RowOrdinal: 1},
		ReasoningTrace: &runtime.TranscriptReasoningTraceRowFact{
			Text:        "Planning\nDetails",
			CompactText: "Planning",
			DurationMs:  &durationMs,
		},
	}
	hydration := mustTranscriptHydration(t, runtime.TranscriptHydrationSnapshot{
		CommittedRows: []runtime.TranscriptCommittedRowFact{fact},
	})
	if len(hydration.TailSegment.Entries) != 1 || hydration.TailSegment.Entries[0].GetReasoningTrace() == nil ||
		hydration.TailSegment.Entries[0].GetReasoningTrace().Duration == nil ||
		hydration.TailSegment.Entries[0].GetReasoningTrace().Duration.AsDuration().Milliseconds() != durationMs {
		t.Fatalf("hydrated reasoning duration = %+v", hydration.TailSegment.Entries)
	}

	page, err := TranscriptPageFromSegment(
		"58e121b5-30f7-4d0f-a1fa-fb3e6695e39c",
		"name",
		runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED,
		runtime.TranscriptSegmentPage{Snapshot: runtime.ChatSnapshot{Entries: []runtime.ChatEntry{{
			StepID:     runtimeStepIDPointer(transcriptProjectionStepID),
			Role:       string(transcript.EntryRoleReasoning),
			Text:       "Planning\nDetails",
			DurationMs: &durationMs,
			CommittedProvenance: &runtime.TranscriptCommittedRowProvenance{
				EventSequence: 1,
			},
		}}}},
	)
	if err != nil {
		t.Fatalf("project bounded transcript page: %v", err)
	}
	if len(page.Entries) != 1 || page.Entries[0].GetReasoningTrace() == nil ||
		page.Entries[0].GetReasoningTrace().Duration == nil ||
		page.Entries[0].GetReasoningTrace().Duration.AsDuration().Milliseconds() != durationMs {
		t.Fatalf("paged reasoning duration = %+v", page.Entries)
	}
}

func TestUnknownToolExecutionProjectsFinalizedFailedInput(t *testing.T) {
	input := json.RawMessage(`{}`)
	call := llm.ToolCall{
		ID:    "call-unknown",
		Name:  "final_answer",
		Input: input,
	}
	client := scriptedllm.NewClient(scriptedllm.Script{
		Steps: []scriptedllm.Step{
			scriptedllm.ToolBatch("", call),
			{
				ExpectedToolResults: []scriptedllm.ExpectedToolResult{{
					CallID: call.ID,
					Name:   call.Name,
				}},
				Response: scriptedllm.FinalAnswer("done").Response,
			},
		},
	})
	store := newRuntimeViewStore(t)
	var completion runtime.Event
	engine := newRuntimeViewEngine(t, store, client, runtime.Config{
		Model: "gpt-5",
		OnEvent: func(evt runtime.Event) {
			if evt.Kind == runtime.EventToolCallCompleted && evt.ToolResult != nil {
				completion = evt
			}
		},
	})
	if _, err := engine.SubmitUserMessage(context.Background(), "exercise unknown tool"); err != nil {
		t.Fatalf("submit user message: %v", err)
	}
	if remaining := client.RemainingSteps(); remaining != 0 {
		t.Fatalf("scripted LLM steps remaining = %d, want continuation after tool result consumed", remaining)
	}
	if completion.ToolResult == nil || !completion.ToolResult.IsError || completion.ToolResult.Presentation == nil {
		t.Fatalf("emitted completion = %+v, want finalized failed result", completion.ToolResult)
	}
	if completion.ToolResult.Presentation.Command != string(input) ||
		completion.ToolResult.Presentation.CompactText != string(input) {
		t.Fatalf("emitted presentation = %+v, want original input preserved", completion.ToolResult.Presentation)
	}

	messages, err := TranscriptMessagesFromRuntimeEventChecked(completion)
	if err != nil {
		t.Fatalf("project failed tool completion: %v", err)
	}
	var row *transcriptpb.CommittedRow
	for _, message := range messages {
		if message.GetCommittedRow() != nil {
			candidate := message.GetCommittedRow()
			if candidate.GetTool() == nil {
				continue
			}
			if row != nil {
				t.Fatalf("client transcript messages = %+v, want exactly one committed tool row", messages)
			}
			row = candidate
		}
	}
	if row == nil {
		t.Fatalf("client transcript messages = %+v, want one committed tool row", messages)
	}
	if row.GetTool() == nil || !row.GetTool().IsError {
		t.Fatalf("client transcript row = %+v, want failed tool row", row)
	}
	if row.GetTool().Presentation == nil ||
		row.GetTool().Presentation.GetCommand() != string(input) ||
		row.GetTool().Presentation.GetCompactText() != string(input) {
		t.Fatalf("projected tool presentation = %+v, want original input preserved", row.GetTool().Presentation)
	}
}

func TestWebSearchLiveAndReopenedPage(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      string
		details     string
		wantDetail  bool
		wantFailure bool
	}{
		{"populated", "completed", `,"results":[{"title":"Example","url":"https://example.com","snippet":"excluded"}]`, true, false},
		{"absent", "completed", "", false, false},
		{"malformed", "completed", `,"results":[{"url":42}]`, false, true},
		{"failed", "failed", `,"results":[{"type":"text_result","title":"Example","url":"https://example.com","snippet":"excluded"}],"error":{"message":"search timed out","type":"provider_error"}`, false, true},
		{"failed without diagnostic", "failed", `,"results":[{"type":"text_result","snippet":"excluded"}]`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := json.RawMessage(`{"type":"web_search_call","id":"ws_1","status":"` + tc.status + `","action":{"type":"search","query":"example","queries":["example"]}` + tc.details + `}`)
			caps := scriptedllm.DefaultProviderCapabilities()
			caps.SupportsNativeWebSearch = true
			client := scriptedllm.NewClient(scriptedllm.Script{
				Capabilities: &caps,
				Steps: []scriptedllm.Step{
					{Response: llm.Response{Assistant: llm.Message{Role: llm.RoleAssistant}, OutputItems: []llm.ResponseItem{{Type: llm.ResponseItemTypeOther, Raw: raw}}}},
					scriptedllm.FinalAnswer("done"),
				},
			})
			store := newRuntimeViewStore(t)
			var completion runtime.Event
			engine := newRuntimeViewEngine(t, store, client, runtime.Config{
				Model: "gpt-5", WebSearchMode: "native", EnabledTools: []toolspec.ID{toolspec.ToolWebSearch},
				OnEvent: func(event runtime.Event) {
					if event.Kind == runtime.EventToolCallCompleted {
						completion = event
					}
				},
			})
			if _, err := engine.SubmitUserMessage(context.Background(), "search example"); err != nil {
				t.Fatal(err)
			}
			events, err := TranscriptMessagesFromRuntimeEventChecked(completion)
			if err != nil {
				t.Fatal(err)
			}
			var live *transcriptpb.ToolRow
			for _, event := range events {
				assertTranscriptRoundTrip(t, &transcriptpb.Message{Sequence: 2, Event: event})
				if row := event.GetCommittedRow(); row != nil {
					live = row.GetTool()
				}
			}
			if live == nil || (live.WebSearch != nil) != tc.wantDetail || live.IsError != tc.wantFailure {
				t.Fatalf("incorrect live projection: %+v", live)
			}
			if !tc.wantFailure && live.Text != "" {
				t.Fatalf("raw output exposed: %q", live.Text)
			}
			if tc.status == "failed" {
				var payload struct {
					Error *struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				if err := json.Unmarshal(raw, &payload); err != nil {
					t.Fatal(err)
				}
				if payload.Error == nil && live.Text != "" ||
					payload.Error != nil && live.Text != payload.Error.Message {
					t.Fatalf("failure output must contain only the supplied diagnostic: %q", live.Text)
				}
			}
			if err := engine.Close(); err != nil {
				t.Fatal(err)
			}
			reopened := newRuntimeViewEngine(t, store, scriptedllm.NewClient(scriptedllm.Script{}))
			segment, err := reopened.TranscriptNewestSegmentPage()
			if err != nil {
				t.Fatal(err)
			}
			page, err := TranscriptPageFromSegment(projectionWorkspaceID, "search", runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, segment)
			if err != nil {
				t.Fatal(err)
			}
			if err := protoapi.Validate(page); err != nil {
				t.Fatal(err)
			}
			encoded, err := proto.Marshal(page)
			if err != nil {
				t.Fatal(err)
			}
			decoded := new(transcriptpb.Page)
			if err := proto.Unmarshal(encoded, decoded); err != nil {
				t.Fatal(err)
			}
			if err := protoapi.Validate(decoded); err != nil {
				t.Fatal(err)
			}
			for _, row := range decoded.Entries {
				if tool := row.GetTool(); tool != nil && tool.GetToolCallId() == "ws_1" {
					if !proto.Equal(live, tool) {
						t.Fatalf("live/history differ: %v / %v", live, tool)
					}
					return
				}
			}
			t.Fatal("saved search missing from bounded page")
		})
	}
}

func TestTranscriptPagePreservesRollbackTargetIdentity(t *testing.T) {
	targetID := rollbacktarget.EncodeUserMessageSeq(91)
	locator := &rollbacktarget.CandidateLocator{
		UserMessageSeq:       91,
		CandidatePageEndByte: 2048,
	}
	page, err := TranscriptPageFromSegment(
		"58e121b5-30f7-4d0f-a1fa-fb3e6695e39c",
		"name",
		runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED,
		runtime.TranscriptSegmentPage{
			LatestRollbackCandidate: locator,
			Snapshot: runtime.ChatSnapshot{Entries: []runtime.ChatEntry{{
				Role:             "user",
				StepID:           runtimeStepIDPointer(transcriptProjectionStepID),
				Text:             "persisted user prompt",
				RollbackTargetID: &targetID,
				CommittedProvenance: &runtime.TranscriptCommittedRowProvenance{
					EventSequence: 5,
				},
			}}},
		},
	)
	if err != nil {
		t.Fatalf("project page: %v", err)
	}

	if len(page.Entries) != 1 || page.Entries[0].GetUser() == nil {
		t.Fatalf("transcript page rows = %#v, want one user row", page.Entries)
	}
	if got := page.Entries[0].GetUser().RollbackTargetId; got == nil || *got != string(targetID) {
		t.Fatalf("projected rollback target = %v, want exact target %q", got, targetID)
	}
	if !proto.Equal(page.LatestRollbackCandidate, &transcriptpb.RollbackCandidate{
		UserMessageSeq:       locator.UserMessageSeq,
		CandidatePageEndByte: locator.CandidatePageEndByte,
	}) {
		t.Fatalf("projected rollback candidate locator = %#v, want %#v", page.LatestRollbackCandidate, locator)
	}
}

func TestTranscriptProjectionCanonicalizesBlankPersistedAssistantPhase(t *testing.T) {
	hydration := mustTranscriptHydration(t, runtime.TranscriptHydrationSnapshot{
		CommittedRows: []runtime.TranscriptCommittedRowFact{{
			Visibility: transcript.EntryVisibilityOngoing,
			Integrity:  transcript.RowIntegrityValid,
			StepID:     runtimeStepIDPointer(transcriptProjectionStepID),
			Kind:       runtime.TranscriptCommittedRowFactAssistant,
			Locator:    transcript.CommittedRowLocator{EventSequence: 3, RowOrdinal: 1},
			Assistant: &runtime.TranscriptAssistantRowFact{
				Text: "legacy final answer",
			},
		}},
	})
	if len(hydration.TailSegment.Entries) != 1 || hydration.TailSegment.Entries[0].GetAssistant() == nil {
		t.Fatalf("hydration rows = %+v, want one assistant row", hydration.TailSegment.Entries)
	}
	if got := hydration.TailSegment.Entries[0].GetAssistant().Phase; got != transcriptpb.AssistantPhase_ASSISTANT_PHASE_FINAL {
		t.Fatalf("persisted assistant phase = %q, want canonical final phase", got)
	}
}

func TestTranscriptPageProjectsReviewerAndBackgroundMetadata(t *testing.T) {
	exitCode := 9
	activityID := uuid.New()
	page, err := TranscriptPageFromSegment("58e121b5-30f7-4d0f-a1fa-fb3e6695e39c", "name", runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED, runtime.TranscriptSegmentPage{
		Snapshot: runtime.ChatSnapshot{Entries: []runtime.ChatEntry{
			{Role: "reviewer_status", Text: "review complete", CommittedProvenance: &runtime.TranscriptCommittedRowProvenance{EventSequence: 6}},
			{
				Role:                 "system",
				Text:                 "background failed",
				MessageType:          llm.MessageTypeBackgroundNotice,
				BackgroundActivityID: activityID.String(),
				BackgroundProcessID:  "process-1",
				BackgroundExitCode:   &exitCode,
				CommittedProvenance:  &runtime.TranscriptCommittedRowProvenance{EventSequence: 7},
			},
		}},
	})
	if err != nil {
		t.Fatalf("project page: %v", err)
	}

	if len(page.Entries) != 2 {
		t.Fatalf("page entries = %+v", page.Entries)
	}
	reviewerStatus := page.Entries[0]
	if reviewerStatus.Visibility != transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED ||
		reviewerStatus.GetNotice() == nil ||
		reviewerStatus.GetNotice().Diagnostic == nil ||
		reviewerStatus.GetNotice().Diagnostic.Code != string(transcript.EntryRoleReviewerStatus) {
		t.Fatalf("reviewer status row = %+v, want collapsed reviewer status notice", reviewerStatus)
	}
	backgroundRow := page.Entries[1]
	if backgroundRow.GetNotice() == nil {
		t.Fatalf("background row = %+v, want notice", backgroundRow)
	}
	if background := backgroundRow.GetNotice().Background; background == nil || background.ExitCode == nil || *background.ExitCode != int32(exitCode) {
		t.Fatalf("background notice = %+v, want exit code %d", background, exitCode)
	}
}

func TestTranscriptHydrationRejectsAssistantStreamWithoutRuntimeIdentity(t *testing.T) {
	_, err := TranscriptHydrationFromSnapshotChecked(runtime.TranscriptHydrationSnapshot{
		ActiveAssistantText:     "hello",
		ActiveAssistantMetadata: &runtime.AssistantStreamMetadata{StepID: transcriptProjectionStepID},
	}, &transcriptpb.TailSegment{})
	if err == nil {
		t.Fatal("expected missing assistant stream identity to be rejected")
	}
}

func TestTranscriptMessagesIgnoreEmptyAssistantDelta(t *testing.T) {
	streamID := uuid.New()
	messages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind:                        runtime.EventAssistantDelta,
		AssistantDelta:              "",
		AssistantTranscriptStreamID: &streamID,
	})
	if err != nil {
		t.Fatalf("project empty assistant delta: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("empty assistant delta messages = %+v, want none", messages)
	}
}

func TestTranscriptMessagesIgnoreNoopAssistantResetWithoutStream(t *testing.T) {
	messages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind:                       runtime.EventAssistantDeltaReset,
		AssistantStreamAbortReason: string(runtime.AssistantStreamAbortSuperseded),
	})
	if err != nil {
		t.Fatalf("project noop assistant reset: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("noop assistant reset messages = %+v, want none", messages)
	}
}

func TestTranscriptMessagesIgnoreFinalizedAssistantReset(t *testing.T) {
	streamID := uuid.New()
	messages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind:                        runtime.EventAssistantDeltaReset,
		StepID:                      runtimeStepIDPointer(transcriptProjectionStepID),
		AssistantTranscriptStreamID: &streamID,
	})
	if err != nil {
		t.Fatalf("project finalized assistant reset: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("finalized assistant reset messages = %+v, want committed assistant row to remain the sole terminal", messages)
	}
}

func TestTranscriptBackgroundActivityUsesRuntimeActivityID(t *testing.T) {
	activityID := uuid.New()
	messages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind:   runtime.EventBackgroundUpdated,
		StepID: runtimeStepIDPointer(transcriptProjectionStepID),
		Background: &runtime.BackgroundShellEvent{
			Type:        runtime.BackgroundShellEventBackgrounded,
			ID:          uuid.NewString(),
			ActivityID:  activityID,
			OwnerRunID:  transcriptProjectionRunID,
			OwnerStepID: transcriptProjectionStepID,
			State:       "running",
			Command:     "go test ./...",
			Workdir:     "/tmp/workspace",
			Preview:     "tests",
		},
	})
	if err != nil {
		t.Fatalf("project background activity: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %+v, want one background activity", messages)
	}
	background := messages[0].GetBackgroundActivity()
	if got := background.ActivityId; got != activityID.String() {
		t.Fatalf("background transcript id = %q, want activity id %q", got, activityID)
	}
}

func TestTranscriptBackgroundActivityLifecycleIgnoresPreviewTruncation(t *testing.T) {
	tests := []struct {
		name           string
		eventType      runtime.BackgroundShellEventType
		previewRemoved int
		wantLifecycle  transcriptpb.BackgroundLifecycle
	}{
		{name: "running truncated preview remains live", eventType: runtime.BackgroundShellEventBackgrounded, previewRemoved: 2, wantLifecycle: transcriptpb.BackgroundLifecycle_BACKGROUND_LIFECYCLE_BACKGROUNDED},
		{name: "completed activity is terminal", eventType: runtime.BackgroundShellEventCompleted, wantLifecycle: transcriptpb.BackgroundLifecycle_BACKGROUND_LIFECYCLE_COMPLETED},
		{name: "killed activity is terminal", eventType: runtime.BackgroundShellEventKilled, wantLifecycle: transcriptpb.BackgroundLifecycle_BACKGROUND_LIFECYCLE_KILLED},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
				Kind:   runtime.EventBackgroundUpdated,
				StepID: runtimeStepIDPointer(transcriptProjectionStepID),
				Background: &runtime.BackgroundShellEvent{
					Type:           tt.eventType,
					ID:             uuid.NewString(),
					ActivityID:     uuid.New(),
					OwnerRunID:     transcriptProjectionRunID,
					OwnerStepID:    transcriptProjectionStepID,
					State:          string(tt.eventType),
					Command:        "sleep 2",
					Workdir:        "/tmp/workspace",
					PreviewRemoved: tt.previewRemoved,
				},
			})
			if err != nil {
				t.Fatalf("project background activity: %v", err)
			}
			if len(messages) != 1 {
				t.Fatalf("messages = %+v, want one background activity", messages)
			}
			background := messages[0].GetBackgroundActivity()
			if got := background.Lifecycle; got != tt.wantLifecycle {
				t.Fatalf("background activity lifecycle = %q, want %q", got, tt.wantLifecycle)
			}
		})
	}
}

func TestTranscriptBackgroundNoticeCarriesTypedExitCode(t *testing.T) {
	exitCode := 3
	activityID := uuid.New()
	messages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind:   runtime.EventConversationUpdated,
		StepID: runtimeStepIDPointer(transcriptProjectionStepID),
		CommittedProvenance: &runtime.TranscriptCommittedRowProvenance{
			EventSequence: 1,
		},
		Message: llm.Message{
			Role:                 llm.RoleDeveloper,
			Name:                 textutil.Value("process-1"),
			MessageType:          textutil.Value(llm.MessageTypeBackgroundNotice),
			Content:              textutil.Value("background failed"),
			CompactContent:       textutil.Value("background failed"),
			BackgroundActivityID: textutil.Value(activityID.String()),
			BackgroundExitCode:   &exitCode,
		},
	})
	if err != nil {
		t.Fatalf("project background notice: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("messages = %+v, want one background notice", messages)
	}
	row := messages[0].GetCommittedRow()
	if row.GetNotice() == nil {
		t.Fatal("background notice row is missing notice payload")
	}
	background := row.GetNotice().Background
	if background == nil || background.ExitCode == nil || *background.ExitCode != int32(exitCode) {
		t.Fatalf("background notice = %+v, want exit code %d", background, exitCode)
	}
}

func TestTranscriptWorktreeNoticeCarriesTypedContextWithoutServerPresentation(t *testing.T) {
	target := session.WorktreeReminderState{
		Mode: session.WorktreeReminderModeEnter,
		WorktreeContext: session.WorktreeContext{
			Branch:        session.OptionalWorktreeBranch("feature/transcript"),
			WorktreePath:  "/tmp/worktree",
			WorkspaceRoot: "/tmp/workspace",
			EffectiveCwd:  "/tmp/worktree/pkg",
		},
	}
	messages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind: runtime.EventConversationUpdated,
		CommittedProvenance: &runtime.TranscriptCommittedRowProvenance{
			EventSequence: 2,
		},
		Message: llm.Message{
			Role:            llm.RoleDeveloper,
			MessageType:     textutil.Value(llm.MessageTypeWorktreeMode),
			SourcePath:      textutil.Value(target.EffectiveCwd),
			WorktreeContext: &target.WorktreeContext,
			Content:         textutil.Value("model-visible worktree context"),
		},
	})
	if err != nil {
		t.Fatalf("project worktree notice: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("messages = %+v, want one worktree notice", messages)
	}
	row := messages[0].GetCommittedRow()
	if row.GetNotice() == nil {
		t.Fatal("worktree notice row is missing notice payload")
	}
	notice := row.GetNotice()
	if notice.Worktree == nil {
		t.Fatal("worktree transcript context is missing")
	}
	wantContext := &transcriptpb.WorktreeContext{
		Branch:        target.Branch,
		WorktreePath:  target.WorktreePath,
		WorkspaceRoot: target.WorkspaceRoot,
		EffectiveCwd:  target.EffectiveCwd,
	}
	if !proto.Equal(notice.Worktree, wantContext) {
		t.Fatalf("worktree transcript context = %+v, want %+v", notice.Worktree, wantContext)
	}
	if notice.CondensedText != nil || notice.CompactLabel != nil {
		t.Fatalf("server-authored worktree presentation leaked into client contract: %+v", notice)
	}
}

func TestTranscriptWorktreeNoticeKeepsMissingBranchNullable(t *testing.T) {
	context := session.WorktreeContext{
		WorktreePath:  "/tmp/detached-worktree",
		WorkspaceRoot: "/tmp/workspace",
		EffectiveCwd:  "/tmp/detached-worktree",
	}
	messages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind: runtime.EventConversationUpdated,
		CommittedProvenance: &runtime.TranscriptCommittedRowProvenance{
			EventSequence: 3,
		},
		Message: llm.Message{
			Role:            llm.RoleDeveloper,
			MessageType:     textutil.Value(llm.MessageTypeWorktreeMode),
			WorktreeContext: &context,
			Content:         textutil.Value("model-visible detached worktree context"),
		},
	})
	if err != nil {
		t.Fatalf("project detached worktree notice: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %+v, want one typed worktree notice", messages)
	}
	row := messages[0].GetCommittedRow()
	if row.GetNotice() == nil || row.GetNotice().Worktree == nil {
		t.Fatal("typed worktree notice is missing projected context")
	}
	if branch := row.GetNotice().Worktree.Branch; branch != nil {
		t.Fatalf("projected detached worktree branch = %v, want null", branch)
	}
}

func TestTranscriptBackgroundActivityRejectsMissingRuntimeActivityID(t *testing.T) {
	_, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind:   runtime.EventBackgroundUpdated,
		StepID: runtimeStepIDPointer(transcriptProjectionStepID),
		Background: &runtime.BackgroundShellEvent{
			Type:        runtime.BackgroundShellEventBackgrounded,
			ID:          uuid.NewString(),
			OwnerRunID:  transcriptProjectionRunID,
			OwnerStepID: transcriptProjectionStepID,
			State:       "running",
			Command:     "go test ./...",
			Workdir:     "/tmp/workspace",
			Preview:     "tests",
		},
	})
	if err == nil {
		t.Fatal("expected missing background activity id to be rejected")
	}
}

func TestAssistantTranscriptMessagesDoNotReemitLiveToolStarts(t *testing.T) {
	for _, kind := range []runtime.EventKind{runtime.EventAssistantMessage, runtime.EventConversationUpdated} {
		t.Run(string(kind), func(t *testing.T) {
			messages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
				Kind:   kind,
				StepID: runtimeStepIDPointer(transcriptProjectionStepID),
				CommittedProvenance: &runtime.TranscriptCommittedRowProvenance{
					EventSequence: 4,
				},
				Message: llm.Message{
					Role:    llm.RoleAssistant,
					Content: textutil.Value("checking the repo"),
					Phase:   textutil.Value(llm.MessagePhaseCommentary),
					ToolCalls: []llm.ToolCall{{
						ID:   "call-1",
						Name: "shell",
					}},
				},
			})
			if err != nil {
				t.Fatalf("project assistant transcript event: %v", err)
			}
			if len(messages) != 1 {
				t.Fatalf("messages = %+v, want only assistant committed row", messages)
			}
			if messages[0].GetCommittedRow() == nil {
				t.Fatalf("message = %+v, want assistant committed row", messages[0])
			}
			row := messages[0].GetCommittedRow()
			if row.GetAssistant() == nil {
				t.Fatalf("message = %+v, want assistant committed row", messages[0])
			}
		})
	}
}

func TestInFlightClearFailureIsOperationalDiagnosticOnly(t *testing.T) {
	event := runtime.Event{
		Kind:   runtime.EventInFlightClearFailed,
		Error:  "clear in-flight state failed",
		StepID: runtimeStepIDPointer(transcriptProjectionStepID),
	}
	if facts := runtime.TranscriptCommittedRowFactsFromEvent(event); len(facts) != 0 {
		t.Fatalf("in-flight clear failure committed facts = %+v, want none", facts)
	}
	messages, err := TranscriptMessagesFromRuntimeEventChecked(event)
	if err != nil {
		t.Fatalf("project in-flight clear failure: %v", err)
	}
	if len(messages) != 1 || messages[0].GetOperationalDiagnostic() == nil {
		t.Fatalf("in-flight clear failure messages = %+v, want one operational diagnostic", messages)
	}
	diagnostic := messages[0].GetOperationalDiagnostic()
	if diagnostic.Code != transcriptpb.OperationalDiagnosticCode_OPERATIONAL_DIAGNOSTIC_CODE_IN_FLIGHT_CLEAR_FAILED {
		t.Fatalf("diagnostic code = %q, want %q", diagnostic.Code, transcriptpb.OperationalDiagnosticCode_OPERATIONAL_DIAGNOSTIC_CODE_IN_FLIGHT_CLEAR_FAILED)
	}
}

func TestContextFactPersistenceFailureIsOperationalDiagnosticOnly(t *testing.T) {
	event := runtime.Event{
		Kind:   runtime.EventContextFactsPersistFailed,
		StepID: textutil.Value(transcriptProjectionStepID),
		Error:  "persist Session Context manual Compact eligibility: disk full",
	}
	if facts := runtime.TranscriptCommittedRowFactsFromEvent(event); len(facts) != 0 {
		t.Fatalf("Context-fact persistence failure committed facts = %+v, want none", facts)
	}
	messages, err := TranscriptMessagesFromRuntimeEventChecked(event)
	if err != nil {
		t.Fatalf("project context-fact persistence failure: %v", err)
	}
	if len(messages) != 1 || messages[0].GetOperationalDiagnostic() == nil {
		t.Fatalf("Context-fact persistence failure messages = %+v, want one operational diagnostic", messages)
	}
	diagnostic := messages[0].GetOperationalDiagnostic()
	if diagnostic.Code != transcriptpb.OperationalDiagnosticCode_OPERATIONAL_DIAGNOSTIC_CODE_CONTEXT_FACTS_PERSIST_FAILED ||
		diagnostic.Detail == "" ||
		diagnostic.StepId == nil ||
		*diagnostic.StepId != transcriptProjectionStepID {
		t.Fatalf("Context-fact persistence diagnostic = %+v", diagnostic)
	}
}
