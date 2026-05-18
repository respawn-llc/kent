package app

import (
	"builder/cli/tui"
	"builder/server/llm"
	"builder/server/runtime"
	"bytes"
	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"time"
)

func TestNativeHistoryFlushesPreserveScheduledOrderWhenDeliveredOutOfOrder(t *testing.T) {
	out := &bytes.Buffer{}
	model := newProjectedStaticUIModel()
	firstCmd := model.emitNativeRenderedText("assistant final\n")
	secondCmd := model.emitNativeRenderedText("queued user\n")
	if firstCmd == nil || secondCmd == nil {
		t.Fatal("expected native history flush commands")
	}
	firstMsg, ok := firstCmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected first nativeHistoryFlushMsg, got %T", firstCmd())
	}
	secondMsg, ok := secondCmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected second nativeHistoryFlushMsg, got %T", secondCmd())
	}
	if secondMsg.Sequence != firstMsg.Sequence+1 {
		t.Fatalf("expected consecutive native flush sequence numbers, first=%d second=%d", firstMsg.Sequence, secondMsg.Sequence)
	}

	program := tea.NewProgram(model, tea.WithInput(strings.NewReader("")), tea.WithOutput(out), tea.WithoutSignals())
	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()

	time.Sleep(30 * time.Millisecond)
	program.Send(secondMsg)
	time.Sleep(30 * time.Millisecond)
	if strings.Contains(normalizedOutput(out.String()), "queued user") {
		t.Fatalf("expected later native flush buffered until earlier flush arrives, got %q", normalizedOutput(out.String()))
	}
	program.Send(firstMsg)
	waitForTestCondition(t, 2*time.Second, "ordered native flush replay", func() bool {
		return containsInOrder(normalizedOutput(out.String()), "assistant final", "queued user")
	})

	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program run failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("program did not terminate")
	}

	if normalized := normalizedOutput(out.String()); !containsInOrder(normalized, "assistant final", "queued user") {
		t.Fatalf("expected native history flushes to preserve scheduled order, got %q", normalized)
	}
}

func TestNativeAssistantDeltaSuppressedInDetailMode(t *testing.T) {
	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(
		nil,
		closedProjectedRuntimeEvents(),
		closedAskEvents(),
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "seed"}}),
	)
	program := tea.NewProgram(model, tea.WithInput(strings.NewReader("")), tea.WithOutput(out), tea.WithoutSignals())
	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	program.Send(tea.KeyMsg{Type: tea.KeyShiftTab})
	time.Sleep(20 * time.Millisecond)
	program.Send(projectedRuntimeEventMsg(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "hidden-delta"}))
	time.Sleep(20 * time.Millisecond)
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program run failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("program did not terminate")
	}
	if strings.Contains(normalizedOutput(out.String()), "hidden-delta") {
		t.Fatalf("expected assistant delta to stay suppressed while in detail mode, got %q", normalizedOutput(out.String()))
	}
}

func TestNativeStreamedFinalThenCommitAppearsOnceInScrollback(t *testing.T) {
	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), closedAskEvents())
	program := tea.NewProgram(model, tea.WithInput(strings.NewReader("")), tea.WithOutput(out), tea.WithoutSignals())
	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	program.Send(projectedRuntimeEventMsg(runtime.Event{Kind: runtime.EventAssistantDelta, StepID: "step-1", AssistantDelta: "final answer"}))
	waitForTestCondition(t, 2*time.Second, "streamed final visible", func() bool {
		return strings.Contains(normalizedOutput(out.String()), "final answer")
	})
	program.Send(projectedRuntimeEventMsg(runtime.Event{
		Kind:                       runtime.EventAssistantMessage,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		CommittedEntryCount:        1,
		CommittedEntryStartSet:     true,
		Message:                    llm.Message{Role: llm.RoleAssistant, Content: "final answer", Phase: llm.MessagePhaseFinal},
	}))
	waitForTestCondition(t, 2*time.Second, "committed final rendered once", func() bool {
		normalized := normalizedOutput(out.String())
		return strings.Contains(normalized, "final answer")
	})
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program run failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("program did not terminate")
	}
	normalized := normalizedOutput(out.String())
	if got := strings.Count(normalized, "final answer"); got != 1 {
		t.Fatalf("expected streamed final plus commit to appear once, got %d in %q", got, normalized)
	}
}

