package app

import (
	"strings"
	"testing"

	"core/cli/tui"
)

func TestNativePendingPartitionExcludesCommittedTransientRows(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.theme = "dark"
	m.transcriptEntries = []tui.TranscriptEntry{
		{Role: tui.TranscriptRoleUser, Text: "prompt", Committed: true},
		{Role: "reviewer_status", Text: "Supervisor ran.", Transient: true, Committed: true},
		{Role: tui.TranscriptRoleToolCall, Text: "pending shell", ToolCallID: "call_a", Transient: true},
	}

	lines := uiViewLayout{model: m}.renderNativePendingLines(80)
	plain := stripANSIPreserve(strings.Join(lines, "\n"))
	if strings.Contains(plain, "Supervisor ran.") {
		t.Fatalf("committed transient row stayed in pending live region: %q", plain)
	}
	if !strings.Contains(plain, "pending shell") {
		t.Fatalf("expected unresolved tool to remain pending, got %q", plain)
	}
}
