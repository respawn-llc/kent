package ongoing

import (
	"bytes"
	"testing"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
)

func TestSurfaceBlankFinalDefersEmptyStreamPromotion(t *testing.T) {
	surface := NewSurface(&bytes.Buffer{})
	streamID := runtimeids.NewAssistantStreamID().String()
	surface.activeAssistant = activeAssistantState{
		streamID: &streamID,
		phase:    transcriptpb.AssistantPhase_ASSISTANT_PHASE_FINAL,
	}

	if !surface.activeAssistantPromotionDeferred() {
		t.Fatal("empty final stream was not deferred")
	}
}

func TestSurfaceBlankFinalDefersWhitespaceStreamPromotion(t *testing.T) {
	surface := NewSurface(&bytes.Buffer{})
	streamID := runtimeids.NewAssistantStreamID().String()
	surface.activeAssistant = activeAssistantState{
		streamID: streamIDPointer(streamID),
		source:   " \n\t ",
		phase:    transcriptpb.AssistantPhase_ASSISTANT_PHASE_FINAL,
	}

	if !surface.activeAssistantPromotionDeferred() {
		t.Fatal("whitespace-only final stream was not deferred")
	}
}

func TestSurfaceBlankFinalAbortClearsDeferredStream(t *testing.T) {
	surface := NewSurface(&bytes.Buffer{})
	streamID := runtimeids.NewAssistantStreamID().String()
	surface.activeAssistant = activeAssistantState{
		streamID: streamIDPointer(streamID),
		source:   " \n\t ",
		phase:    transcriptpb.AssistantPhase_ASSISTANT_PHASE_FINAL,
	}

	if _, err := surface.abortAssistantStream(streamID, blankFinalFrame()); err != nil {
		t.Fatalf("abort assistant stream: %v", err)
	}
	if surface.activeAssistant.streamID != nil || surface.activeAssistantPromotionDeferred() {
		t.Fatalf("aborted assistant stream state = %+v, want cleared", surface.activeAssistant)
	}
}

func TestSurfaceBlankFinalHydrationDefersWhitespaceStream(t *testing.T) {
	surface := NewSurface(&bytes.Buffer{})
	streamID := runtimeids.NewAssistantStreamID().String()
	if !surface.hydrateActiveAssistantStream(&transcriptpb.AssistantStream{
		StreamId: streamID,
		Text:     " \n\t ",
		Phase:    transcriptpb.AssistantPhase_ASSISTANT_PHASE_FINAL,
	}) {
		t.Fatal("whitespace active stream was not hydrated")
	}
	if !surface.activeAssistantPromotionDeferred() {
		t.Fatal("hydrated whitespace-only final stream was not deferred")
	}
}

func TestSurfaceBlankFinalOrdinaryTextStillPromotes(t *testing.T) {
	var output bytes.Buffer
	surface := NewSurface(&output)
	streamID := runtimeids.NewAssistantStreamID().String()
	surface.activeAssistant = activeAssistantState{
		streamID: streamIDPointer(streamID),
		phase:    transcriptpb.AssistantPhase_ASSISTANT_PHASE_FINAL,
	}

	if _, err := surface.finalizeAssistantStream(streamID, "ordinary final", blankFinalFrame()); err != nil {
		t.Fatalf("finalize ordinary assistant stream: %v", err)
	}
	if surface.activeAssistant.streamID != nil {
		t.Fatal("ordinary final stream remained active")
	}
	if output.Len() == 0 {
		t.Fatal("ordinary final stream produced no terminal output")
	}
}

func streamIDPointer(streamID string) *string {
	return &streamID
}

func blankFinalFrame() FrameInput {
	return FrameInput{Size: Size{Width: 80, Height: 24}}
}
