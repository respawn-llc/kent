package app

import (
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"reflect"
	"testing"
	"time"
)

func TestFrameAnimationClockUsesElapsedFrameBoundaries(t *testing.T) {
	var clock frameAnimationClock
	anchor := time.Unix(1_700_000_000, 0)
	clock.Start(anchor)

	if got := clock.Frame(anchor.Add(-time.Millisecond), 8, 80*time.Millisecond); got != 0 {
		t.Fatalf("expected negative elapsed frame to clamp to 0, got %d", got)
	}
	if got := clock.Frame(anchor.Add(79*time.Millisecond), 8, 80*time.Millisecond); got != 0 {
		t.Fatalf("expected first frame before boundary, got %d", got)
	}
	if got := clock.Frame(anchor.Add(80*time.Millisecond), 8, 80*time.Millisecond); got != 1 {
		t.Fatalf("expected second frame at first boundary, got %d", got)
	}
	if got := clock.Frame(anchor.Add(640*time.Millisecond), 8, 80*time.Millisecond); got != 0 {
		t.Fatalf("expected frame index to wrap after full cycle, got %d", got)
	}
	if got := clock.NextDelay(anchor.Add(241*time.Millisecond), 80*time.Millisecond); got != 79*time.Millisecond {
		t.Fatalf("expected next delay aligned to next frame boundary, got %s", got)
	}
}

func TestHandleSpinnerTickJumpsFromElapsedTimeAndKeepsBoundaryAlignedDelay(t *testing.T) {
	oldInterval := spinnerTickInterval
	spinnerTickInterval = 10 * time.Millisecond
	t.Cleanup(func() { spinnerTickInterval = oldInterval })

	anchor := time.Unix(1_700_000_100, 0)
	m := newProjectedStaticUIModel()
	m.setRuntimeActivityBusyForTest(true)
	m.spinnerTickToken = 1
	m.spinnerGeneration = 1
	m.spinnerClock.Start(anchor)

	tickAt := anchor.Add(35 * time.Millisecond)
	next, cmd := m.inputController().handleSpinnerTick(spinnerTickMsg{token: 1, at: tickAt})
	updated := next.(*uiModel)
	if got, want := updated.spinnerFrame, 3; got != want {
		t.Fatalf("expected late tick to jump to frame %d from elapsed time, got %d", want, got)
	}
	if got, want := updated.spinnerClock.NextDelay(tickAt, spinnerTickInterval), 5*time.Millisecond; got != want {
		t.Fatalf("expected next delay %s after late tick, got %s", want, got)
	}
	if got, want := updated.spinnerTickDue, tickAt.Add(5*time.Millisecond); !got.Equal(want) {
		t.Fatalf("expected next tick due at %s after late tick, got %s", want, got)
	}
	if cmd == nil {
		t.Fatal("expected spinner tick to schedule next boundary-aligned tick")
	}
}

func TestTranscriptRuntimeProgressStartsAndRearmsSpinner(t *testing.T) {
	oldInterval := spinnerTickInterval
	oldGrace := spinnerTickRearmGrace
	oldNow := uiAnimationNow
	spinnerTickInterval = 10 * time.Millisecond
	spinnerTickRearmGrace = 30 * time.Millisecond
	anchor := time.Unix(1_700_000_150, 0)
	now := anchor
	uiAnimationNow = func() time.Time { return now }
	t.Cleanup(func() {
		spinnerTickInterval = oldInterval
		spinnerTickRearmGrace = oldGrace
		uiAnimationNow = oldNow
	})

	m := newAnimationTranscriptModel(t)
	running := ongoingTranscriptMessage(2, reflect.TypeFor[*transcriptpb.Event_RuntimeReadModelUpdate]())
	runningPayload := running.GetEvent().GetRuntimeReadModelUpdate()
	runningPayload.Activity = &runtimepb.Activity{
		State:    runtimepb.ActivityState_RUNTIME_ACTIVITY_RUNNING,
		Reviewer: runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE,
		ActiveStep: &runtimepb.ActiveStep{RunId: ongoingTestRunID().String(), StepId: ongoingTestStepID().String(),
			ActiveKind: runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_USER_TURN}}
	running = transcriptTestMessage(2, runningPayload)
	next, cmd := m.Update(ongoingTranscriptEvent{Kind: ongoingTranscriptEventMessage, Message: running})
	updated := next.(*uiModel)
	if !updated.isBusy() || updated.spinnerTickToken == 0 || updated.spinnerTickDue.IsZero() || cmd == nil {
		t.Fatalf(
			"runtime progress did not start spinner: busy=%t token=%d due=%s cmd=%v",
			updated.isBusy(),
			updated.spinnerTickToken,
			updated.spinnerTickDue,
			cmd,
		)
	}

	previousToken := updated.spinnerTickToken
	updated.spinnerTickDue = anchor.Add(spinnerTickInterval)
	now = updated.spinnerTickDue.Add(spinnerTickRearmGrace + time.Millisecond)
	next, cmd = updated.Update(ongoingTranscriptEvent{
		Kind:    ongoingTranscriptEventMessage,
		Message: animationAssistantDeltaMessage(3),
	})
	updated = next.(*uiModel)
	if updated.spinnerTickToken == 0 || updated.spinnerTickToken == previousToken {
		t.Fatalf("runtime progress did not rearm spinner token: previous=%d current=%d", previousToken, updated.spinnerTickToken)
	}
	if !updated.spinnerTickDue.After(now) || cmd == nil {
		t.Fatalf("runtime progress did not schedule a fresh spinner tick: due=%s now=%s cmd=%v", updated.spinnerTickDue, now, cmd)
	}
}

func newAnimationTranscriptModel(t *testing.T) *uiModel {
	t.Helper()
	surface := &ongoingSurfaceSpy{}
	m := newProjectedStaticUIModel()
	runtimeClient := newUIRuntimeClientWithReads(
		ongoingTestSessionID().String(), &countingSessionViewClient{},
		newUnavailableRuntimeControlService(), nil, nil,
	).(*sessionRuntimeClient)
	m.ongoingTranscript = newOngoingTranscriptController(
		surface,
		m.ongoingFrameInput,
		runtimeClient.admitTranscriptMessageState,
		m.applyAdmittedTranscriptMessageState,
	)
	next, _ := m.Update(ongoingTranscriptEvent{
		Kind:    ongoingTranscriptEventMessage,
		Message: ongoingHydrationMessage(1),
	})
	return next.(*uiModel)
}

func animationAssistantDeltaMessage(sequence uint64) *transcriptpb.Message {
	streamID := runtimeids.NewAssistantStreamID()
	return transcriptTestMessage(sequence, &transcriptpb.AssistantDelta{StepId: ongoingTestStepID().String(), StreamId: streamID.String(),
		Delta: "working",
		Phase: transcriptpb.AssistantPhase_ASSISTANT_PHASE_COMMENTARY})
}
