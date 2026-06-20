package tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestModeToggleReSnapsTailAfterViewportShrink(t *testing.T) {
	m := NewModel(WithPreviewLines(7))
	for i := 1; i <= 20; i++ {
		m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "line"})
	}

	m = updateModel(t, m, ToggleModeMsg{}) // detail
	for i := 0; i < 10; i++ {
		m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "new"})
	}
	m = updateModel(t, m, ToggleModeMsg{}) // ongoing snaps using detail viewport

	beforeResize := m.OngoingScroll()
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 4, Width: 80})
	afterResize := m.OngoingScroll()
	if afterResize <= beforeResize {
		t.Fatalf("expected viewport resize to re-snap ongoing tail, got %d from %d", afterResize, beforeResize)
	}
	if got, want := m.OngoingScroll(), m.maxOngoingScroll(); got != want {
		t.Fatalf("expected to stay at bottom after resize snap, got %d want %d", got, want)
	}
}

func TestToggleToDetailCanSkipWarmup(t *testing.T) {
	m := NewModel(WithPreviewLines(2))
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a1"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a2"})

	m = updateModel(t, m, ToggleModeMsg{SkipDetailWarmup: true})

	if got := m.Mode(); got != ModeDetail {
		t.Fatalf("mode after skip-warmup toggle = %q, want %q", got, ModeDetail)
	}
	if m.DetailMetricsResolved() {
		t.Fatal("expected skip-warmup toggle to leave detail metrics lazy")
	}
	if got := m.detailScroll; got != 0 {
		t.Fatalf("detail scroll after skip-warmup toggle = %d, want 0", got)
	}
}

func TestDetailSetConversationPreservesFocusedAbsoluteEntryAcrossBaseOffsetShift(t *testing.T) {
	m := NewModel(WithPreviewLines(8))
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 8, Width: 80})
	m = updateModel(t, m, SetConversationMsg{
		BaseOffset:   0,
		TotalEntries: 1000,
		Entries:      transcriptEntriesRange(0, 1000),
	})
	m = updateModel(t, m, SetModeMsg{Mode: ModeDetail})
	m = updateModel(t, m, FocusTranscriptEntryMsg{EntryIndex: 500, Center: true})

	beforeStart, beforeEnd, ok := m.DetailVisibleEntryRange()
	if !ok {
		t.Fatal("expected visible range before base offset shift")
	}
	if beforeStart > 500 || beforeEnd < 500 {
		t.Fatalf("expected entry 500 visible before base offset shift, got range %d..%d", beforeStart, beforeEnd)
	}

	m = updateModel(t, m, SetConversationMsg{
		BaseOffset:   200,
		TotalEntries: 1200,
		Entries:      transcriptEntriesRange(200, 1200),
	})

	afterStart, afterEnd, ok := m.DetailVisibleEntryRange()
	if !ok {
		t.Fatal("expected visible range after base offset shift")
	}
	if afterStart > 500 || afterEnd < 500 {
		t.Fatalf("expected entry 500 to remain visible after base offset shift, got range %d..%d", afterStart, afterEnd)
	}
}

func TestDetailScrollStaysStableOnIncomingMessages(t *testing.T) {
	m := NewModel(WithPreviewLines(2))
	m = updateModel(t, m, AppendTranscriptMsg{Role: "user", Text: "u1"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a1"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "user", Text: "u2"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a2"})
	m = updateModel(t, m, ToggleModeMsg{})
	m.ensureDetailScrollResolved()
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.DetailScroll() == 0 {
		t.Fatal("expected detail viewport to move before appending")
	}

	beforeScroll := m.DetailScroll()
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a3"})
	if got := m.DetailScroll(); got != beforeScroll {
		t.Fatalf("detail scroll changed while new messages arrived, got %d want %d", got, beforeScroll)
	}
}

