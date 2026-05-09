package tui

import (
	"builder/server/llm"
	"builder/shared/transcript"
	patchformat "builder/shared/transcript/patchformat"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func testPatchRender(lines ...patchformat.RenderedLine) *patchformat.RenderedPatch {
	return &patchformat.RenderedPatch{DetailLines: lines}
}

func TestModeToggleReturnsToLatestOngoingTail(t *testing.T) {
	m := NewModel(WithPreviewLines(2))
	m = updateModel(t, m, StreamAssistantMsg{Delta: "l1\nl2\nl3\nl4"})
	m = updateModel(t, m, ScrollOngoingMsg{Delta: -1})

	if got := m.OngoingScroll(); got != 1 {
		t.Fatalf("scroll before toggle = %d, want 1", got)
	}

	before := m.View()
	linesBefore := strings.Split(before, "\n")
	if len(linesBefore) != 2 {
		t.Fatalf("ongoing lines = %d, want 2", len(linesBefore))
	}
	if strings.TrimSpace(linesBefore[0]) != "l2" || strings.TrimSpace(linesBefore[1]) != "l3" {
		t.Fatalf("unexpected ongoing view before toggle: %q", before)
	}

	m = updateModel(t, m, ToggleModeMsg{})
	if got := m.Mode(); got != ModeDetail {
		t.Fatalf("mode after first toggle = %q, want %q", got, ModeDetail)
	}

	m = updateModel(t, m, ToggleModeMsg{})
	if got := m.Mode(); got != ModeOngoing {
		t.Fatalf("mode after second toggle = %q, want %q", got, ModeOngoing)
	}
	if got, want := m.OngoingScroll(), m.maxOngoingScroll(); got != want {
		t.Fatalf("scroll after roundtrip toggle = %d, want latest %d", got, want)
	}

	after := strings.Split(m.View(), "\n")
	if len(after) != 2 {
		t.Fatalf("ongoing lines after toggle = %d, want 2", len(after))
	}
	if strings.TrimSpace(after[0]) != "l3" || strings.TrimSpace(after[1]) != "l4" {
		t.Fatalf("unexpected ongoing tail after toggle: %q", m.View())
	}
}

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

func TestModeToggleReturnsToBottomWhenConversationGrewInDetail(t *testing.T) {
	m := NewModel(WithPreviewLines(2))
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a1"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a2"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a3"})
	m = updateModel(t, m, ScrollOngoingMsg{Delta: -1})
	before := m.OngoingScroll()
	if before >= m.maxOngoingScroll() {
		t.Fatalf("expected to start above bottom, got %d", before)
	}

	m = updateModel(t, m, ToggleModeMsg{})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a4"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a5"})
	m = updateModel(t, m, ToggleModeMsg{})

	if got, want := m.OngoingScroll(), m.maxOngoingScroll(); got != want {
		t.Fatalf("expected ongoing to snap to bottom after growth in detail: got %d want %d", got, want)
	}
	view := plainTranscript(m.View())
	if !strings.Contains(view, "a5") {
		t.Fatalf("expected newest entry visible after returning from detail, got %q", view)
	}
}

