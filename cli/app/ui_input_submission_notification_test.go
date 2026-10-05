package app

import (
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	textutil "core/shared/textutil"
	"testing"
)

func TestSubmitDoneDispatchesQueuedTurnWithoutNotificationTranscriptFacts(t *testing.T) {
	ringer := &countRinger{}
	hooks := newUnfocusedBellHooks(ringer)
	hooks.OnTranscriptMessage(bellToolStartMessage(1))
	hooks.OnTranscriptMessage(bellToolStartMessage(1))

	model := newProjectedStaticUIModel(WithUITurnQueueHook(hooks))
	model.activeSubmit = activeSubmitState{token: 1, text: "current turn"}
	model.setRuntimeActivityBusyForTest(true)
	model.queued = queuedInputsForTest("next queued turn")

	next, cmd := model.Update(submitDoneMsg{token: 1, submittedText: "current turn", message: "done"})
	updated := next.(*uiModel)

	if cmd == nil {
		t.Fatal("submit completion did not dispatch the next queued turn")
	}
	if got := updated.activeSubmit.text; got != "next queued turn" {
		t.Fatalf("active submission = %q, want queued turn", got)
	}
	if len(updated.queued) != 0 {
		t.Fatalf("queued turns after dispatch = %+v, want empty", updated.queued)
	}
	if ringer.total() != 0 {
		t.Fatalf("queued dispatch emitted %d notifications without final transcript facts", ringer.total())
	}
}

func TestManualCompactionNotificationWaitsForTerminalTranscriptOutcome(t *testing.T) {
	ringer := &countRinger{}
	hooks := newUnfocusedBellHooks(ringer)
	model := newProjectedStaticUIModel(WithUITurnQueueHook(hooks))
	requestID := runtimeids.NewCompactionRequestID()
	otherRequestID := runtimeids.NewCompactionRequestID()
	model.registerPendingCompactionRequest(requestID)

	next, _ := model.Update(compactDoneMsg{requestID: requestID})
	model = next.(*uiModel)
	if ringer.total() != 0 {
		t.Fatalf("compaction scheduling emitted %d notification events before terminal outcome", ringer.total())
	}

	for sequence, status := range []*transcriptpb.CompactionStatus{
		{StepId: ongoingTestStepID().String(),
			State: transcriptpb.CompactionState_COMPACTION_STATE_STARTED,
			Mode:  transcriptpb.CompactionMode_COMPACTION_MODE_MANUAL,
			Count: 1, RequestId: textutil.Value((&requestID).String())},
		{StepId: ongoingTestStepID().String(),
			State: transcriptpb.CompactionState_COMPACTION_STATE_COMPLETED,
			Mode:  transcriptpb.CompactionMode_COMPACTION_MODE_AUTO, Count: 1}, {StepId: ongoingTestStepID().String(), State: transcriptpb.CompactionState_COMPACTION_STATE_FAILED, Mode: transcriptpb.CompactionMode_COMPACTION_MODE_MANUAL,
			Count: 1, RequestId: textutil.Value((&otherRequestID).String()),
			Diagnostic: &transcriptpb.Diagnostic{
				Code:   "compaction_failed",
				Detail: "provider failed"}}} {
		model.applyAdmittedTranscriptMessageState(transcriptTestMessage(uint64(sequence+2), status), runtimeTupleMergeResult{})
	}
	if ringer.total() != 0 {
		t.Fatalf("non-success manual outcomes emitted %d notification events", ringer.total())
	}
	if _, exists := model.pendingCompactionRequestIDs[requestID]; !exists {
		t.Fatal("unrelated terminal compaction cleared the initiating TUI request")
	}

	model.applyAdmittedTranscriptMessageState(transcriptTestMessage(2, &transcriptpb.CompactionStatus{StepId: ongoingTestStepID().String(),
		State: transcriptpb.CompactionState_COMPACTION_STATE_COMPLETED,
		Mode:  transcriptpb.CompactionMode_COMPACTION_MODE_MANUAL,
		Count: 1, RequestId: textutil.Value((&requestID).String())}), runtimeTupleMergeResult{})
	if ringer.notifications != 1 {
		t.Fatalf("terminal manual compaction emitted %d notifications, want 1", ringer.notifications)
	}
	if len(model.pendingCompactionRequestIDs) != 0 {
		t.Fatal("matching terminal compaction retained the completed request")
	}
}

