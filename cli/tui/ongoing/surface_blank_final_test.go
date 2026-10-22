package ongoing

import (
	"bytes"
	"testing"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/textutil"

	"github.com/google/uuid"
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

func TestSurfaceAcceptsSeparateAssistantStreamsBeforeEitherCommits(t *testing.T) {
	var output bytes.Buffer
	surface := NewSurface(&output)
	first := runtimeids.NewAssistantStreamID().String()
	second := runtimeids.NewAssistantStreamID().String()
	phase := transcriptpb.AssistantPhase_ASSISTANT_PHASE_COMMENTARY
	apply := func(event *transcriptpb.Event) {
		t.Helper()
		if _, err := surface.ApplyTerminalMessage(&transcriptpb.Message{Event: event}, blankFinalFrame()); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{first, second} {
		apply(&transcriptpb.Event{Payload: &transcriptpb.Event_AssistantDelta{
			AssistantDelta: &transcriptpb.AssistantDelta{StreamId: id, Phase: phase, Delta: "item"},
		}})
	}
	for _, id := range []string{first, second} {
		apply(&transcriptpb.Event{Payload: &transcriptpb.Event_CommittedRow{
			CommittedRow: &transcriptpb.CommittedRow{Row: &transcriptpb.CommittedRow_Assistant{
				Assistant: &transcriptpb.AssistantRow{StreamId: &id, Phase: phase, Text: "item"},
			}},
		}})
		if id == first {
			apply(&transcriptpb.Event{Payload: &transcriptpb.Event_CommittedRow{
				CommittedRow: &transcriptpb.CommittedRow{
					Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED,
					Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
					Locator:    &transcriptpb.CommittedRowLocator{EventSequence: 1, RowOrdinal: 2},
					Row: &transcriptpb.CommittedRow_ToolCall{ToolCall: &transcriptpb.ToolStart{
						StepId: uuid.NewString(), ToolCallId: "native-call", ToolName: "exec_command",
					}},
				},
			}})
		}
	}
	apply(&transcriptpb.Event{Payload: &transcriptpb.Event_CommittedRow{
		CommittedRow: &transcriptpb.CommittedRow{
			Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED,
			Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
			Locator:    &transcriptpb.CommittedRowLocator{EventSequence: 1, RowOrdinal: 2},
			Row: &transcriptpb.CommittedRow_Tool{Tool: &transcriptpb.ToolRow{
				ToolCallId: textutil.Value("native-call"), ToolName: textutil.Value("exec_command"), ResultSummary: textutil.Value("result"),
			}},
		},
	}})
	if output.Len() == 0 {
		t.Fatal("separate assistant streams produced no terminal output")
	}
}