func TestToggleToDetailStartsAtBottom(t *testing.T) {
	m := NewModel(WithPreviewLines(2))
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a1"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a2"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a3"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a4"})

	m = updateModel(t, m, ToggleModeMsg{})

	if m.DetailMetricsResolved() {
		t.Fatal("expected detail entry to remain lazily bottom-anchored until the first navigation action")
	}
	view := plainTranscript(m.View())
	if !strings.Contains(view, "a4") {
		t.Fatalf("expected detail toggle to show newest content, got %q", view)
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

func TestOngoingShowsFullConversationContext(t *testing.T) {
	m := NewModel(WithCompactDetail(), WithPreviewLines(20), WithTheme("dark"))
	m = updateModel(t, m, AppendTranscriptMsg{Role: "user", Text: "first question"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "first answer"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "user", Text: "second question"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "second answer"})

	view := plainTranscript(m.View())
	if !containsInOrder(view, "❯", "first question", "❮", "first answer", "❯", "second question", "❮", "second answer") {
		t.Fatalf("expected first user message in ongoing view, got %q", view)
	}
}

func TestOngoingDoesNotPinOngoingErrorToBottomLine(t *testing.T) {
	m := NewModel(WithPreviewLines(4))
	m = updateModel(t, m, SetConversationMsg{
		Entries:      []TranscriptEntry{{Role: "assistant", Text: "line one"}},
		Ongoing:      "line two",
		OngoingError: "error: should not pin",
	})

	view := plainTranscript(m.View())
	if strings.Contains(view, "should not pin") {
		t.Fatalf("did not expect ongoing error to consume a fixed viewport line, got %q", view)
	}
	if !containsInOrder(view, "line one", "line two") {
		t.Fatalf("expected transcript content to remain visible, got %q", view)
	}
}

func TestErrorEntryVisibleInDetailAndHiddenInOngoing(t *testing.T) {
	m := NewModel(WithPreviewLines(6))
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "ready"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "error", Text: "boom trace"})

	ongoing := m.View()
	ongoingPlain := plainTranscript(ongoing)
	if strings.Contains(ongoingPlain, "boom trace") {
		t.Fatalf("expected error entry hidden in ongoing view, got %q", ongoingPlain)
	}

	m = updateModel(t, m, ToggleModeMsg{})
	detail := m.View()
	plain := plainTranscript(detail)
	if !containsInOrder(plain, "❮", "ready", "!", "boom trace") {
		t.Fatalf("expected error entry in detail transcript history, got %q", plain)
	}
	renderedError := m.palette().error.Render("boom trace")
	if !strings.Contains(detail, renderedError) {
		t.Fatalf("expected error text to use error style in detail, got %q", detail)
	}
}

func TestDetailUpdatesWhileOpenAndKeepsScrollStable(t *testing.T) {
	m := NewModel()
	m = updateModel(t, m, AppendTranscriptMsg{Role: "user", Text: "question"})
	m = updateModel(t, m, StreamAssistantMsg{Delta: "alpha"})
	m = updateModel(t, m, ToggleModeMsg{})
	m.ensureDetailScrollResolved()

	initial := plainTranscript(m.View())
	initialScroll := m.DetailScroll()
	if !containsInOrder(initial, "❮", "alpha") {
		t.Fatalf("detail view missing assistant stream: %q", initial)
	}

	m = updateModel(t, m, StreamAssistantMsg{Delta: " beta"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "tool", Text: "ran"})

	updated := plainTranscript(m.View())
	if updated == initial {
		t.Fatalf("detail view did not update while open")
	}
	if got := m.DetailScroll(); got != initialScroll {
		t.Fatalf("detail scroll changed while content updated, got %d want %d", got, initialScroll)
	}
	if !containsInOrder(updated, "❮", "alpha beta") {
		t.Fatalf("updated detail view missing full assistant stream: %q", updated)
	}
	if !containsInOrder(updated, "•", "ran") {
		t.Fatalf("updated detail view missing new transcript entry: %q", updated)
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
	m = updateModel(t, m, ScrollOngoingMsg{Delta: 1})

	beforeScroll := m.DetailScroll()
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a3"})
	if got := m.DetailScroll(); got != beforeScroll {
		t.Fatalf("detail scroll changed while new messages arrived, got %d want %d", got, beforeScroll)
	}
}

func TestClearOngoingAssistantMsgDropsPartialStream(t *testing.T) {
	m := NewModel()
	m = updateModel(t, m, StreamAssistantMsg{Delta: "partial"})
	m = updateModel(t, m, ClearOngoingAssistantMsg{})
	m = updateModel(t, m, StreamAssistantMsg{Delta: "final"})
	m = updateModel(t, m, CommitAssistantMsg{})
	m = updateModel(t, m, ToggleModeMsg{})

	snapshot := plainTranscript(m.View())
	if strings.Contains(snapshot, "partial") {
		t.Fatalf("snapshot should not contain discarded attempt delta: %q", snapshot)
	}
	if !strings.Contains(snapshot, "final") {
		t.Fatalf("snapshot missing committed final assistant output: %q", snapshot)
	}
}

func TestOngoingShowsCommittedAssistantAfterCommit(t *testing.T) {
	m := NewModel(WithPreviewLines(3))
	m = updateModel(t, m, StreamAssistantMsg{Delta: "line1\nline2"})
	m = updateModel(t, m, CommitAssistantMsg{})

	view := plainTranscript(m.View())
	if !strings.Contains(view, "line1") || !strings.Contains(view, "line2") {
		t.Fatalf("ongoing view should keep committed assistant visible, got %q", view)
	}
}

func TestOngoingDoesNotInsertDividerBetweenCommentaryAndLiveAssistantTail(t *testing.T) {
	m := NewModel(WithPreviewLines(20))
	m = updateModel(t, m, AppendTranscriptMsg{
		Role:  "assistant",
		Text:  "Decision: keep custom tool grammar {\"patch\": ...",
		Phase: llm.MessagePhaseCommentary,
	})
	m = updateModel(t, m, StreamAssistantMsg{Delta: "  } executor input so runtime/UI stays compatible."})

	view := plainTranscript(m.View())
	if strings.Contains(view, TranscriptDivider) {
		t.Fatalf("ongoing commentary continuation should not be split by divider, got %q", view)
	}
	if !containsInOrder(view, "Decision:", "executor input") {
		t.Fatalf("expected committed commentary and live tail in one assistant group, got %q", view)
	}
}

func TestOngoingAutoFollowsWhenUserIsAtBottom(t *testing.T) {
	m := NewModel(WithPreviewLines(2))
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a1"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a2"})
	if got, want := m.OngoingScroll(), m.maxOngoingScroll(); got != want {
		t.Fatalf("expected to start at bottom, got %d want %d", got, want)
	}

	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "a3"})
	if got, want := m.OngoingScroll(), m.maxOngoingScroll(); got != want {
		t.Fatalf("scroll after growth = %d, want bottom %d", got, want)
	}
	view := plainTranscript(m.View())
	if !strings.Contains(view, "a3") {
		t.Fatalf("expected latest line visible at bottom, got %q", view)
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

func TestFocusTranscriptEntryCentersInDetailModeFromLazyEntry(t *testing.T) {
	m := NewModel(WithPreviewLines(4))
	for i := 0; i < 20; i++ {
		m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf("line %d", i)})
	}
	m = updateModel(t, m, ToggleModeMsg{})
	if m.DetailMetricsResolved() {
		t.Fatal("expected detail entry to remain lazy before focus")
	}

	m = updateModel(t, m, FocusTranscriptEntryMsg{EntryIndex: 10, Center: true})
	if !m.DetailMetricsResolved() {
		t.Fatal("expected focus to resolve detail metrics on the authoritative model")
	}
	if m.detailScroll <= 0 {
		t.Fatalf("expected lazy detail focus to scroll into transcript, got %d", m.detailScroll)
	}
	if plain := plainTranscript(m.View()); !strings.Contains(plain, "line 10") {
		t.Fatalf("expected focused entry visible after lazy detail focus, got %q", plain)
	}
}

