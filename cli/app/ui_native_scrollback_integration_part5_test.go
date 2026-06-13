package app

import (
	"core/cli/tui"
	"core/server/llm"
	"core/server/runtime"
	"core/shared/clientui"
	"bytes"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestNativeHistoryFlushesPreserveScheduledOrderWhenDeliveredOutOfOrder(t *testing.T) {
	out := &bytes.Buffer{}
	model := newProjectedStaticUIModel()
	firstCmd := model.emitNativeRenderedTextWithOptions("assistant final\n", false)
	secondCmd := model.emitNativeRenderedTextWithOptions("queued user\n", false)
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

	program := startNativeProgram(t, model, out)

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
	program.Wait(2 * time.Second)

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
	program := startNativeProgram(t, model, out)
	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	program.Send(tea.KeyMsg{Type: tea.KeyShiftTab})
	time.Sleep(20 * time.Millisecond)
	program.Send(projectedRuntimeEventMsg(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "hidden-delta"}))
	time.Sleep(20 * time.Millisecond)
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	program.Wait(2 * time.Second)
	if strings.Contains(normalizedOutput(out.String()), "hidden-delta") {
		t.Fatalf("expected assistant delta to stay suppressed while in detail mode, got %q", normalizedOutput(out.String()))
	}
}

func TestNativeStreamedFinalThenCommitAppearsOnceInScrollback(t *testing.T) {
	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), closedAskEvents())
	program := startNativeProgram(t, model, out)
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
		return strings.TrimSpace(model.view.OngoingStreamingText()) == "" &&
			!model.nativeStreamingActive &&
			model.nativeFlushedSequence >= model.nativeFlushSequence &&
			model.waitRuntimeEventAfterFlushSequence == 0 &&
			len(model.nativePendingFlushes) == 0 &&
			strings.Contains(normalizedOutput(replayTerminalPlainText(out.String())), "final answer")
	})
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	program.Wait(2 * time.Second)
	finalTerminal := normalizedOutput(replayTerminalPlainText(out.String()))
	if got := strings.Count(finalTerminal, "final answer"); got != 1 {
		t.Fatalf("expected streamed final plus commit to appear once in terminal, got %d in %q", got, finalTerminal)
	}
	committed := normalizedOutput(model.view.OngoingCommittedSnapshot())
	if got := strings.Count(committed, "final answer"); got != 1 {
		t.Fatalf("expected committed final once in rendered committed ongoing transcript, got %d in %q", got, committed)
	}
}

func TestNativeStreamedMultilineMarkdownFinalThenCommitAppearsOnceInScrollback(t *testing.T) {
	out := &bytes.Buffer{}
	runtimeEvents := make(chan clientui.Event, 8)
	model := newProjectedTestUIModel(nil, runtimeEvents, closedAskEvents())
	program := startNativeProgram(t, model, out)
	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 80, Height: 30})

	prefix := "Captured the Kent project board in the browser:\n\n"
	path := "/Users/nek/.builder/worktrees/builder-cli-c2f75fc8-68f5-4deb-a23c-21cc5820436d/gui-fixes/.builder/proofs/gui-browser-kent-board/screenshot-1779219845652.png"
	tail := "\n\nI opened it via the browser client against `ws://127.0.0.1:53082/rpc`; the board URL was `http://127.0.0.1:1433/projects/project-94b18685-19ed-4513-96bb-bcffa10410ff?workflowId=workflow-9f88e01d-f923-45a6-8c96-95298da24815&taskId=&resumeRunId=`."
	finalText := prefix + path + tail

	runtimeEvents <- projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, StepID: "step-1", AssistantDelta: prefix + path + "\n"})
	waitForTestCondition(t, 2*time.Second, "streamed markdown prefix visible", func() bool {
		return strings.Contains(normalizedOutput(out.String()), "Captured the Kent project board")
	})
	runtimeEvents <- projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, StepID: "step-1", AssistantDelta: tail})
	runtimeEvents <- projectRuntimeEvent(runtime.Event{
		Kind:                       runtime.EventAssistantMessage,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		CommittedEntryCount:        1,
		CommittedEntryStartSet:     true,
		Message:                    llm.Message{Role: llm.RoleAssistant, Content: finalText, Phase: llm.MessagePhaseFinal},
	})
	waitForTestCondition(t, 2*time.Second, "committed multiline final rendered", func() bool {
		return strings.TrimSpace(model.view.OngoingStreamingText()) == "" &&
			!model.nativeStreamingActive &&
			model.nativeFlushedSequence >= model.nativeFlushSequence &&
			model.waitRuntimeEventAfterFlushSequence == 0 &&
			len(model.nativePendingFlushes) == 0 &&
			strings.Contains(normalizedOutput(out.String()), "I opened it via the browser client")
	})
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	program.Wait(2 * time.Second)
	normalized := normalizedOutput(out.String())
	if got := strings.Count(normalized, "Captured the Kent project board"); got != 1 {
		t.Fatalf("expected streamed multiline final prefix once, got %d in %q", got, normalized)
	}
	finalTerminal := normalizedOutput(replayTerminalPlainText(out.String()))
	if got := strings.Count(finalTerminal, "Captured the Kent project board"); got != 1 {
		t.Fatalf("expected final terminal prefix once, got %d in %q", got, finalTerminal)
	}
	if got := strings.Count(finalTerminal, "I opened it via the browser client"); got != 1 {
		t.Fatalf("expected final terminal tail once, got %d in %q", got, finalTerminal)
	}
	committed := normalizedOutput(model.view.OngoingCommittedSnapshot())
	if got := strings.Count(committed, "Captured the Kent project board"); got != 1 {
		t.Fatalf("expected committed multiline final prefix once, got %d in %q", got, committed)
	}
	if got := strings.Count(committed, "I opened it via the browser client"); got != 1 {
		t.Fatalf("expected committed multiline final tail once, got %d in %q", got, committed)
	}
}

