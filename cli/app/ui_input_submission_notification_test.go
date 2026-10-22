package app

import (
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"testing"
)

func TestManualCompactionNotificationWaitsForTerminalTranscriptOutcome(t *testing.T) {
	ringer := &countRinger{}
	hooks := newUnfocusedBellHooks(ringer)
	model := newProjectedStaticUIModel(WithUITurnQueueHook(newTurnQueueHooks(hooks, nil)))
	next, _ := model.Update(compactDoneMsg{})
	model = next.(*uiModel)
	if ringer.total() != 0 {
		t.Fatalf("compaction scheduling emitted %d notification events before terminal outcome", ringer.total())
	}

	for sequence, status := range []*transcriptpb.CompactionStatus{
		{StepId: ongoingTestStepID().String(),
			State: transcriptpb.CompactionState_COMPACTION_STATE_STARTED,
			Mode:  transcriptpb.CompactionMode_COMPACTION_MODE_MANUAL,
			Count: 1},
		{StepId: ongoingTestStepID().String(),
			State: transcriptpb.CompactionState_COMPACTION_STATE_COMPLETED,
			Mode:  transcriptpb.CompactionMode_COMPACTION_MODE_AUTO, Count: 1}, {StepId: ongoingTestStepID().String(), State: transcriptpb.CompactionState_COMPACTION_STATE_FAILED, Mode: transcriptpb.CompactionMode_COMPACTION_MODE_MANUAL,
			Count: 1,
			Diagnostic: &transcriptpb.Diagnostic{
				Code:   "compaction_failed",
				Detail: "provider failed"}}} {
		model.applyAdmittedTranscriptMessageState(transcriptTestMessage(uint64(sequence+2), status), runtimeTupleMergeResult{})
	}
	if ringer.total() != 0 {
		t.Fatalf("non-success manual outcomes emitted %d notification events", ringer.total())
	}

	model.applyAdmittedTranscriptMessageState(transcriptTestMessage(2, &transcriptpb.CompactionStatus{StepId: ongoingTestStepID().String(),
		State: transcriptpb.CompactionState_COMPACTION_STATE_COMPLETED,
		Mode:  transcriptpb.CompactionMode_COMPACTION_MODE_MANUAL,
		Count: 1}), runtimeTupleMergeResult{})
	if ringer.notifications != 1 {
		t.Fatalf("terminal manual compaction emitted %d notifications, want 1", ringer.notifications)
	}
}

func TestManualCompactionTerminalEventNotifiesAllAttachedUnfocusedTUIs(t *testing.T) {
	initiatorRinger := &countRinger{}
	observerRinger := &countRinger{}
	initiator := newProjectedStaticUIModel(WithUITurnQueueHook(newTurnQueueHooks(newUnfocusedBellHooks(initiatorRinger), nil)))
	observer := newProjectedStaticUIModel(WithUITurnQueueHook(newTurnQueueHooks(newUnfocusedBellHooks(observerRinger), nil)))

	event := transcriptTestMessage(1, &transcriptpb.CompactionStatus{StepId: ongoingTestStepID().String(),
		State: transcriptpb.CompactionState_COMPACTION_STATE_COMPLETED,
		Mode:  transcriptpb.CompactionMode_COMPACTION_MODE_MANUAL,
		Count: 1})
	initiator.applyAdmittedTranscriptMessageState(event, runtimeTupleMergeResult{})
	observer.applyAdmittedTranscriptMessageState(event, runtimeTupleMergeResult{})

	if initiatorRinger.notifications != 1 {
		t.Fatalf("initiating TUI notifications = %d, want 1", initiatorRinger.notifications)
	}
	if observerRinger.notifications != 1 {
		t.Fatalf("observing TUI notifications = %d, want 1", observerRinger.notifications)
	}
}
