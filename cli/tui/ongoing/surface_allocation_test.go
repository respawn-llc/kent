package ongoing

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"core/internal/testharness/pty"
	"core/internal/testharness/pty/analyzer"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
)

func TestSurfacePriorityAllocation(t *testing.T) {
	frame := FrameInput{
		Size: Size{Width: 40, Height: 9},
		Sections: []FrameSection{
			{Kind: FrameSectionPendingTools, Lines: []string{"tool1", "tool2", "tool3"}},
			{Kind: FrameSectionInput, Lines: []string{"upper", "draft1", "draft2", "draft3", "lower"}},
			{Kind: FrameSectionStatus, Lines: []string{"status"}},
		},
	}
	var output bytes.Buffer
	surface := NewSurface(&output)
	stream, err := analyzer.NewStream(pty.MustDimensions(9, 40))
	if err != nil {
		t.Fatal(err)
	}
	frame.Size.Height = 8
	if err := stream.Resize(pty.MustDimensions(8, 40)); err != nil {
		t.Fatal(err)
	}
	if _, err := surface.Render(frame); err != nil {
		t.Fatal(err)
	}
	if err := stream.Feed(output.Bytes()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := stream.ScreenSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tool2", "tool3", "upper", "draft1", "draft2", "draft3", "lower", "status"}
	for row, text := range want {
		if got := screenRow(snapshot, row); got != text {
			t.Fatalf("row %d = %q, want %q", row, got, text)
		}
	}
}

func TestSurfaceCommentaryRollsBeforeTools(t *testing.T) {
	var output bytes.Buffer
	surface := NewSurface(&output)
	frame := FrameInput{
		Size: Size{Width: 40, Height: 10},
		Sections: []FrameSection{
			{Kind: FrameSectionPendingTools, Lines: []string{"tool1", "tool2"}},
			{Kind: FrameSectionInput, Lines: []string{"upper", "draft", "lower"}},
			{Kind: FrameSectionStatus, Lines: []string{"status"}},
		},
	}
	message := &transcriptpb.Message{Event: &transcriptpb.Event{Payload: &transcriptpb.Event_AssistantDelta{
		AssistantDelta: &transcriptpb.AssistantDelta{
			StreamId: "stream", Delta: strings.Repeat("word ", 24) + "tail",
			Phase: transcriptpb.AssistantPhase_ASSISTANT_PHASE_COMMENTARY,
		},
	}}}
	if _, err := surface.ApplyTerminalMessage(message, frame); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	stream, err := analyzer.NewStream(pty.MustDimensions(8, 40))
	if err != nil {
		t.Fatal(err)
	}
	frame.Size.Height = 8
	if _, err := surface.Render(frame); err != nil {
		t.Fatal(err)
	}
	if err := stream.Feed(output.Bytes()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := stream.ScreenSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if screenRow(snapshot, 0) == "" || screenRow(snapshot, 1) != "tail" {
		t.Fatalf("commentary should retain two newest rows: %q", snapshot.RenderText())
	}
	if screenRow(snapshot, 2) != "tool1" || screenRow(snapshot, 3) != "tool2" {
		t.Fatalf("commentary reduction changed tool allocation: %q", snapshot.RenderText())
	}
}

func TestSurfaceOverlayGrowsIndependentlyAboveInput(t *testing.T) {
	var output bytes.Buffer
	surface := NewSurface(&output)
	frame := FrameInput{
		Size: Size{Width: 40, Height: 11},
		Sections: []FrameSection{
			{Kind: FrameSectionInput, Lines: []string{"upper", "draft", "lower"}},
			{Kind: FrameSectionStatus, Lines: []string{"status"}},
		},
		Overlay: &FrameDemand{PreferredHeight: 7, MinimumHeight: 1, Project: func(rows int) SectionProjection {
			lines := make([]string, rows)
			for index := range lines {
				lines[index] = fmt.Sprintf("result%d", index)
			}
			return SectionProjection{Lines: lines}
		}},
	}
	stream, err := analyzer.NewStream(pty.MustDimensions(11, 40))
	if err != nil {
		t.Fatal(err)
	}
	for _, height := range []int{11, 5, 4, 11} {
		frame.Size.Height = height
		if err := stream.Resize(pty.MustDimensions(height, 40)); err != nil {
			t.Fatal(err)
		}
		if _, err := surface.Resize(frame.Size, frame); err != nil {
			t.Fatal(err)
		}
		if err := stream.Feed(output.Bytes()); err != nil {
			t.Fatal(err)
		}
		output.Reset()
		snapshot, err := stream.ScreenSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		for row := range height - 4 {
			if got, want := screenRow(snapshot, row), fmt.Sprintf("result%d", row); got != want {
				t.Fatalf("height %d overlay row %d = %q, want %q", height, row, got, want)
			}
		}
		if got := screenRow(snapshot, height-3); got != "draft" {
			t.Fatalf("overlay moved input: %q", snapshot.RenderText())
		}
	}
}

func TestSurfaceAllocationMinimumAndRegrowth(t *testing.T) {
	var output bytes.Buffer
	surface := NewSurface(&output)
	frame := FrameInput{
		Size: Size{Width: 40, Height: 12},
		Sections: []FrameSection{
			{Kind: FrameSectionPendingTools, Lines: []string{"tool1", "tool2", "tool3"}},
			{Kind: FrameSectionQueuedOrSteered, Demand: &FrameDemand{
				PreferredHeight: 4, MinimumHeight: 2,
				Project: func(height int) SectionProjection {
					rows := make([]string, height)
					for index := range rows {
						rows[index] = fmt.Sprintf("queue%d", 4-height+index)
					}
					return SectionProjection{Lines: rows}
				},
			}},
			{Kind: FrameSectionInput, Demand: &FrameDemand{
				PreferredHeight: 5, MinimumHeight: 3,
				Project: func(height int) SectionProjection {
					rows := []string{"upper"}
					for index := range height - 2 {
						rows = append(rows, fmt.Sprintf("draft%d", index))
					}
					return SectionProjection{Lines: append(rows, "lower")}
				},
			}},
			{Kind: FrameSectionStatus, Lines: []string{"status"}},
		},
	}
	stream, err := analyzer.NewStream(pty.MustDimensions(12, 40))
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		height int
		want   []string
	}{
		{12, []string{"tool1", "tool2", "tool3", "queue1", "queue2", "queue3", "upper", "draft0", "draft1", "draft2", "lower", "status"}},
		{11, []string{"tool1", "tool2", "tool3", "queue2", "queue3", "upper", "draft0", "draft1", "draft2", "lower", "status"}},
		{10, []string{"tool2", "tool3", "queue2", "queue3", "upper", "draft0", "draft1", "draft2", "lower", "status"}},
		{9, []string{"tool3", "queue2", "queue3", "upper", "draft0", "draft1", "draft2", "lower", "status"}},
		{8, []string{"tool3", "queue2", "queue3", "upper", "draft0", "draft1", "lower", "status"}},
		{7, []string{"tool3", "queue2", "queue3", "upper", "draft0", "lower", "status"}},
		{6, nil},
		{12, []string{"tool1", "tool2", "tool3", "queue1", "queue2", "queue3", "upper", "draft0", "draft1", "draft2", "lower", "status"}},
	} {
		if err := stream.Resize(pty.MustDimensions(step.height, 40)); err != nil {
			t.Fatal(err)
		}
		frame.Size.Height = step.height
		if _, err := surface.Resize(frame.Size, frame); err != nil {
			t.Fatal(err)
		}
		if err := stream.Feed(output.Bytes()); err != nil {
			t.Fatal(err)
		}
		output.Reset()
		snapshot, err := stream.ScreenSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		for row := range step.height {
			want := ""
			if row >= step.height-len(step.want) {
				want = step.want[row-(step.height-len(step.want))]
			}
			if got := screenRow(snapshot, row); got != want {
				t.Fatalf("height %d row %d = %q, want %q", step.height, row, got, want)
			}
		}
	}
}