type replayTerminal struct {
	lines [][]rune
	row   int
	col   int
}

func replayTerminalPlainText(output string) string {
	terminal := &replayTerminal{}
	parser := xansi.NewParser()
	parser.SetHandler(xansi.Handler{
		Print: terminal.print,
		Execute: func(b byte) {
			switch b {
			case '\r':
				terminal.col = 0
			case '\n':
				terminal.row++
				terminal.col = 0
			case '\b':
				if terminal.col > 0 {
					terminal.col--
				}
			case '\t':
				spaces := 4 - terminal.col%4
				for idx := 0; idx < spaces; idx++ {
					terminal.print(' ')
				}
			}
		},
		HandleCsi: func(cmd xansi.Cmd, params xansi.Params) {
			terminal.handleCSI(cmd, params)
		},
	})
	for idx := 0; idx < len(output); idx++ {
		parser.Advance(output[idx])
	}
	return terminal.String()
}

func (t *replayTerminal) print(r rune) {
	if r == '\n' || r == '\r' {
		return
	}
	t.ensureLine(t.row)
	line := t.lines[t.row]
	for len(line) < t.col {
		line = append(line, ' ')
	}
	if t.col < len(line) {
		line[t.col] = r
	} else {
		line = append(line, r)
	}
	t.lines[t.row] = line
	t.col++
}

func (t *replayTerminal) handleCSI(cmd xansi.Cmd, params xansi.Params) {
	n, _, ok := params.Param(0, 1)
	if !ok {
		n = 1
	}
	switch cmd.Final() {
	case 'A':
		t.row -= n
		if t.row < 0 {
			t.row = 0
		}
	case 'B':
		t.row += n
	case 'C':
		t.col += n
	case 'D':
		t.col -= n
		if t.col < 0 {
			t.col = 0
		}
	case 'G':
		t.col = max(0, n-1)
	case 'H', 'f':
		col, _, _ := params.Param(1, 1)
		t.row = max(0, n-1)
		t.col = max(0, col-1)
	case 'J':
		mode, _, _ := params.Param(0, 0)
		switch mode {
		case 0:
			t.eraseScreenBelow()
		case 2:
			t.lines = nil
			t.row = 0
			t.col = 0
		}
	case 'K':
		mode, _, _ := params.Param(0, 0)
		t.eraseLine(mode)
	}
}

func (t *replayTerminal) eraseScreenBelow() {
	t.ensureLine(t.row)
	if t.col < len(t.lines[t.row]) {
		t.lines[t.row] = t.lines[t.row][:t.col]
	}
	for idx := t.row + 1; idx < len(t.lines); idx++ {
		t.lines[idx] = nil
	}
}

func (t *replayTerminal) eraseLine(mode int) {
	t.ensureLine(t.row)
	switch mode {
	case 0:
		if t.col < len(t.lines[t.row]) {
			t.lines[t.row] = t.lines[t.row][:t.col]
		}
	case 1:
		for idx := 0; idx <= t.col && idx < len(t.lines[t.row]); idx++ {
			t.lines[t.row][idx] = ' '
		}
	case 2:
		t.lines[t.row] = nil
	}
}

func (t *replayTerminal) ensureLine(row int) {
	for len(t.lines) <= row {
		t.lines = append(t.lines, nil)
	}
}

func (t *replayTerminal) String() string {
	lines := make([]string, 0, len(t.lines))
	for _, line := range t.lines {
		lines = append(lines, strings.TrimRight(string(line), " "))
	}
	return strings.Join(lines, "\n")
}

func TestNativeStreamingTinyDeltasRemainContiguous(t *testing.T) {
	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), closedAskEvents())
	program := startNativeProgram(t, model, out)
	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	for _, delta := range []string{"he", "llo", " ", "wor", "ld", "\n"} {
		program.Send(projectedRuntimeEventMsg(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: delta}))
	}
	time.Sleep(40 * time.Millisecond)
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	program.Wait(2 * time.Second)
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
	program := startNativeProgram(t, model, out)
	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	for _, delta := range []string{"long", " paragraph", " without", " newline"} {
		program.Send(projectedRuntimeEventMsg(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: delta}))
	}
	time.Sleep(40 * time.Millisecond)
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	program.Wait(2 * time.Second)
	if !strings.Contains(xansi.Strip(out.String()), "long paragraph without newline") {
		t.Fatalf("expected non-newline streaming text to still become visible, got %q", xansi.Strip(out.String()))
	}
}

func TestNativeProgramClearsResidualLivePadAfterStreamingCommit(t *testing.T) {
	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), closedAskEvents())
	program := startNativeProgram(t, model, out)
	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 20})
	program.Send(projectedRuntimeEventMsg(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "line1\nline2"}))
	time.Sleep(30 * time.Millisecond)
	program.Send(tui.SetConversationMsg{Entries: []tui.TranscriptEntry{}, Ongoing: ""})
	time.Sleep(30 * time.Millisecond)
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	program.Wait(2 * time.Second)

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
	program := startNativeProgram(t, model, out)
	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	expected := []string{"LADDER-01", "LADDER-02", "LADDER-03", "LADDER-04"}
	for _, token := range expected {
		program.Send(projectedRuntimeEventMsg(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: token + "\n"}))
		program.Send(spinnerTickMsg{})
	}
	time.Sleep(50 * time.Millisecond)
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	program.Wait(2 * time.Second)
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
