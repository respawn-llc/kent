package tui

import (
	"core/shared/transcript"
	"strings"
	"testing"
)

func TestOngoingCompactsToolCallAndHidesThinking(t *testing.T) {
	m := NewModel(WithPreviewLines(20))
	m = updateModel(t, m, AppendTranscriptMsg{Role: "user", Text: "run command"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "thinking", Text: "internal trace"})
	m = updateModel(t, m, AppendTranscriptMsg{
		Role: "tool_call",
		Text: "pwd",
		ToolCall: &transcript.ToolCallMeta{
			IsShell:      true,
			Command:      "pwd",
			TimeoutLabel: "timeout: 5m",
		},
	})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "tool_result_ok", Text: "/tmp"})

	view := m.View()
	if strings.Contains(view, "internal trace") {
		t.Fatalf("expected thinking trace hidden in ongoing view, got %q", view)
	}
	if !strings.Contains(view, "pwd") {
		t.Fatalf("expected compact one-line tool input in ongoing view, got %q", view)
	}
	if strings.Contains(view, "workdir: /tmp") {
		t.Fatalf("expected tool input to stay one line in ongoing view, got %q", view)
	}
	if strings.Contains(view, "/tmp") {
		t.Fatalf("expected tool output to be omitted in ongoing view, got %q", view)
	}
}

func TestDetailReordersTrailingReasoningBeforeAssistantResponse(t *testing.T) {
	m := NewModel(WithPreviewLines(20))
	m = updateModel(t, m, AppendTranscriptMsg{Role: "user", Text: "u"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "final answer"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "reasoning", Text: "hidden plan"})
	m = updateModel(t, m, ToggleModeMsg{})

	detail := plainTranscript(m.View())
	if !containsInOrder(detail, "hidden plan", "final answer") {
		t.Fatalf("expected trailing reasoning rendered before assistant response, got %q", detail)
	}
}

func TestDetailReordersTrailingReasoningBeforeToolCalls(t *testing.T) {
	m := NewModel(WithPreviewLines(20))
	m = updateModel(t, m, AppendTranscriptMsg{Role: "tool_call", Text: "run"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "reasoning", Text: "decide to call tool"})
	m = updateModel(t, m, ToggleModeMsg{})

	detail := plainTranscript(m.View())
	if !containsInOrder(detail, "decide to call tool", "run") {
		t.Fatalf("expected trailing reasoning rendered before tool call, got %q", detail)
	}
}

func TestDetailRefreshesForLiveStreamingReasoning(t *testing.T) {
	m := NewModel(WithPreviewLines(20))
	m = updateModel(t, m, AppendTranscriptMsg{Role: "user", Text: "u"})
	m = updateModel(t, m, ToggleModeMsg{})
	m = updateModel(t, m, UpsertStreamingReasoningMsg{Key: "rs_1:summary:0", Role: "reasoning", Text: "Plan summary"})

	view := plainTranscript(m.View())
	if !strings.Contains(view, "Plan summary") {
		t.Fatalf("expected live reasoning to refresh detail snapshot, got %q", view)
	}

	m = updateModel(t, m, ClearStreamingReasoningMsg{})
	view = plainTranscript(m.View())
	if strings.Contains(view, "Plan summary") {
		t.Fatalf("expected live reasoning cleared from detail snapshot, got %q", view)
	}
}

func TestDeveloperContextRendersDetailOnly(t *testing.T) {
	m := NewModel(WithPreviewLines(20))
	m = updateModel(t, m, AppendTranscriptMsg{Role: roleDeveloperContext, Text: "AGENTS context block"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "done"})

	ongoing := plainTranscript(m.View())
	if strings.Contains(ongoing, "AGENTS context block") {
		t.Fatalf("expected developer context hidden in ongoing view, got %q", ongoing)
	}
	if !strings.Contains(ongoing, "done") {
		t.Fatalf("expected assistant visible in ongoing view, got %q", ongoing)
	}

	m = updateModel(t, m, ToggleModeMsg{})
	detail := plainTranscript(m.View())
	if !containsInOrder(detail, "AGENTS context block", "done") {
		t.Fatalf("expected developer context visible in detail view, got %q", detail)
	}
}