func TestOngoingDoesNotAutoFollowWhenUserScrolledUp(t *testing.T) {
	m := NewModel(WithPreviewLines(2))
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a1"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a2"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a3"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a4"})
	if got, want := m.OngoingScroll(), m.maxOngoingScroll(); got != want {
		t.Fatalf("expected to start at bottom, got %d want %d", got, want)
	}

	m = updateModel(t, m, ScrollOngoingMsg{Delta: -1})
	pinned := m.OngoingScroll()
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a5"})
	if got := m.OngoingScroll(); got != pinned {
		t.Fatalf("scroll should stay pinned when user scrolled up, got %d want %d", got, pinned)
	}
	if m.OngoingScroll() == m.maxOngoingScroll() {
		t.Fatalf("expected to remain above bottom after new message")
	}
}

func TestMouseWheelScrollsOngoingView(t *testing.T) {
	m := NewModel(WithPreviewLines(2))
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a1"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a2"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a3"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a4"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a5"})

	start := m.OngoingScroll()
	if start == 0 {
		t.Fatalf("expected ongoing mode to start at bottom, got ongoingScroll=%d", start)
	}

	m = updateModel(t, m, tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	afterUp := m.OngoingScroll()
	if afterUp >= start {
		t.Fatalf("expected wheel up to scroll ongoing view up, got %d from %d", afterUp, start)
	}

	m = updateModel(t, m, tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if got := m.OngoingScroll(); got != start {
		t.Fatalf("expected wheel down to return ongoing scroll to start, got %d want %d", got, start)
	}
}

func TestMouseWheelScrollsDetailView(t *testing.T) {
	m := NewModel(WithPreviewLines(2))
	m = updateModel(t, m, AppendTranscriptMsg{Role: "user", Text: "u1"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a1"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "user", Text: "u2"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a2"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "user", Text: "u3"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a3"})
	m = updateModel(t, m, ToggleModeMsg{})
	ongoingStart := m.ongoingScroll
	if m.DetailMetricsResolved() {
		t.Fatal("expected detail mode to defer global scroll metric resolution until navigation")
	}
	initial := plainTranscript(m.View())

	m = updateModel(t, m, tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	if m.DetailMetricsResolved() {
		t.Fatal("expected first detail navigation to stay lazy")
	}
	afterUp := m.DetailScroll()
	if afterUp <= 0 {
		t.Fatalf("expected wheel up to advance lazy detail offset, got %d", afterUp)
	}
	if plainTranscript(m.View()) == initial {
		t.Fatal("expected detail wheel up to change the visible viewport")
	}
	if got := m.ongoingScroll; got != ongoingStart {
		t.Fatalf("expected detail wheel scroll to leave ongoing scroll untouched, got %d want %d", got, ongoingStart)
	}

	m = updateModel(t, m, tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if got := m.DetailScroll(); got != 0 {
		t.Fatalf("expected wheel down to return lazy detail offset to bottom, got %d", got)
	}
	if got := m.ongoingScroll; got != ongoingStart {
		t.Fatalf("expected detail wheel scroll to keep ongoing scroll unchanged, got %d want %d", got, ongoingStart)
	}
}

func TestPageKeysDoNotScrollOngoingView(t *testing.T) {
	m := NewModel(WithPreviewLines(2))
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a1"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a2"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a3"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a4"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a5"})

	start := m.OngoingScroll()
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if got := m.OngoingScroll(); got != start {
		t.Fatalf("expected pgup not to mutate ongoing scroll, got %d from %d", got, start)
	}

	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	if got := m.OngoingScroll(); got != start {
		t.Fatalf("expected pgdown not to mutate ongoing scroll, got %d from %d", got, start)
	}

	m = updateModel(t, m, ToggleModeMsg{})
	detailStart := m.DetailScroll()
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if got := m.DetailScroll(); got == detailStart {
		t.Fatalf("expected pgup to keep scrolling detail view, got %d", got)
	}
}

func TestFocusTranscriptEntryCentersOngoingViewport(t *testing.T) {
	m := NewModel(WithPreviewLines(6))
	for i := 0; i < 40; i++ {
		role := "assistant"
		if i%2 == 0 {
			role = "user"
		}
		m = updateModel(t, m, AppendTranscriptMsg{Role: TranscriptRoleFromWire(role), Text: fmt.Sprintf("line %d", i)})
	}

	entryIndex := 10
	start, end, ok := m.ongoingLineRangeForEntry(entryIndex)
	if !ok {
		t.Fatalf("expected line range for transcript entry %d", entryIndex)
	}
	midpoint := (start + end) / 2
	expected := clamp(midpoint-m.viewportLines/2, 0, m.maxOngoingScroll())

	m = updateModel(t, m, FocusTranscriptEntryMsg{EntryIndex: entryIndex, Center: true})
	if got := m.OngoingScroll(); got != expected {
		t.Fatalf("expected centered scroll %d for entry %d, got %d", expected, entryIndex, got)
	}
}

func TestFocusTranscriptEntryClampsNearTopAndBottom(t *testing.T) {
	m := NewModel(WithPreviewLines(6))
	for i := 0; i < 40; i++ {
		role := "assistant"
		if i%2 == 0 {
			role = "user"
		}
		m = updateModel(t, m, AppendTranscriptMsg{Role: TranscriptRoleFromWire(role), Text: fmt.Sprintf("line %d", i)})
	}

	topEntry := 0
	m = updateModel(t, m, FocusTranscriptEntryMsg{EntryIndex: topEntry, Center: true})
	if got := m.OngoingScroll(); got != 0 {
		t.Fatalf("expected top entry focus to clamp to scroll 0, got %d", got)
	}
	if start, end, ok := m.ongoingLineRangeForEntry(topEntry); !ok || end < m.OngoingScroll() || start >= m.OngoingScroll()+m.viewportLines {
		t.Fatalf("expected top entry visible after focus, range=(%d,%d) scroll=%d", start, end, m.OngoingScroll())
	}

	bottomEntry := len(m.transcriptInput.Entries) - 1
	m = updateModel(t, m, FocusTranscriptEntryMsg{EntryIndex: bottomEntry, Center: true})
	if got, want := m.OngoingScroll(), m.maxOngoingScroll(); got != want {
		t.Fatalf("expected bottom entry focus to clamp to max scroll %d, got %d", want, got)
	}
	if start, end, ok := m.ongoingLineRangeForEntry(bottomEntry); !ok || end < m.OngoingScroll() || start >= m.OngoingScroll()+m.viewportLines {
		t.Fatalf("expected bottom entry visible after focus, range=(%d,%d) scroll=%d", start, end, m.OngoingScroll())
	}
}

func TestFocusTranscriptEntryCentersInDetailMode(t *testing.T) {
	m := NewModel(WithPreviewLines(4))
	for i := 0; i < 20; i++ {
		m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf("line %d", i)})
	}
	m = updateModel(t, m, ToggleModeMsg{})
	m = updateModel(t, m, FocusTranscriptEntryMsg{EntryIndex: 0, Center: true})
	if m.detailScroll != 0 {
		t.Fatalf("expected detail focus of first entry to clamp to top, got %d", m.detailScroll)
	}

	m = updateModel(t, m, FocusTranscriptEntryMsg{EntryIndex: 10, Center: true})
	if m.detailScroll <= 0 {
		t.Fatalf("expected detail focus of middle entry to scroll into transcript, got %d", m.detailScroll)
	}
	start, end, ok := m.detailLineRangeForEntry(10)
	if !ok {
		t.Fatal("expected detail line range for focused entry")
	}
	midpoint := (start + end) / 2
	visibleMid := m.detailScroll + m.viewportLines/2
	if diff := absInt(midpoint - visibleMid); diff > 2 {
		t.Fatalf("expected focused entry near viewport center, midpoint=%d visibleMid=%d", midpoint, visibleMid)
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
