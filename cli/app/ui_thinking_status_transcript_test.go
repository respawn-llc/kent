package app

import (
	"bytes"
	"reflect"
	"testing"

	"core/cli/tui/ongoing"
	"core/cli/tui/transcriptrender"
	"core/internal/testharness/pty"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
)

func TestThinkingStatusUpdatesStayOutOfTranscriptAndKeepDetailTraceRenderable(t *testing.T) {
	var output bytes.Buffer
	surface := ongoing.NewSurface(&output)
	runtimeClient := newUIRuntimeClientWithReads(
		ongoingTestSessionID().String(),
		&countingSessionViewClient{},
		newUnavailableRuntimeControlService(),
		nil,
		nil,
	).(*sessionRuntimeClient)
	model := sizedTestUIModel(
		newProjectedTestUIModel(runtimeClient, WithUIOngoingSurface(surface)),
		80,
		24,
	)
	model.ongoingTranscript = newOngoingTranscriptController(
		surface,
		model.ongoingFrameInput,
		runtimeClient.admitTranscriptMessageState,
		model.applyAdmittedTranscriptMessageState,
	)

	hydration := ongoingHydrationMessage(1)
	hydration.Event.GetHydration().TailSegment.Entries = []*transcriptpb.CommittedRow{
		ongoingTranscriptMessage(2, reflect.TypeFor[*transcriptpb.Event_CommittedRow]()).
			GetEvent().GetCommittedRow(),
	}
	model = updateUIModel(t, model, ongoingTranscriptEvent{
		Kind:    ongoingTranscriptEventMessage,
		Message: hydration,
	})

	dimensions := pty.MustDimensions(24, 80)
	analyzeOutput := func(raw []byte) pty.Analysis {
		t.Helper()
		capture, err := pty.NewCapture(dimensions, []pty.Chunk{pty.NewChunk(0, 0, raw)})
		if err != nil {
			t.Fatalf("capture terminal output: %v", err)
		}
		analysis, err := pty.Analyze(capture)
		if err != nil {
			t.Fatalf("analyze terminal output: %v", err)
		}
		return analysis
	}
	immutableBottomForFrame := func(frame ongoing.FrameInput) int {
		liveBandHeight := 0
		for _, section := range frame.Sections {
			liveBandHeight += len(section.Lines) + len(section.StyledLines)
		}
		return dimensions.Rows - liveBandHeight
	}
	initialScreen := analyzeOutput(output.Bytes()).Screen
	immutableBottom := immutableBottomForFrame(model.ongoingFrameInput())
	if immutableBottom <= 0 {
		t.Fatalf("live band leaves no transcript region: bottom=%d", immutableBottom)
	}
	transcriptRows := make([][]pty.Cell, immutableBottom)
	hasTranscriptContent := false
	for row := range transcriptRows {
		transcriptRows[row] = append([]pty.Cell(nil), initialScreen.Cells[row]...)
		for _, cell := range transcriptRows[row] {
			hasTranscriptContent = hasTranscriptContent || cell.Content != ""
		}
	}
	if !hasTranscriptContent {
		t.Fatal("hydration did not produce visible transcript content")
	}

	statuses := []string{"status marker one", "status marker two", "status marker three"}
	for index, status := range statuses {
		outputStart := output.Len()
		model = updateUIModel(t, model, ongoingTranscriptEvent{
			Kind: ongoingTranscriptEventMessage,
			Message: transcriptTestMessage(uint64(index+2), &transcriptpb.ThinkingStatusUpdate{
				StepId: ongoingTestStepID().String(),
				Text:   status,
			}),
		})
		if got := model.reasoningStatusHeader; got != status {
			t.Fatalf("live thinking status = %q, want the latest status data %q", got, status)
		}

		if _, err := surface.Render(model.ongoingFrameInput()); err != nil {
			t.Fatalf("render live thinking status: %v", err)
		}
		frame := model.ongoingFrameInput()
		if got := immutableBottomForFrame(frame); got != immutableBottom {
			t.Fatalf("thinking status changed transcript boundary from %d to %d", immutableBottom, got)
		}
		statusOutput := append([]byte(nil), output.Bytes()[outputStart:]...)
		analysis := analyzeOutput(statusOutput)
		for _, transaction := range analysis.Operations {
			for _, operation := range pty.OperationRecords(transaction) {
				if operation.Kind == pty.OperationScrollRegionChange && operation.Region.Bottom < dimensions.Rows {
					t.Fatalf("thinking status changed transcript scrollback: %+v", operation)
				}
			}
		}
		screen := analyzeOutput(output.Bytes()).Screen
		if !reflect.DeepEqual(screen.Cells[:immutableBottom], transcriptRows) {
			t.Fatalf("thinking status changed transcript region after update %d", index+1)
		}
	}

	stepID, err := runtimeids.ParseStepID(ongoingTestStepID().String())
	if err != nil {
		t.Fatalf("parse reasoning trace step ID: %v", err)
	}
	trace := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_DETAIL,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row: &transcriptpb.CommittedRow_ReasoningTrace{ReasoningTrace: &transcriptpb.ReasoningTraceRow{
			StepId:      stepID.String(),
			CompactText: "A committed reasoning summary",
			Text:        "A committed reasoning trace",
		}},
	}
	rendered := transcriptrender.RenderCommittedRow(
		trace,
		80,
		"dark",
		transcriptrender.ModeDetailExpanded,
	)
	if rendered.Group != transcriptrender.GroupReasoningTrace || len(rendered.Lines) == 0 {
		t.Fatalf("committed reasoning trace is not renderable in Detail: %+v", rendered)
	}
}