func TestNativeStreamingTinyDeltasRemainContiguous(t *testing.T) {
	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), closedAskEvents())
	program := tea.NewProgram(model, tea.WithInput(strings.NewReader("")), tea.WithOutput(out), tea.WithoutSignals())
	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	for _, delta := range []string{"he", "llo", " ", "wor", "ld", "\n"} {
		program.Send(projectedRuntimeEventMsg(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: delta}))
	}
	time.Sleep(40 * time.Millisecond)
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program run failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("program did not terminate")
	}
	plain := xansi.Strip(out.String())
	if !strings.Contains(plain, "hello world") {
		t.Fatalf("expected contiguous streamed text from tiny deltas, got %q", plain)
	}
	if strings.Contains(plain, "he\nllo") || strings.Contains(plain, "wor\nld") {
		t.Fatalf("expected no per-delta forced newlines in streamed text, got %q", plain)
	}
}

func TestNativeStreamingWithoutNewlineStillVisible(t *testing.T) {
	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), closedAskEvents())
	program := tea.NewProgram(model, tea.WithInput(strings.NewReader("")), tea.WithOutput(out), tea.WithoutSignals())
	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	for _, delta := range []string{"long", " paragraph", " without", " newline"} {
		program.Send(projectedRuntimeEventMsg(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: delta}))
	}
	time.Sleep(40 * time.Millisecond)
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program run failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("program did not terminate")
	}
	if !strings.Contains(xansi.Strip(out.String()), "long paragraph without newline") {
		t.Fatalf("expected non-newline streaming text to still become visible, got %q", xansi.Strip(out.String()))
	}
}

func TestNativeProgramClearsResidualLivePadAfterStreamingCommit(t *testing.T) {
	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), closedAskEvents())
	program := tea.NewProgram(model, tea.WithInput(strings.NewReader("")), tea.WithOutput(out), tea.WithoutSignals())
	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 20})
	program.Send(projectedRuntimeEventMsg(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "line1\nline2"}))
	time.Sleep(30 * time.Millisecond)
	program.Send(tui.SetConversationMsg{Entries: []tui.TranscriptEntry{}, Ongoing: ""})
	time.Sleep(30 * time.Millisecond)
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program run failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("program did not terminate")
	}

	if model.nativeLiveRegionPad <= 0 {
		t.Fatalf("expected fresh conversation to restore native live region pad after streaming commit, got %d", model.nativeLiveRegionPad)
	}
	if model.nativeStreamingActive {
		t.Fatal("expected native streaming active flag cleared after commit")
	}
}

func TestNativeStreamingInterleavedRendersKeepsLinesLeftAligned(t *testing.T) {
	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), closedAskEvents())
	program := tea.NewProgram(model, tea.WithInput(strings.NewReader("")), tea.WithOutput(out), tea.WithoutSignals())
	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	expected := []string{"LADDER-01", "LADDER-02", "LADDER-03", "LADDER-04"}
	for _, token := range expected {
		program.Send(projectedRuntimeEventMsg(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: token + "\n"}))
		program.Send(spinnerTickMsg{})
	}
	time.Sleep(50 * time.Millisecond)
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program run failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("program did not terminate")
	}
	plain := xansi.Strip(out.String())
	normalized := strings.ReplaceAll(strings.ReplaceAll(plain, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(normalized, "\n")
	for index, token := range expected {
		prefix := "  "
		if index == 0 {
			prefix = "❮ "
		}
		expectedLine := prefix + token
		matched := false
		for _, line := range lines {
			trimmedRight := strings.TrimRight(line, " ")
			if trimmedRight == expectedLine {
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("expected streamed line %q to render as %q, got %q", token, expectedLine, normalized)
		}
	}
}