func TestManualCompactionTerminalEventDoesNotNotifyOtherAttachedTUI(t *testing.T) {
	requestID := runtimeids.NewCompactionRequestID()
	initiatorRinger := &countRinger{}
	observerRinger := &countRinger{}
	initiator := newProjectedStaticUIModel(WithUITurnQueueHook(newUnfocusedBellHooks(initiatorRinger)))
	observer := newProjectedStaticUIModel(WithUITurnQueueHook(newUnfocusedBellHooks(observerRinger)))
	initiator.registerPendingCompactionRequest(requestID)

	event := transcriptTestMessage(1, &transcriptpb.CompactionStatus{StepId: ongoingTestStepID().String(),
		State: transcriptpb.CompactionState_COMPACTION_STATE_COMPLETED,
		Mode:  transcriptpb.CompactionMode_COMPACTION_MODE_MANUAL,
		Count: 1, RequestId: textutil.Value((&requestID).String())})
	initiator.applyAdmittedTranscriptMessageState(event, runtimeTupleMergeResult{})
	observer.applyAdmittedTranscriptMessageState(event, runtimeTupleMergeResult{})

	if initiatorRinger.notifications != 1 {
		t.Fatalf("initiating TUI notifications = %d, want 1", initiatorRinger.notifications)
	}
	if observerRinger.total() != 0 {
		t.Fatalf("observing TUI received %d spurious notifications", observerRinger.total())
	}
}

func TestTranscriptHydrationClearsNotificationStateWithoutReplayingRows(t *testing.T) {
	ringer := &countRinger{}
	hooks := newUnfocusedBellHooks(ringer)
	recordToolHeavyBellTurn(hooks, 1)

	model := newProjectedStaticUIModel(WithUITurnQueueHook(hooks))
	hydration := ongoingHydrationMessage(1)
	hydratedFinal := bellAssistantFinalMessageWithText(2, "hydrated historical answer")
	hydrationPayload := hydration.Event.GetHydration()
	hydrationPayload.TailSegment.Entries = []*transcriptpb.CommittedRow{
		hydratedFinal.Event.GetCommittedRow()}
	hydration = transcriptTestMessage(1, hydrationPayload)
	model.applyAdmittedTranscriptMessageState(hydration, runtimeTupleMergeResult{})
	hooks.OnTurnQueueDrained()

	if ringer.total() != 0 {
		t.Fatalf("hydration emitted %d historical notification events", ringer.total())
	}
}

func TestTranscriptSubscriptionLossClearsNotificationState(t *testing.T) {
	ringer := &countRinger{}
	hooks := newUnfocusedBellHooks(ringer)
	recordToolHeavyBellTurn(hooks, 1)

	model := newProjectedStaticUIModel(WithUITurnQueueHook(hooks))
	requestID := runtimeids.NewCompactionRequestID()
	model.registerPendingCompactionRequest(requestID)
	model.ongoingTranscript = newNoopOngoingTranscriptController(
		&ongoingSurfaceSpy{},
		ongoingTestFrameProvider,
	)
	model.handleOngoingTranscriptEvent(ongoingTranscriptEvent{Kind: ongoingTranscriptEventLoss})
	hooks.OnTurnQueueDrained()

	if ringer.total() != 0 {
		t.Fatalf("subscription loss retained %d notification events", ringer.total())
	}
	if len(model.pendingCompactionRequestIDs) != 0 {
		t.Fatal("subscription loss retained a pending compaction notification request")
	}
}
