package app

import (
	"core/cli/tui"
	"core/shared/transcript"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
)

func stripANSIText(v string) string {
	return strings.Join(strings.Fields(xansi.Strip(v)), " ")
}

func stripANSIPreserve(v string) string {
	return xansi.Strip(v)
}

func pendingSpinnerFrameText(frame int) string {
	return pendingToolSpinnerFrame(frame)
}

func pendingSpinnerLine(frame int, text string) string {
	return pendingSpinnerFrameText(frame) + " " + text
}

func collectNativeHistoryFlushText(msgs []tea.Msg) string {
	var out strings.Builder
	for _, msg := range msgs {
		flush, ok := msg.(nativeHistoryFlushMsg)
		if !ok {
			continue
		}
		out.WriteString(stripANSIPreserve(flush.Text))
		out.WriteByte('\n')
	}
	return out.String()
}

func makeStreamingLines(count int) string {
	parts := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		parts = append(parts, fmt.Sprintf("line-%02d", i))
	}
	return strings.Join(parts, "\n")
}

func TestNativeScrollbackStartupEmptyConversationEmitsBlankScreenSpacer(t *testing.T) {
	m := newProjectedStaticUIModel()

	next, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	updated, ok := next.(*uiModel)
	if !ok {
		t.Fatalf("unexpected model type %T", next)
	}
	m = updated
	if cmd == nil {
		t.Fatal("expected blank spacer command after first window size without transcript")
	}
	msg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg after first empty window size, got %T", cmd())
	}
	if !msg.AllowBlank {
		t.Fatal("expected blank spacer replay to allow whitespace-only flushes")
	}
	if got := strings.Count(msg.Text, "\n"); got != 30 {
		t.Fatalf("expected blank spacer to emit one empty screen worth of lines, got %d newlines", got)
	}
	if !m.nativeHistoryReplayed {
		t.Fatal("expected empty-history startup to mark native scrollback as replayed")
	}
	if m.nativeRenderedSnapshot != "" {
		t.Fatalf("expected empty-history startup to keep rendered history snapshot empty, got %q", m.nativeRenderedSnapshot)
	}
	if cmd := m.syncNativeHistoryFromTranscript(); cmd != nil {
		t.Fatalf("expected empty-history replay to emit spacer only once without resize, got %T", cmd())
	}
}

func TestNativeHeightOnlyResizeDoesNotScheduleFullReplay(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "seed"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}

	next, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(*uiModel)
	if cmd != nil {
		t.Fatalf("expected height-only resize to avoid full native replay scheduling, got %T", cmd)
	}
	if m.nativeResizeReplayToken != 0 {
		t.Fatalf("expected height-only resize to avoid changing replay token, got %d", m.nativeResizeReplayToken)
	}
}

func TestNativeResizeReplayInvalidatedAcrossModeSwitch(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "seed"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}

	next, resizeCmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = next.(*uiModel)
	if resizeCmd == nil {
		t.Fatal("expected debounced resize replay command")
	}
	staleToken := m.nativeResizeReplayToken

	_ = m.toggleTranscriptModeWithNativeReplay(false)
	if m.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected detail mode, got %q", m.view.Mode())
	}
	_ = m.toggleTranscriptModeWithNativeReplay(false)
	if m.view.Mode() != tui.ModeOngoing {
		t.Fatalf("expected ongoing mode, got %q", m.view.Mode())
	}
	if m.nativeResizeReplayToken == staleToken {
		t.Fatalf("expected mode switch to invalidate stale resize replay token %d", staleToken)
	}

	next, staleCmd := m.Update(nativeResizeReplayMsg{token: staleToken})
	m = next.(*uiModel)
	if staleCmd != nil {
		t.Fatalf("expected stale resize replay ignored after mode switch, got %T", staleCmd)
	}
}

func TestNativeScrollbackShrinkRebasesWithoutReemittingHistory(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "line one"}, {Role: "assistant", Text: "line two"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}

	m.transcriptEntries = m.transcriptEntries[:1]
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})
	cmd := m.syncNativeHistoryFromTranscript()
	if cmd != nil {
		t.Fatalf("expected no replay emission after transcript shrink, got %T", cmd())
	}
}

func TestNativeScrollbackIncrementalFlushConcatenationMatchesFullSnapshot(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "line 1"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}
	startupMsg, ok := startupCmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", startupCmd())
	}
	combined := startupMsg.Text + "\n"

	appendEntry := func(text string) {
		t.Helper()
		m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: text})
		m.transcriptEntries = append(m.transcriptEntries, tui.TranscriptEntry{Role: "assistant", Text: text})
		cmd := m.syncNativeHistoryFromTranscript()
		if cmd == nil {
			t.Fatalf("expected replay command after append %q", text)
		}
		msg, ok := cmd().(nativeHistoryFlushMsg)
		if !ok {
			t.Fatalf("expected nativeHistoryFlushMsg, got %T", cmd())
		}
		combined += msg.Text + "\n"
	}

	appendEntry("line 2\n\n```yaml\nroot:\n  key: value\n```")
	appendEntry("line 3 with `code`")

	combined = strings.TrimSuffix(combined, "\n")
	expected := renderStyledNativeProjectionLines(tui.ProjectCommittedOngoingTranscript(m.transcriptEntries, m.theme, m.nativeFormatterWidth).Lines(tui.TranscriptDivider), m.theme, m.nativeFormatterWidth)
	if combined != expected {
		t.Fatalf("expected concatenated incremental flush output to match full snapshot\ncombined=%q\nexpected=%q", combined, expected)
	}
}

func TestNativeCommittedEntriesStopsAtFirstUnresolvedToolCall(t *testing.T) {
	entries := []tui.TranscriptEntry{
		{Role: "user", Text: "prompt"},
		{Role: "tool_call", Text: "echo a", ToolCallID: "call_a", ToolCall: &transcript.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo a"}},
		{Role: "tool_call", Text: "echo b", ToolCallID: "call_b", ToolCall: &transcript.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo b"}},
		{Role: "tool_result_ok", Text: "out-b", ToolCallID: "call_b"},
	}

	committed := committedTranscriptEntriesForApp(entries)
	if len(committed) != 1 || committed[0].Text != "prompt" {
		t.Fatalf("expected only stable prefix committed, got %#v", committed)
	}
	pending := tui.PendingOngoingEntries(entries)
	if len(pending) != 3 {
		t.Fatalf("expected unresolved tool tail to stay pending, got %d entries", len(pending))
	}

	entries = append(entries, tui.TranscriptEntry{Role: "tool_result_ok", Text: "out-a", ToolCallID: "call_a"})
	committed = committedTranscriptEntriesForApp(entries)
	if len(committed) != len(entries) {
		t.Fatalf("expected full transcript committed once first unresolved call completes, got %d of %d entries", len(committed), len(entries))
	}
}