func TestFocusTranscriptEntryCentersFromLazyDetailEntry(t *testing.T) {
	m := NewModel(WithPreviewLines(4))
	for i := 0; i < 20; i++ {
		m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf("line %d", i)})
	}
	m = updateModel(t, m, ToggleModeMsg{})
	if m.DetailMetricsResolved() {
		t.Fatal("expected detail entry to start lazy before focus")
	}

	m = updateModel(t, m, FocusTranscriptEntryMsg{EntryIndex: 10, Center: true})
	if !m.DetailMetricsResolved() {
		t.Fatal("expected focus to resolve detail metrics on the authoritative model")
	}
	if m.detailScroll <= 0 {
		t.Fatalf("expected focus from lazy detail entry to scroll into transcript, got %d", m.detailScroll)
	}
	plain := plainTranscript(m.View())
	if !strings.Contains(plain, "line 10") {
		t.Fatalf("expected focused entry visible after lazy detail focus, got %q", plain)
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestDetailMatchesParallelShellResultsByCallID(t *testing.T) {
	m := NewModel()
	m = updateModel(t, m, AppendTranscriptMsg{
		Role:       "tool_call",
		Text:       "echo a",
		ToolCallID: "call_a",
		ToolCall: &transcript.ToolCallMeta{
			IsShell: true,
			Command: "echo a",
		},
	})
	m = updateModel(t, m, AppendTranscriptMsg{
		Role:       "tool_call",
		Text:       "echo b",
		ToolCallID: "call_b",
		ToolCall: &transcript.ToolCallMeta{
			IsShell: true,
			Command: "echo b",
		},
	})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "tool_result_ok", ToolCallID: "call_a", Text: "out-a"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "tool_result_ok", ToolCallID: "call_b", Text: "out-b"})
	m = updateModel(t, m, ToggleModeMsg{})

	view := plainTranscript(m.View())
	idxCallA := strings.Index(view, "echo a")
	idxOutA := strings.Index(view, "out-a")
	idxCallB := strings.Index(view, "echo b")
	idxOutB := strings.Index(view, "out-b")
	if idxCallA < 0 || idxOutA < 0 || idxCallB < 0 || idxOutB < 0 {
		t.Fatalf("expected both calls and outputs in view, got %q", view)
	}
	if !(idxCallA < idxOutA && idxOutA < idxCallB && idxCallB < idxOutB) {
		t.Fatalf("expected each output to stay with matching call, got %q", view)
	}
	if strings.Contains(view, "• out-a") || strings.Contains(view, "• out-b") {
		t.Fatalf("expected no standalone tool result blocks for matched call IDs, got %q", view)
	}
}

