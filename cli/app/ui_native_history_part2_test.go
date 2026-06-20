package app

import (
	"core/cli/tui"
	"core/shared/transcript"
	"fmt"
	"strings"
	"testing"
)

func TestNativePendingToolEntriesTrackParallelCommitFrontier(t *testing.T) {
	entries := []tui.TranscriptEntry{
		{Role: "user", Text: "prompt"},
		{Role: "tool_call", Text: "echo a", ToolCallID: "call_a", ToolCall: &transcript.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo a"}},
		{Role: "tool_call", Text: "echo b", ToolCallID: "call_b", ToolCall: &transcript.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo b"}},
		{Role: "tool_result_ok", Text: "out-b", ToolCallID: "call_b"},
	}

	pending := tui.PendingToolEntries(entries)
	if len(pending) != 3 {
		t.Fatalf("expected pending tool calls plus matching completed result, got %#v", pending)
	}
	if pending[0].ToolCallID != "call_a" || pending[0].Role != "tool_call" || pending[0].Text != "echo a" {
		t.Fatalf("unexpected first pending tool entry: %#v", pending[0])
	}
	if pending[1].ToolCallID != "call_b" || pending[1].Role != "tool_call" || pending[1].Text != "echo b" {
		t.Fatalf("unexpected second pending tool entry: %#v", pending[1])
	}
	if pending[2].ToolCallID != "call_b" || pending[2].Role != "tool_result_ok" || pending[2].Text != "out-b" {
		t.Fatalf("unexpected pending tool result entry: %#v", pending[2])
	}
}

func TestUIInitClearsScreen(t *testing.T) {
	m := newProjectedStaticUIModel()
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected init command")
	}
}

func TestNativeScrollbackReplayIsChunkedForLargeSessions(t *testing.T) {
	lines := make([]string, 0, 10000)
	for i := 0; i < 10000; i++ {
		lines = append(lines, fmt.Sprintf("entry-%d", i))
	}
	rendered := strings.Join(lines, "\n")
	chunks := splitNativeScrollbackChunks(rendered, 4096)
	if len(chunks) < 2 {
		t.Fatalf("expected chunked replay for large history, got %d chunk(s)", len(chunks))
	}
	for idx, chunk := range chunks {
		if len(chunk) > 8192 {
			t.Fatalf("expected bounded chunk size, chunk %d has %d bytes", idx, len(chunk))
		}
	}
}

func TestNativeOngoingShrinksLiveRegionAfterInputShrinkWhenNotStreaming(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.termHeight = 20
	m.windowSizeKnown = true
	m.input = "line 1\nline 2\nline 3"
	m.layout().syncViewport()
	firstPad := m.nativeLiveRegionPad
	first := strings.Split(m.View(), "\n")
	if len(first) != 20 {
		t.Fatalf("expected fresh conversation to fill terminal height before shrink, got %d lines", len(first))
	}
	m.input = ""
	m.layout().syncViewport()
	secondPad := m.nativeLiveRegionPad
	second := strings.Split(m.View(), "\n")
	if len(second) != 20 {
		t.Fatalf("expected fresh conversation to keep filling terminal height after shrink, got %d lines", len(second))
	}
	if secondPad <= firstPad {
		t.Fatalf("expected top padding to grow after input shrink, first=%d second=%d", firstPad, secondPad)
	}
}

func TestNativeOngoingDoesNotRenderBeforeWindowSizeKnown(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.input = "hello"
	got := stripANSIPreserve(m.View())
	if got != "" {
		t.Fatalf("expected no native ongoing render before first window size, got %q", got)
	}
}

func TestNativeOngoingClearsLiveRegionPadWhenStreamingEnds(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.termHeight = 12
	m.windowSizeKnown = true
	m.forwardToView(tui.SetConversationMsg{Ongoing: "line1\nline2"})
	m.layout().syncViewport()
	if !m.nativeStreamingActive {
		t.Fatal("expected streaming active after ongoing stream snapshot")
	}
	m.forwardToView(tui.SetConversationMsg{Ongoing: ""})
	m.layout().syncViewport()
	if m.nativeLiveRegionPad <= 0 {
		t.Fatalf("expected fresh conversation to restore top padding after streaming ends, got %d", m.nativeLiveRegionPad)
	}
	if m.nativeStreamingActive {
		t.Fatal("expected streaming inactive after ongoing clears")
	}
}
