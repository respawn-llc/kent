package tui

import (
	"builder/shared/transcript"
	"builder/shared/uiglyphs"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func leadingViewportSelectableDetailEntry(t *testing.T, m Model) int {
	t.Helper()

	lookup := newDetailProjectionLookup(m.detailViewProjection())
	owners := lookup.projection.DetailViewport(m.currentDetailViewportState()).Owners
	for _, entryIndex := range owners {
		if lookup.blockIndexForEntry(entryIndex) < 0 {
			continue
		}
		return entryIndex
	}
	t.Fatalf("expected visible selectable detail entry, owners=%+v", owners)
	return -1
}

func newTallExpandedCenterRailModel(t *testing.T) Model {
	t.Helper()

	m := NewModel(WithCompactDetail(), WithPreviewLines(6))
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 6, Width: 80})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "intro line 0\nintro line 1\nintro line 2"})
	m = updateModel(t, m, AppendTranscriptMsg{
		Role:       "tool_call",
		Text:       "long-command",
		ToolCallID: "call_1",
		ToolCall:   &transcript.ToolCallMeta{ToolName: "exec_command", IsShell: true, Command: "long-command"},
	})
	outputLines := make([]string, 0, 12)
	for idx := 0; idx < 12; idx++ {
		outputLines = append(outputLines, fmt.Sprintf("output line %02d", idx))
	}
	m = updateModel(t, m, AppendTranscriptMsg{Role: "tool_result_ok", ToolCallID: "call_1", Text: strings.Join(outputLines, "\n")})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "tail"})
	m = updateModel(t, m, ToggleModeMsg{})
	m.detailSelectedEntry = 1
	m.detailSelectedActive = true
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m.detailBottomAnchor = false
	m.detailScroll = 0
	m.refreshDetailViewport()
	m.detailSelectedEntry = 0
	m.detailSelectedActive = true
	return m
}

func assertCenterRailOnExpandedOutput(t *testing.T, m Model) {
	t.Helper()

	lines := strings.Split(xansi.Strip(m.View()), "\n")
	center := m.viewportLines / 2
	if center >= len(lines) {
		t.Fatalf("center line %d outside rendered lines %d", center, len(lines))
	}
	if !strings.HasPrefix(lines[center], uiglyphs.SelectionRailGlyph) || !strings.Contains(lines[center], "output line") {
		t.Fatalf("expected selected rail on center output line, got center=%q view=%q", lines[center], xansi.Strip(m.View()))
	}
}

func assertRailBearingSpacerLine(t *testing.T, line string, modeBg rgbColor, railColor rgbColor) {
	t.Helper()

	plain := xansi.Strip(line)
	if !strings.HasPrefix(plain, uiglyphs.SelectionRailGlyph) {
		t.Fatalf("expected spacer line to extend selection rail, got %q", plain)
	}
	if strings.TrimSpace(strings.TrimPrefix(plain, uiglyphs.SelectionRailGlyph)) != "" {
		t.Fatalf("expected highlighted spacer line to be blank after rail, got %q", plain)
	}
	if !strings.Contains(line, fmt.Sprintf("48;2;%d;%d;%d", modeBg.r, modeBg.g, modeBg.b)) {
		t.Fatalf("expected spacer line to use mode background, got %q", line)
	}
	if !containsColor(extractForegroundTrueColors(line), railColor) {
		t.Fatalf("expected spacer rail to use selected rail color, got %q", line)
	}
}

func centerVisibleSelectableDetailEntry(t *testing.T, m Model) int {
	t.Helper()

	lookup := newDetailProjectionLookup(m.detailViewProjection())
	owners := lookup.projection.DetailViewport(m.currentDetailViewportState()).Owners
	if len(owners) == 0 {
		t.Fatal("expected visible detail entries")
	}
	anchor := m.viewportLines / 2
	if anchor >= len(owners) {
		anchor = len(owners) - 1
	}
	bestEntry := -1
	bestDistance := len(owners) + 1
	for lineIndex, entryIndex := range owners {
		if lookup.blockIndexForEntry(entryIndex) < 0 {
			continue
		}
		distance := detailLineDistance(lineIndex, anchor)
		if distance >= bestDistance {
			continue
		}
		bestEntry = entryIndex
		bestDistance = distance
	}
	if bestEntry < 0 {
		t.Fatalf("expected center visible selectable detail entry, owners=%+v", owners)
	}
	return bestEntry
}

func selectedDetailDistanceFromCenter(t *testing.T, m Model) int {
	t.Helper()

	visible := m.visibleSelectableDetailEntries()
	selected := detailVisibleEntryIndex(visible, m.detailSelectedEntry)
	if !m.detailSelectedActive || selected < 0 {
		t.Fatalf("expected selected entry in visible entries, selected=%d active=%v visible=%+v", m.detailSelectedEntry, m.detailSelectedActive, visible)
	}
	center := detailVisibleEntryIndex(visible, centerVisibleSelectableDetailEntry(t, m))
	if center < 0 {
		t.Fatalf("expected center entry in visible entries, visible=%+v", visible)
	}
	return selected - center
}