func TestDetailDoesNotMatchAdjacentResultWhenCallIDMissing(t *testing.T) {
	m := NewModel()
	m = updateModel(t, m, AppendTranscriptMsg{
		Role: "tool_call",
		Text: "echo missing-id",
		ToolCall: &transcript.ToolCallMeta{
			IsShell: true,
			Command: "echo missing-id",
		},
	})
	m = updateModel(t, m, AppendTranscriptMsg{
		Role:       "tool_result_ok",
		ToolCallID: "call_other",
		Text:       "out-other",
	})
	m = updateModel(t, m, ToggleModeMsg{})

	view := plainTranscript(m.View())
	if !containsInOrder(view, "$", "echo missing-id", "•", "out-other") {
		t.Fatalf("expected unmatched result to remain standalone, got %q", view)
	}
}

func TestDetailAskQuestionRendersQuestionSuggestionsAndAnswer(t *testing.T) {
	m := NewModel(WithPreviewLines(20))
	m = updateModel(t, m, AppendTranscriptMsg{
		Role: "tool_call",
		Text: "question: Choose scope?\nsuggestions: - flat scan\n  - Recursive scan",
		ToolCall: &transcript.ToolCallMeta{
			ToolName:               "ask_question",
			Question:               "Choose scope?",
			Suggestions:            []string{"flat scan", "Recursive scan"},
			RecommendedOptionIndex: 1,
		},
	})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "tool_result_ok", Text: "Use flat scan."})
	m = updateModel(t, m, ToggleModeMsg{})

	plain := plainTranscript(m.View())
	if strings.Contains(plain, "question:") || strings.Contains(plain, "suggestions:") {
		t.Fatalf("expected ask_question labels removed from detail view, got %q", plain)
	}
	if !containsInOrder(plain, "?", "Choose scope?", "- flat scan", "- Recursive scan", "Use flat scan.") {
		t.Fatalf("expected question, suggestions and answer in detail order, got %q", plain)
	}

	colored := m.View()
	if !strings.Contains(colored, m.palette().model.Render("- flat scan")) {
		t.Fatalf("expected recommended suggestion to be green in detail view, got %q", colored)
	}
	if !strings.Contains(colored, m.palette().preview.Faint(true).Render("- Recursive scan")) {
		t.Fatalf("expected non-recommended suggestions to stay muted in detail view, got %q", colored)
	}
	if !strings.Contains(colored, m.palette().user.Render("Use flat scan.")) {
		t.Fatalf("expected answer to use user color in detail view, got %q", colored)
	}
}

func TestDetailAskQuestionRendersLargeMarkdownQuestionSnapshot(t *testing.T) {
	question := strings.Join([]string{
		"Please review **this plan** before I continue:",
		"",
		"```kotlin",
		"fun main() {",
		"    println(\"hi\")",
		"}",
		"```",
		"",
		"- Keep the four leading spaces in the code block.",
		"- Do not collapse blank lines.",
	}, "\n")
	m := NewModel(WithPreviewLines(40), WithTheme("dark"))
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 40, Width: 80})
	m = updateModel(t, m, AppendTranscriptMsg{
		Role: "tool_call",
		Text: question,
		ToolCall: &transcript.ToolCallMeta{
			ToolName: "ask_question",
			Question: question,
		},
	})
	m = updateModel(t, m, ToggleModeMsg{})

	plain := plainTranscript(m.View())
	if strings.Contains(plain, "**this plan**") || strings.Contains(plain, "```") {
		t.Fatalf("expected rendered markdown markers, got %q", plain)
	}
	if !containsInOrder(plain,
		"?",
		"Please review this plan before I continue:",
		"fun main() {",
		"    println(\"hi\")",
		"}",
		"Keep the four leading spaces in the code block.",
		"Do not collapse blank lines.",
	) {
		t.Fatalf("ask_question markdown snapshot missing expected content, got %q", plain)
	}
}

