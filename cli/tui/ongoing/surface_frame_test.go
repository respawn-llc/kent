package ongoing_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"core/cli/tui/ongoing"
	"core/internal/testharness/pty/analyzer"

	"github.com/charmbracelet/x/ansi"
)

func TestLiveBandTransactionsKeepIntermediateErasesOffscreen(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*ongoing.Surface, ongoing.FrameInput) error
	}{
		{name: "repaint", apply: func(surface *ongoing.Surface, frame ongoing.FrameInput) error {
			frame.Sections[0].Lines[0] = "next"
			_, err := surface.Render(frame)
			return err
		}},
		{name: "resize", apply: func(surface *ongoing.Surface, frame ongoing.FrameInput) error {
			_, err := surface.Resize(ongoing.Size{Width: 90, Height: 20}, frame)
			return err
		}},
		{name: "scratch reset", apply: func(surface *ongoing.Surface, frame ongoing.FrameInput) error {
			_, err := surface.ResetForScratchHydration(ongoing.RehydrateReasonWidthChange, frame)
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testLiveBandFrame(t, test.apply)
		})
	}
}

func testLiveBandFrame(t *testing.T, apply func(*ongoing.Surface, ongoing.FrameInput) error) {
	t.Helper()
	var output bytes.Buffer
	surface := ongoing.NewSurface(&output)
	frame := ongoing.FrameInput{
		Size: ongoing.Size{Width: 80, Height: 18},
		Sections: []ongoing.FrameSection{{
			Kind:  ongoing.FrameSectionStatus,
			Lines: []string{"active"},
		}},
	}
	if _, err := surface.Render(frame); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := apply(surface, frame); err != nil {
		t.Fatal(err)
	}
	analysis, err := analyzer.Analyze(analyzer.Capture{
		Dimensions: analyzer.MustDimensions(20, 90),
		Raw:        output.Bytes(),
		Chunks:     []analyzer.Chunk{analyzer.NewChunk(0, 0, output.Bytes())},
	})
	if err != nil {
		t.Fatal(err)
	}
	synchronized := false
	sawRepaint := false
	for _, operation := range analysis.Operations {
		if operation.PrivateMode != nil && operation.PrivateMode.Mode == int(ansi.ModeSynchronizedOutput) {
			synchronized = operation.PrivateMode.Enabled
			continue
		}
		changesCells := operation.Kind == analyzer.OperationWrite || operation.Kind == analyzer.OperationErase || len(operation.WriteSegments) > 0
		for _, control := range operation.Controls {
			changesCells = changesCells || control.Kind == analyzer.OperationErase
		}
		if changesCells {
			sawRepaint = true
			if !synchronized {
				t.Fatal("live-band erase or repaint is visible before the complete frame is ready")
			}
		}
	}
	if !sawRepaint {
		t.Fatal("no live-band repaint observed")
	}
	if synchronized {
		t.Fatal("completed frame left terminal presentation suspended")
	}
}

func TestLiveBandFrameReportsIncompleteOutput(t *testing.T) {
	failure := errors.New("terminal disconnected")
	for _, test := range []struct {
		name string
		n    int
		err  error
		want error
	}{
		{name: "short write", n: 1, want: io.ErrShortWrite},
		{name: "write failure", err: failure, want: failure},
	} {
		t.Run(test.name, func(t *testing.T) {
			surface := ongoing.NewSurface(frameFailureWriter{n: test.n, err: test.err})
			_, err := surface.Render(ongoing.FrameInput{Size: ongoing.Size{Width: 80, Height: 18}})
			if !errors.Is(err, test.want) {
				t.Fatalf("incomplete terminal output error = %v, want %v", err, test.want)
			}
		})
	}
}

type frameFailureWriter struct {
	n   int
	err error
}

func (w frameFailureWriter) Write([]byte) (int, error) {
	return w.n, w.err
}
