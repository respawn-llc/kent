package tui

import (
	"testing"

	"core/shared/transcript"
	"github.com/charmbracelet/lipgloss"
)

func TestStyleForRoleAssistantCommentaryMatchesAssistant(t *testing.T) {
	m := NewModel(WithTheme("dark"))
	palette := m.palette()
	text := "same assistant text"

	assistant := styleForRole(RenderIntentAssistant, palette).Render(text)
	commentary := styleForRole(RenderIntentAssistantCommentary, palette).Render(text)
	if commentary != assistant {
		t.Fatalf("expected assistant commentary style to match assistant style, assistant=%q commentary=%q", assistant, commentary)
	}
}

func TestToolResultIndexFindMatchingToolResultIndexSkipsConsumedDuplicateResults(t *testing.T) {
	entries := []TranscriptEntry{
		{Role: "tool_call", ToolCallID: "call_a"},
		{Role: "tool_result_ok", ToolCallID: "call_a", Text: "first"},
		{Role: "assistant", Text: "gap"},
		{Role: "tool_result_ok", ToolCallID: "call_a", Text: "second"},
	}
	index := buildToolResultIndex(entries)
	consumed := map[int]struct{}{1: {}}

	got := index.findMatchingToolResultIndex(entries, 0, consumed)
	if got != 3 {
		t.Fatalf("expected later unmatched duplicate result, got %d", got)
	}

	got = index.findMatchingToolResultIndex(entries, 0, consumed)
	if got != 3 {
		t.Fatalf("expected repeated lookup to stay on same unmatched result, got %d", got)
	}

	consumed[3] = struct{}{}
	got = index.findMatchingToolResultIndex(entries, 0, consumed)
	if got != -1 {
		t.Fatalf("expected no remaining matches after consuming duplicates, got %d", got)
	}
}

func TestToolResultIndexFindMatchingToolResultIndexPrefersAdjacentMatch(t *testing.T) {
	entries := []TranscriptEntry{
		{Role: "tool_call", ToolCallID: "call_a"},
		{Role: "tool_result_ok", ToolCallID: "call_a", Text: "adjacent"},
		{Role: "tool_result_ok", ToolCallID: "call_a", Text: "later"},
	}
	index := buildToolResultIndex(entries)

	got := index.findMatchingToolResultIndex(entries, 0, map[int]struct{}{})
	if got != 1 {
		t.Fatalf("expected adjacent result to win, got %d", got)
	}
}

func TestUsesWarningShellSymbolForRawOrTruncatedShellOutput(t *testing.T) {
	testCases := []struct {
		name string
		role RenderIntent
		meta *transcript.ToolCallMeta
		want bool
	}{
		{
			name: "raw shell request",
			role: RenderIntentToolShell,
			meta: &transcript.ToolCallMeta{ToolName: "exec_command", IsShell: true, RawOutputRequested: true},
			want: true,
		},
		{
			name: "truncated shell success",
			role: RenderIntentToolShellSuccess,
			meta: &transcript.ToolCallMeta{ToolName: "exec_command", IsShell: true, OutputTruncated: true},
			want: true,
		},
		{
			name: "truncated shell error",
			role: RenderIntentToolShellError,
			meta: &transcript.ToolCallMeta{ToolName: "exec_command", IsShell: true, OutputTruncated: true},
			want: true,
		},
		{
			name: "normal shell success",
			role: RenderIntentToolShellSuccess,
			meta: &transcript.ToolCallMeta{ToolName: "exec_command", IsShell: true},
			want: false,
		},
		{
			name: "non shell role",
			role: RenderIntentToolSuccess,
			meta: &transcript.ToolCallMeta{ToolName: "exec_command", IsShell: true, OutputTruncated: true},
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := usesWarningShellSymbol(tc.role, tc.meta); got != tc.want {
				t.Fatalf("usesWarningShellSymbol = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestResultOnlyToolBlockRoleUsesShellMetadata(t *testing.T) {
	meta := &transcript.ToolCallMeta{ToolName: "exec_command", IsShell: true, OutputTruncated: true}

	if got := resultOnlyToolBlockRole(TranscriptRoleToolResultOK, meta); got != RenderIntentToolShellSuccess {
		t.Fatalf("success role = %q, want %q", got, RenderIntentToolShellSuccess)
	}
	if got := resultOnlyToolBlockRole(TranscriptRoleToolResultError, meta); got != RenderIntentToolShellError {
		t.Fatalf("error role = %q, want %q", got, RenderIntentToolShellError)
	}
}

func TestShellWarningSymbolOverridePreservesPrefixWidth(t *testing.T) {
	meta := &transcript.ToolCallMeta{ToolName: "exec_command", IsShell: true, OutputTruncated: true}
	m := NewModel()
	m.toolSymbolGap = 2

	got := m.shellWarningSymbolOverride(RenderIntentToolShellSuccess, meta)
	wantWidth := lipgloss.Width(m.entryPrefix(RenderIntentToolShellSuccess, ""))

	if lipgloss.Width(got) != wantWidth {
		t.Fatalf("warning shell prefix width = %d, want %d", lipgloss.Width(got), wantWidth)
	}
}
