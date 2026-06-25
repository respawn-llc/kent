package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type sgrStyleState struct {
	hasForeground bool
	faint         bool
}

func TestDetailProjectionViewportKeepsLineCountAcrossScrollUpdates(t *testing.T) {
	m := NewModel(WithTheme("dark"))
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 24, Width: 100})
	m = updateModel(t, m, SetConversationMsg{Entries: []TranscriptEntry{
		{Role: "user", Text: "hello"},
		{Role: "assistant", Text: "world"},
	}})
	m = updateModel(t, m, ToggleModeMsg{})

	if len(m.detailViewProjection().DetailViewport(m.currentDetailViewportState()).Lines) == 0 {
		t.Fatal("expected detail projection viewport lines on detail entry")
	}
	startLen := len(m.detailViewProjection().DetailViewport(m.currentDetailViewportState()).Lines)

	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if got := len(m.detailViewProjection().DetailViewport(m.currentDetailViewportState()).Lines); got != startLen {
		t.Fatalf("expected detail projection viewport length to stay stable across scroll updates, got %d want %d", got, startLen)
	}
}

func TestDetailScrollStepAllocsStayBounded(t *testing.T) {
	entries := benchmarkDetailEntries(300)
	m := NewModel(WithTheme("dark"))
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 40, Width: 120})
	m = updateModel(t, m, SetConversationMsg{Entries: entries})
	m = updateModel(t, m, ToggleModeMsg{})

	allocs := testing.AllocsPerRun(20, func() {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(Model)
		_ = m.View()
	})
	if allocs > 120 {
		t.Fatalf("expected detail scroll allocations to stay bounded, got %.2f allocs/op", allocs)
	}
}

func TestDetailSelectableLookupHotPathAllocsStayBounded(t *testing.T) {
	entries := benchmarkDetailEntries(600)
	m := NewModel(WithTheme("dark"), WithCompactDetail())
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 40, Width: 120})
	m = updateModel(t, m, SetConversationMsg{Entries: entries})
	m = updateModel(t, m, ToggleModeMsg{})
	_ = m.renderDetailSnapshot()

	allocs := testing.AllocsPerRun(20, func() {
		_ = m.visibleSelectableDetailEntries()
		_ = m.centerVisibleSelectableDetailEntry()
		_ = m.renderDetailSnapshot()
	})
	if allocs > 550 {
		t.Fatalf("expected detail selectable lookup hot path allocations to stay bounded, got %.2f allocs/op", allocs)
	}
}

func TestOngoingKeyNoopAllocsStayBounded(t *testing.T) {
	entries := benchmarkDetailEntries(300)
	m := NewModel(WithTheme("dark"))
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 40, Width: 120})
	m = updateModel(t, m, SetConversationMsg{Entries: entries})

	allocs := testing.AllocsPerRun(20, func() {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(Model)
		_ = m.View()
	})
	if allocs > 100 {
		t.Fatalf("expected ongoing scroll allocations to stay bounded, got %.2f allocs/op", allocs)
	}
}

func TestDetailStreamingUpdateAllocsStayBounded(t *testing.T) {
	entries := benchmarkDetailEntries(300)
	m := NewModel(WithTheme("dark"))
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 40, Width: 120})
	m = updateModel(t, m, SetConversationMsg{Entries: entries})
	m = updateModel(t, m, ToggleModeMsg{})

	base := m
	allocs := testing.AllocsPerRun(20, func() {
		local := base
		next, _ := local.Update(StreamAssistantMsg{Delta: "x"})
		local = next.(Model)
		_ = local.View()
	})
	if allocs > 300 {
		t.Fatalf("expected detail streaming update allocations to stay bounded, got %.2f allocs/op", allocs)
	}
}

func TestOngoingStreamingUpdateAllocsStayBounded(t *testing.T) {
	entries := benchmarkDetailEntries(300)
	m := NewModel(WithTheme("dark"))
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 40, Width: 120})
	m = updateModel(t, m, SetConversationMsg{Entries: entries})

	base := m
	allocs := testing.AllocsPerRun(20, func() {
		local := base
		next, _ := local.Update(StreamAssistantMsg{Delta: "x"})
		local = next.(Model)
		_ = local.View()
	})
	if allocs > 120 {
		t.Fatalf("expected ongoing streaming update allocations to stay bounded, got %.2f allocs/op", allocs)
	}
}

func updateModel(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()

	next, _ := m.Update(msg)
	updated, ok := next.(Model)
	if !ok {
		t.Fatalf("unexpected model type %T", next)
	}
	return updated
}

func transcriptEntriesRange(start, end int) []TranscriptEntry {
	entries := make([]TranscriptEntry, 0, max(0, end-start))
	for i := start; i < end; i++ {
		entries = append(entries, TranscriptEntry{Role: "assistant", Text: fmt.Sprintf("line %d", i)})
	}
	return entries
}

func plainTranscript(view string) string {
	stripped := ansi.Strip(view)
	lines := strings.Split(stripped, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n")
}

func containsInOrder(text string, parts ...string) bool {
	offset := 0
	for _, part := range parts {
		idx := strings.Index(text[offset:], part)
		if idx < 0 {
			return false
		}
		offset += idx + len(part)
	}
	return true
}