func TestDeveloperFeedbackRendersInOngoingAndInterruptionRendersOnlyInDetail(t *testing.T) {
	m := NewModel(WithPreviewLines(20))
	m = updateModel(t, m, AppendTranscriptMsg{Role: roleDeveloperFeedback, Text: "phase mismatch warning"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: roleInterruption, Text: "User interrupted you"})

	ongoing := plainTranscript(m.View())
	if !strings.Contains(ongoing, "phase mismatch warning") {
		t.Fatalf("expected ongoing-visible developer feedback, got %q", ongoing)
	}
	if strings.Contains(ongoing, "User interrupted you") || strings.Contains(ongoing, interruptionUserVisibleText) {
		t.Fatalf("expected interruption hidden from ongoing transcript, got %q", ongoing)
	}

	m = updateModel(t, m, ToggleModeMsg{})
	detail := plainTranscript(m.View())
	if !containsInOrder(detail, "phase mismatch warning", interruptionUserVisibleText) {
		t.Fatalf("expected developer feedback and interruption visible in detail view, got %q", detail)
	}
	if strings.Contains(detail, "User interrupted you") {
		t.Fatalf("expected detail interruption to hide model-facing wording, got %q", detail)
	}
}

func TestDeveloperErrorFeedbackRendersInOngoing(t *testing.T) {
	m := NewModel(WithPreviewLines(20))
	m = updateModel(t, m, AppendTranscriptMsg{Role: roleDeveloperErrorFeedback, Text: "unable to start run"})

	ongoing := plainTranscript(m.View())
	if !strings.Contains(ongoing, "unable to start run") {
		t.Fatalf("expected developer error feedback visible in ongoing view, got %q", ongoing)
	}

	m = updateModel(t, m, ToggleModeMsg{})
	detail := plainTranscript(m.View())
	if !strings.Contains(detail, "unable to start run") {
		t.Fatalf("expected developer error feedback visible in detail view, got %q", detail)
	}
}

func TestCompactionSoonReminderRendersDetailOnly(t *testing.T) {
	m := NewModel(WithPreviewLines(20))
	m = updateModel(t, m, AppendTranscriptMsg{Role: "warning", Text: "warning marker"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "done"})

	ongoing := plainTranscript(m.View())
	if strings.Contains(ongoing, "warning marker") {
		t.Fatalf("expected compaction soon reminder hidden in ongoing view, got %q", ongoing)
	}
	if !strings.Contains(ongoing, "done") {
		t.Fatalf("expected assistant visible in ongoing view, got %q", ongoing)
	}

	m = updateModel(t, m, ToggleModeMsg{})
	detail := plainTranscript(m.View())
	if !containsInOrder(detail, "warning marker", "done") {
		t.Fatalf("expected compaction soon reminder visible in detail view, got %q", detail)
	}
}

func TestHeadlessModeContextVariantsRenderDetailOnly(t *testing.T) {
	m := NewModel(WithPreviewLines(30))
	m = updateModel(t, m, AppendTranscriptMsg{Role: roleDeveloperContext, Text: "headless mode instructions"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: roleDeveloperContext, Text: "interactive mode instructions"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "done"})

	ongoing := plainTranscript(m.View())
	if strings.Contains(ongoing, "headless mode instructions") || strings.Contains(ongoing, "interactive mode instructions") {
		t.Fatalf("expected headless context variants hidden in ongoing view, got %q", ongoing)
	}

	m = updateModel(t, m, ToggleModeMsg{})
	detail := plainTranscript(m.View())
	if !containsInOrder(detail, "headless mode instructions", "interactive mode instructions", "done") {
		t.Fatalf("expected headless context variants visible in detail view, got %q", detail)
	}
}

func TestManualCompactionCarryoverRenderingByMode(t *testing.T) {
	m := NewModel(WithPreviewLines(20))
	m = updateModel(t, m, AppendTranscriptMsg{
		Role: roleManualCompactionCarryover,
		Text: "# Last user message before manual compaction\n\nplease keep tests green",
	})

	ongoing := plainTranscript(m.View())
	if strings.Contains(ongoing, "Last user message before manual compaction") || strings.Contains(ongoing, "please keep tests green") {
		t.Fatalf("expected manual compaction carryover hidden in ongoing view, got %q", ongoing)
	}

	m = updateModel(t, m, ToggleModeMsg{})
	detail := plainTranscript(m.View())
	if !containsInOrder(detail, "# Last user message before manual compaction", "please keep tests green") {
		t.Fatalf("expected manual compaction carryover visible in detail view, got %q", detail)
	}
}
