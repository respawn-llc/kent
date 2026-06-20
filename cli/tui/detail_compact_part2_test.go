package tui

import (
	"core/shared/transcript"
	"fmt"
	"strings"
	"testing"
)

func newCompactDetailModel(t *testing.T, previewLines int, opts ...Option) Model {
	t.Helper()
	modelOptions := append([]Option{WithCompactDetail(), WithPreviewLines(previewLines)}, opts...)
	return NewModel(modelOptions...)
}

func newSizedCompactDetailModel(t *testing.T, previewLines int) Model {
	t.Helper()
	return updateModel(t, newCompactDetailModel(t, previewLines), SetViewportSizeMsg{Lines: previewLines, Width: 80})
}

func appendAssistantLines(t *testing.T, m Model, count int, format string) Model {
	t.Helper()
	for idx := 0; idx < count; idx++ {
		m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf(format, idx)})
	}
	return m
}

func appendShellToolCall(t *testing.T, m Model, id, command string) Model {
	t.Helper()
	return updateModel(t, m, AppendTranscriptMsg{
		Role:       "tool_call",
		Text:       command,
		ToolCallID: id,
		ToolCall:   &transcript.ToolCallMeta{ToolName: "exec_command", IsShell: true, Command: command},
	})
}

func appendToolResultLines(t *testing.T, m Model, id string, count int, format string) Model {
	t.Helper()
	lines := make([]string, 0, count)
	for idx := 0; idx < count; idx++ {
		lines = append(lines, fmt.Sprintf(format, idx))
	}
	return updateModel(t, m, AppendTranscriptMsg{Role: "tool_result_ok", ToolCallID: id, Text: strings.Join(lines, "\n")})
}

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
