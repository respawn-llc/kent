package ongoing

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"core/cli/tui/transcriptrender"
	"core/internal/testharness/pty"
	"core/internal/testharness/pty/analyzer"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestSurfaceFullHeightBoundaryPreservesCommittedOutput(t *testing.T) {
	const height = 8
	var output bytes.Buffer
	surface := NewSurface(&output)
	stream, err := analyzer.NewStream(pty.MustDimensions(height, 40))
	if err != nil {
		t.Fatal(err)
	}
	var retired []string
	feed := func() {
		t.Helper()
		for _, b := range output.Bytes() {
			var before analyzer.ScreenSnapshot
			if b == '\n' {
				before, err = stream.ScreenSnapshot()
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := stream.Feed([]byte{b}); err != nil {
				t.Fatal(err)
			}
			if b == '\n' && before.Cursor.Row == height-1 {
				retired = append(retired, screenRow(before, 0))
			}
		}
		output.Reset()
	}
	frame := FrameInput{Size: Size{Width: 40, Height: height}, Sections: []FrameSection{{Kind: FrameSectionStatus, Lines: []string{"live"}}}}
	if _, err := surface.ApplyTerminalMessage(committedAssistantMessage("original"), frame); err != nil {
		t.Fatal(err)
	}
	feed()
	for _, band := range []int{height - 1, height} {
		frame.Sections[0].Lines = make([]string, band)
		for index := range frame.Sections[0].Lines {
			frame.Sections[0].Lines[index] = "mutable"
		}
		if _, err := surface.Render(frame); err != nil {
			t.Fatal(err)
		}
		feed()
	}
	if !strings.Contains(strings.Join(retired, "\n"), "original") {
		t.Fatalf("growth lost original committed output: %q", retired)
	}
	retired = nil
	var expected []string
	for index, band := range []int{height, height - 1, height - 1, height} {
		frame.Sections[0].Lines = frame.Sections[0].Lines[:band]
		if _, err := surface.Render(frame); err != nil {
			t.Fatal(err)
		}
		feed()
		text := []string{"first", strings.Repeat("x", 40), strings.Repeat("y", 65), "last"}[index]
		message := committedAssistantMessage(text)
		rendered := transcriptrender.RenderCommittedRow(message.GetEvent().GetCommittedRow(), 40, "", transcriptrender.ModeOngoingStable)
		for _, row := range transcriptrender.PlainLines(rendered.Lines) {
			expected = append(expected, strings.Split(xansi.Hardwrap(strings.TrimRight(row, " "), 40, true), "\n")...)
		}
		if _, err := surface.ApplyTerminalMessage(message, frame); err != nil {
			t.Fatal(err)
		}
		feed()
		snapshot, err := stream.ScreenSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		for row := height - band; row < height; row++ {
			if got := screenRow(snapshot, row); got != "mutable" {
				t.Fatalf("append with band %d row %d = %q, want mutable", band, row, got)
			}
		}
		if index == 2 {
			frame.Sections[0].Lines = append(frame.Sections[0].Lines, "mutable")
		}
	}
	if !slices.Equal(retired, expected) {
		t.Fatalf("committed output = %q, want %q", retired, expected)
	}
	for _, row := range retired {
		if row == "mutable" {
			t.Fatalf("formerly mutable output was promoted: %q", retired)
		}
	}
}
