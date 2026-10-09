package app

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"core/internal/testharness/pty/analyzer"

	tea "github.com/charmbracelet/bubbletea"
)

func TestOnboardingNativeCursorMovesWithoutTextChanges(t *testing.T) {
	const width, height = 24, 24
	model := newOnboardingModelAtModelInput(t)
	model.width, model.height = width, height
	model.input.Editor.Replace("abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJ")
	model.input.Editor.SetCursor(30)
	model.terminalCursor = newUITerminalCursorState()
	model.View()
	placement, ok := model.terminalCursor.Snapshot()
	if !ok {
		t.Fatal("input did not request a native cursor")
	}
	initial := analyzer.Position{Row: placement.CursorRow, Col: placement.CursorCol}
	stream, err := analyzer.NewStream(analyzer.Dimensions{Rows: height, Cols: width})
	if err != nil {
		t.Fatal(err)
	}
	output := &onboardingTerminalOutput{stream: stream, changed: make(chan struct{}, 1)}
	input, keyboard := io.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	program := tea.NewProgram(model,
		tea.WithInput(input),
		tea.WithOutput(newUITerminalCursorWriter(output, model.terminalCursor)),
		tea.WithAltScreen(),
		tea.WithContext(ctx),
		tea.WithoutSignalHandler(),
	)
	done := make(chan error, 1)
	go func() {
		_, runErr := program.Run()
		done <- runErr
	}()
	t.Cleanup(func() {
		program.Quit()
		_ = keyboard.Close()
		_ = input.Close()
		if runErr := <-done; runErr != nil {
			t.Errorf("terminal program: %v", runErr)
		}
		cancel()
		if _, finishErr := stream.Finish(); finishErr != nil {
			t.Errorf("terminal interpretation: %v", finishErr)
		}
	})
	program.Send(tea.WindowSizeMsg{Width: width, Height: height})
	output.waitCursor(t, ctx, initial)
	for _, step := range []struct {
		name     string
		sequence string
		position analyzer.Position
	}{
		{"left", "\x1b[D", analyzer.Position{Row: initial.Row, Col: initial.Col - 1}},
		{"right", "\x1b[C", initial},
		{"up", "\x1b[A", analyzer.Position{Row: initial.Row - 1, Col: initial.Col}},
		{"down", "\x1b[B", initial},
	} {
		if _, err := io.WriteString(keyboard, step.sequence); err != nil {
			t.Fatalf("%s input: %v", step.name, err)
		}
		output.waitCursor(t, ctx, step.position)
	}
}

type onboardingTerminalOutput struct {
	mu      sync.Mutex
	stream  *analyzer.Stream
	changed chan struct{}
	err     error
}

func (o *onboardingTerminalOutput) Write(payload []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.err = o.stream.Feed(payload)
	select {
	case o.changed <- struct{}{}:
	default:
	}
	if o.err != nil {
		return 0, o.err
	}
	return len(payload), nil
}

func (o *onboardingTerminalOutput) waitCursor(t *testing.T, ctx context.Context, want analyzer.Position) {
	t.Helper()
	for {
		o.mu.Lock()
		screen, err := o.stream.ScreenSnapshot()
		writeErr := o.err
		o.mu.Unlock()
		if err != nil || writeErr != nil {
			t.Fatalf("terminal output: snapshot=%v write=%v", err, writeErr)
		}
		if screen.Cursor == want {
			return
		}
		select {
		case <-o.changed:
		case <-ctx.Done():
			t.Fatalf("terminal cursor = %+v, want %+v: %v", screen.Cursor, want, ctx.Err())
		}
	}
}
