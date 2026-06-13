package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"core/shared/theme"
	"core/shared/transcript"
)

func TestGoalFeedbackRendersInPrimaryColor(t *testing.T) {
	for _, themeName := range []string{"dark", "light"} {
		m := NewModel(WithTheme(themeName), WithPreviewLines(4))
		m = updateModel(t, m, SetViewportSizeMsg{Lines: 4, Width: 80})
		m = updateModel(t, m, AppendTranscriptMsg{Role: TranscriptRole(transcript.EntryRoleGoalFeedback), Text: "Goal paused"})
		assertGoalFeedbackView(t, m.View(), themeName, "ongoing")
	}
}

func TestGoalFeedbackExpandedDetailKeepsPromptTextNormal(t *testing.T) {
	m := NewModel(WithCompactDetail(), WithPreviewLines(8))
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 8, Width: 80})
	m = updateModel(t, m, AppendTranscriptMsg{
		Role:        TranscriptRole(transcript.EntryRoleGoalFeedback),
		Text:        "Full goal prompt line one\nFull goal prompt line two",
		OngoingText: "Goal complete. Cooked for 31m",
	})
	m = updateModel(t, m, ToggleModeMsg{})

	collapsedLine := goalFeedbackLine(t, m.View(), "Goal complete. Cooked for 31m")
	primary := rgbColorFromHex(theme.ResolvePalette(m.theme).App.Primary.TrueColor)
	if got := countColor(extractForegroundTrueColors(collapsedLine), primary); got < 3 {
		t.Fatalf("expected collapsed detail icon and text to use primary color, count=%d line=%q", got, collapsedLine)
	}

	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	expandedLine := goalFeedbackLine(t, m.View(), "Full goal prompt line one")
	if got := countColor(extractForegroundTrueColors(expandedLine), primary); got != 2 {
		t.Fatalf("expected expanded detail to keep only rail and icon primary, count=%d line=%q", got, expandedLine)
	}
}

func assertGoalFeedbackView(t *testing.T, view string, themeName string, mode string) {
	t.Helper()
	if !strings.Contains(view, "ℹ") || !strings.Contains(view, "Goal paused") {
		t.Fatalf("expected %s %s goal feedback info line, got %q", themeName, mode, view)
	}
	goalLine := goalFeedbackLine(t, view, "Goal paused")
	primary := rgbColorFromHex(theme.ResolvePalette(themeName).App.Primary.TrueColor)
	if got := countColor(extractForegroundTrueColors(goalLine), primary); got < 2 {
		t.Fatalf("expected %s %s goal feedback icon and text to use primary color %+v, count=%d line=%q", themeName, mode, primary, got, goalLine)
	}
}

func goalFeedbackLine(t *testing.T, view string, text string) string {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, text) {
			return line
		}
	}
	t.Fatalf("expected goal feedback line containing %q, got %q", text, view)
	return ""
}
