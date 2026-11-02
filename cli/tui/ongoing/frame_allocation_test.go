package ongoing

import (
	"bytes"
	"testing"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
)

func TestFrameAllocationExposesInputLossWithoutWriting(t *testing.T) {
	var output bytes.Buffer
	surface := NewSurface(&output)
	frame := FrameInput{
		Size: Size{Width: 80, Height: 10},
		Sections: []FrameSection{
			{Kind: FrameSectionInput, Demand: &FrameDemand{
				PreferredHeight: 9, MinimumHeight: 3,
				Project: func(rows int) SectionProjection {
					return SectionProjection{Lines: make([]string, rows)}
				},
			}},
			{Kind: FrameSectionStatus, Lines: []string{"status"}},
		},
	}
	if rows := surface.AllocateFrame(frame).SectionRows(FrameSectionInput); len(rows) != 9 {
		t.Fatalf("exact-fit input coverage = %v", rows)
	}
	frame.Sections = append([]FrameSection{{Kind: FrameSectionPendingTools, Lines: []string{"tool"}}}, frame.Sections...)
	rows := surface.AllocateFrame(frame).SectionRows(FrameSectionInput)
	if len(rows) >= 9 || len(rows) == 0 {
		t.Fatalf("competing-section input coverage = %v, want reduced input", rows)
	}
	if output.Len() != 0 {
		t.Fatal("allocation query wrote terminal output")
	}
}

func TestFrameAllocationAcrossQueuedResetDoesNotConsumeSource(t *testing.T) {
	var output bytes.Buffer
	surface := NewSurface(&output)
	frame := FrameInput{Size: Size{Width: 80, Height: 10}, Sections: []FrameSection{{
		Kind: FrameSectionInput, Lines: []string{"upper", "draft", "lower"},
	}}}
	delta := func(stream string) *transcriptpb.Message {
		return &transcriptpb.Message{Event: &transcriptpb.Event{Payload: &transcriptpb.Event_AssistantDelta{
			AssistantDelta: &transcriptpb.AssistantDelta{
				StreamId: stream, Delta: "preview", Phase: transcriptpb.AssistantPhase_ASSISTANT_PHASE_COMMENTARY,
			},
		}}}
	}
	if _, err := surface.ApplyTerminalMessage(delta("old"), frame); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	count := 0
	for allocation := range surface.AllocateTerminalFrames([]TerminalWork{
		TerminalReset{Reason: RehydrateReasonResize},
		TerminalMessage{Message: delta("new")},
	}, frame) {
		count++
		if len(allocation.SectionRows(FrameSectionInput)) != 3 {
			t.Fatal("queued reset changed input coverage")
		}
	}
	if count != 2 || output.Len() != 0 {
		t.Fatalf("allocation count=%d terminal bytes=%d", count, output.Len())
	}
	if _, err := surface.ApplyTerminalMessage(delta("old"), frame); err != nil {
		t.Fatal(err)
	}
}