func TestOngoingAskQuestionRendersSelectedOptionText(t *testing.T) {
	m := NewModel(WithPreviewLines(20))
	m = updateModel(t, m, AppendTranscriptMsg{
		Role:       "tool_call",
		Text:       "Choose scope?",
		ToolCallID: "call_ask",
		ToolCall: &transcript.ToolCallMeta{
			ToolName:    "ask_question",
			Question:    "Choose scope?",
			Suggestions: []string{"flat scan", "Recursive scan"},
		},
	})
	m = updateModel(t, m, AppendTranscriptMsg{
		Role:        "tool_result_ok",
		ToolCallID:  "call_ask",
		Text:        "User chose option #2. They also said: include tests",
		OngoingText: "Recursive scan\nUser also said:\ninclude tests",
	})

	plain := plainTranscript(m.View())
	if !containsInOrder(plain, "?", "Choose scope?", "Recursive scan", "User also said:", "include tests") {
		t.Fatalf("expected ongoing answer to show selected option text and commentary, got %q", plain)
	}
	if !containsInOrder(plain, "? Choose scope?", "│ User also said:", "└ include tests") {
		t.Fatalf("expected ongoing ask_question response to render tree guides, got %q", plain)
	}
	if strings.Contains(plain, "option #2") || strings.Contains(plain, "flat scan") {
		t.Fatalf("expected ongoing answer to omit numeric summary and unchosen suggestions, got %q", plain)
	}
}

func TestOngoingMultilineToolBlocksRenderTreeGuides(t *testing.T) {
	m := NewModel(WithPreviewLines(20))
	m = updateModel(t, m, AppendTranscriptMsg{
		Role:       "tool_call",
		Text:       "Patch",
		ToolCallID: "call_patch",
		ToolCall: &transcript.ToolCallMeta{
			ToolName:    "patch",
			Command:     "./docs/a.md +2\n./docs/b.md +5\n./docs/c.md +8",
			CompactText: "./docs/a.md +2\n./docs/b.md +5\n./docs/c.md +8",
		},
	})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "tool_result_ok", ToolCallID: "call_patch"})

	plain := plainTranscript(m.View())
	if !containsInOrder(plain, "⇄ ./docs/a.md +2", "│ ./docs/b.md +5", "└ ./docs/c.md +8") {
		t.Fatalf("expected multiline ongoing tool block to render tree guides, got %q", plain)
	}
}

func TestOngoingEditResultUsesPatchSummaryHeadline(t *testing.T) {
	m := NewModel(WithPreviewLines(20))
	m = updateModel(t, m, AppendTranscriptMsg{
		Role:       "tool_call",
		Text:       "edit server/runtime/compaction.go",
		ToolCallID: "call_edit",
		ToolCall: &transcript.ToolCallMeta{
			ToolName:    "edit",
			Command:     "server/runtime/compaction.go",
			CompactText: "server/runtime/compaction.go",
		},
	})
	m = updateModel(t, m, AppendTranscriptMsg{
		Role:       "tool_result_ok",
		ToolCallID: "call_edit",
		ToolCall: &transcript.ToolCallMeta{
			ToolName:     "edit",
			Command:      "server/runtime/compaction.go +2 -1",
			CompactText:  "server/runtime/compaction.go +2 -1",
			PatchSummary: "server/runtime/compaction.go +2 -1",
		},
	})

	plain := plainTranscript(m.View())
	if !strings.Contains(plain, "⇄ server/runtime/compaction.go +2 -1") {
		t.Fatalf("expected edit result to use patch summary headline, got %q", plain)
	}
	if strings.Contains(plain, "⇄ edit server/runtime/compaction.go") {
		t.Fatalf("expected edit headline to omit tool name, got %q", plain)
	}
}

