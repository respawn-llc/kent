package app

import (
	"bytes"
	"core/cli/tui/ongoing"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"reflect"
	"strings"
	"testing"
)

func TestOngoingTranscriptControllerScratchRehydrationTriggersResetSequence(t *testing.T) {
	controller := newTestOngoingTranscriptController(&ongoingSurfaceSpy{}, ongoingTestFrameProvider)
	if _, err := controller.Accept(ongoingHydrationMessage(1)); err != nil {
		t.Fatalf("accept hydration: %v", err)
	}
	if result, err := controller.Accept(ongoingTranscriptMessage(3, reflect.TypeFor[*transcriptpb.Event_SessionStatus]())); err != nil || result.Action != ongoing.ResultRequestScratchRehydration {
		t.Fatalf("sequence gap result=%+v err=%v, want scratch rehydration", result, err)
	}

	controller.ResetForScratchHydration()

	if result, err := controller.Accept(ongoingHydrationMessage(1)); err != nil || result.Action != ongoing.ResultNoop {
		t.Fatalf("post-reset hydration result=%+v err=%v, want accepted hydration", result, err)
	}
}

func TestOngoingTranscriptControllerResizeCutoverPreservesQueuedDelivery(t *testing.T) {
	surface := &ongoingSurfaceSpy{}
	controller := newTestOngoingTranscriptController(surface, ongoingTestFrameProvider)
	if _, err := controller.Accept(ongoingHydrationMessage(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.SetNormalBufferOwned(false); err != nil {
		t.Fatal(err)
	}
	old := transcriptTestMessage(2, &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row:        &transcriptpb.CommittedRow_Assistant{Assistant: &transcriptpb.AssistantRow{Text: "old delivery outside replacement tail"}},
	})
	if _, err := controller.Accept(old); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AcceptCutover(ongoing.RehydrateReasonResize); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Accept(ongoingHydrationMessage(1)); err != nil {
		t.Fatal(err)
	}
	surface.calls = nil
	if _, err := controller.SetNormalBufferOwned(true); err != nil {
		t.Fatal(err)
	}
	if got := surface.callKinds(); !reflect.DeepEqual(got, []string{"apply", "reset", "apply"}) {
		t.Fatalf("terminal cutover delivery = %v", got)
	}
	if surface.calls[0].message != old {
		t.Fatal("old queued delivery was replaced by hydration")
	}
}

func TestOngoingTranscriptControllerResizeCutoverKeepsOldAssistantUntilMarker(t *testing.T) {
	var output bytes.Buffer
	surface := ongoing.NewSurface(&output)
	controller := newTestOngoingTranscriptController(surface, ongoingTestFrameProvider)
	if _, err := controller.Accept(ongoingHydrationMessage(1)); err != nil {
		t.Fatal(err)
	}
	stream := runtimeids.NewAssistantStreamID().String()
	if _, err := controller.Accept(transcriptTestMessage(2, &transcriptpb.AssistantDelta{
		StepId: ongoingTestStepID().String(), StreamId: stream, Delta: "old start ", Phase: transcriptpb.AssistantPhase_ASSISTANT_PHASE_COMMENTARY,
	})); err != nil {
		t.Fatal(err)
	}
	controller.SetNormalBufferOwned(false)
	if _, err := controller.Accept(transcriptTestMessage(3, &transcriptpb.AssistantDelta{
		StepId: ongoingTestStepID().String(), StreamId: stream, Delta: "old completion\n\n", Phase: transcriptpb.AssistantPhase_ASSISTANT_PHASE_COMMENTARY,
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AcceptCutover(ongoing.RehydrateReasonResize); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Accept(ongoingHydrationMessage(1)); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if _, err := controller.SetNormalBufferOwned(true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "old start old completion") {
		t.Fatalf("old assistant stream was reset ahead of queued promotion: %q", output.String())
	}
}

func TestOngoingTranscriptControllerSubscriptionLossRequestsScratchRehydration(t *testing.T) {
	controller := newTestOngoingTranscriptController(&ongoingSurfaceSpy{}, ongoingTestFrameProvider)
	if _, err := controller.Accept(ongoingHydrationMessage(1)); err != nil {
		t.Fatalf("accept hydration: %v", err)
	}

	result := controller.HandleSubscriptionLoss()

	if result.Action != ongoing.ResultRequestScratchRehydration {
		t.Fatalf("subscription loss action = %q, want scratch rehydration", result.Action)
	}
	controller.ResetForScratchHydration()
	if result, err := controller.Accept(ongoingHydrationMessage(1)); err != nil || result.Action != ongoing.ResultNoop {
		t.Fatalf("post-loss hydration result=%+v err=%v, want accepted hydration", result, err)
	}
}
