package app

import (
	"fmt"
	"strings"
	"testing"

	"core/cli/tui"

	tea "github.com/charmbracelet/bubbletea"
)

type testUILogger struct {
	lines []string
}

func (l *testUILogger) Logf(format string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func TestHandleRenderDiagnosticRoutesThroughUpdateAndAutoClears(t *testing.T) {
	logger := &testUILogger{}
	m := newProjectedStaticUIModel(WithUILogger(logger))

	m.handleRenderDiagnostic(tui.RenderDiagnostic{
		Component: "markdown_renderer",
		Message:   "markdown renderer disabled, falling back to plain text: boom",
		Severity:  tui.RenderDiagnosticSeverityWarn,
	})
	renderMsg, ok := startupCmdMessage[renderDiagnosticMsg](m.startupCmds)
	if !ok {
		t.Fatalf("expected renderDiagnosticMsg in startup commands, got %d command(s)", len(m.startupCmds))
	}
	next, cmd := m.Update(renderMsg)
	updated := next.(*uiModel)

	if got := strings.TrimSpace(updated.transientStatus); got != "markdown renderer disabled, falling back to plain text: boom" {
		t.Fatalf("expected transient status set, got %q", got)
	}
	if updated.transientStatusKind != uiStatusNoticeNeutral {
		t.Fatalf("expected neutral notice kind for warn diagnostic, got %d", updated.transientStatusKind)
	}
	if cmd == nil {
		t.Fatal("expected transient status clear cmd")
	}
	clearMsg := cmd()
	clear, ok := clearMsg.(clearTransientStatusMsg)
	if !ok {
		t.Fatalf("expected clearTransientStatusMsg, got %T", clearMsg)
	}
	next, _ = updated.Update(clear)
	updated = next.(*uiModel)
	if updated.transientStatus != "" {
		t.Fatalf("expected transient status cleared, got %q", updated.transientStatus)
	}
	if updated.transientStatusKind != uiStatusNoticeNeutral {
		t.Fatalf("expected neutral status kind after clear, got %d", updated.transientStatusKind)
	}
}

func TestApplyRunLoggerDiagnosticSetsErrorTransientStatus(t *testing.T) {
	logger := &testUILogger{}
	m := newProjectedStaticUIModel(WithUILogger(logger))

	m.startupCmds = append(m.startupCmds, func() tea.Msg {
		return runLoggerDiagnosticMsg{diagnostic: runLoggerDiagnostic{
			Kind:    "write_failed",
			Message: "run log write failed; observability degraded: disk full",
		}}
	})
	runLogMsg, ok := startupCmdMessage[runLoggerDiagnosticMsg](m.startupCmds)
	if !ok {
		t.Fatalf("expected runLoggerDiagnosticMsg in startup commands, got %d command(s)", len(m.startupCmds))
	}
	next, _ := m.Update(runLogMsg)
	updated := next.(*uiModel)

	if got := strings.TrimSpace(updated.transientStatus); got != "run log write failed; observability degraded: disk full" {
		t.Fatalf("expected transient status set, got %q", got)
	}
	if updated.transientStatusKind != uiStatusNoticeError {
		t.Fatalf("expected error notice kind, got %d", updated.transientStatusKind)
	}
}