func TestDetailEditResultUsesPatchSummaryHeadline(t *testing.T) {
	m := NewModel(WithPreviewLines(20))
	m = updateModel(t, m, AppendTranscriptMsg{
		Role:       "tool_call",
		Text:       "edit server/runtime/compaction.go",
		ToolCallID: "call_edit",
		ToolCall: &transcript.ToolCallMeta{
			ToolName:    "edit",
			Command:     "server/runtime/compaction.go",
			CompactText: "server/runtime/compaction.go",
		},
	})
	m = updateModel(t, m, AppendTranscriptMsg{
		Role:       "tool_result_ok",
		ToolCallID: "call_edit",
		ToolCall: &transcript.ToolCallMeta{
			ToolName:     "edit",
			Command:      "server/runtime/compaction.go +2 -1",
			CompactText:  "server/runtime/compaction.go +2 -1",
			PatchSummary: "server/runtime/compaction.go +2 -1",
		},
	})
	m = updateModel(t, m, ToggleModeMsg{})

	plain := plainTranscript(m.View())
	if !strings.Contains(plain, "⇄ server/runtime/compaction.go +2 -1") {
		t.Fatalf("expected detail edit result to use patch summary headline, got %q", plain)
	}
	if strings.Contains(plain, "⇄ edit server/runtime/compaction.go") {
		t.Fatalf("expected detail edit headline to omit tool name, got %q", plain)
	}
}

func TestOngoingWrappedToolBlocksRenderTreeGuides(t *testing.T) {
	m := NewModel(WithPreviewLines(20))
	m = updateModel(t, m, SetViewportSizeMsg{Lines: 20, Width: 34})
	m = updateModel(t, m, AppendTranscriptMsg{
		Role:       "tool_call",
		Text:       "inspect a very long generated transcript rendering path",
		ToolCallID: "call_tool",
		ToolCall: &transcript.ToolCallMeta{
			ToolName:    "custom_tool",
			Command:     "inspect a very long generated transcript rendering path",
			CompactText: "inspect a very long generated transcript rendering path",
		},
	})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "tool_result_ok", ToolCallID: "call_tool"})

	plain := plainTranscript(m.View())
	if !containsInOrder(plain, "• inspect a very long generated", "└ transcript rendering path") {
		t.Fatalf("expected wrapped ongoing tool block to render tree guide, got %q", plain)
	}
}

func TestPendingOngoingMultilineToolBlockRendersTreeGuidesWithSpinner(t *testing.T) {
	entries := []TranscriptEntry{{
		Role:       "tool_call",
		Text:       "Patch",
		ToolCallID: "call_patch",
		ToolCall: &transcript.ToolCallMeta{
			ToolName:    "patch",
			Command:     "./docs/a.md +2\n./docs/b.md +5",
			CompactText: "./docs/a.md +2\n./docs/b.md +5",
		},
	}}

	plain := plainTranscript(RenderPendingOngoingSnapshot(entries, "dark", 80, "*"))
	if !containsInOrder(plain, "* ./docs/a.md +2", "└ ./docs/b.md +5") {
		t.Fatalf("expected pending multiline ongoing tool block to render tree guides with spinner, got %q", plain)
	}
}

func TestDetailAskQuestionKeepsToolResultTextWhenOngoingTextDiffers(t *testing.T) {
	m := NewModel(WithPreviewLines(20))
	m = updateModel(t, m, AppendTranscriptMsg{
		Role:       "tool_call",
		Text:       "Choose scope?",
		ToolCallID: "call_ask",
		ToolCall: &transcript.ToolCallMeta{
			ToolName:    "ask_question",
			Question:    "Choose scope?",
			Suggestions: []string{"flat scan", "Recursive scan"},
		},
	})
	m = updateModel(t, m, AppendTranscriptMsg{
		Role:        "tool_result_ok",
		ToolCallID:  "call_ask",
		Text:        "User chose option #2. They also said: include tests",
		OngoingText: "Recursive scan\nUser also said:\ninclude tests",
	})
	m = updateModel(t, m, ToggleModeMsg{})

	plain := plainTranscript(m.View())
	if !strings.Contains(plain, "User chose option #2. They also said: include tests") {
		t.Fatalf("expected detail answer to keep raw tool result text, got %q", plain)
	}
	if strings.Contains(plain, "User also said:") {
		t.Fatalf("expected detail answer to ignore ongoing-only text, got %q", plain)
	}
}
