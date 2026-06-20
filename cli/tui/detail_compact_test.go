package tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestCompactDetailToggleStartsWithBottomVisibleEntrySelected(t *testing.T) {
	m := newSizedCompactDetailModel(t, 4)
	m = updateModel(t, m, AppendTranscriptMsg{Role: "user", Text: "first"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "second"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "user", Text: "third"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "fourth"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "user", Text: "fifth"})

	m = updateModel(t, m, ToggleModeMsg{})

	visible := m.visibleSelectableDetailEntries()
	if len(visible) == 0 {
		t.Fatal("expected visible selectable detail entries")
	}
	want := visible[len(visible)-1]
	if !m.detailSelectedActive || m.detailSelectedEntry != want {
		t.Fatalf("expected bottom visible entry selected on detail open, got active=%v entry=%d want=%d visible=%+v", m.detailSelectedActive, m.detailSelectedEntry, want, visible)
	}
	if !m.detailBottomAnchor || m.DetailScroll() != 0 {
		t.Fatalf("expected detail to remain anchored at bottom, anchored=%v scroll=%d", m.detailBottomAnchor, m.DetailScroll())
	}
}

func TestCompactDetailToggleStartsWithMultilineTailBlockSelected(t *testing.T) {
	m := newSizedCompactDetailModel(t, 5)
	m = updateModel(t, m, AppendTranscriptMsg{Role: "user", Text: "first"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "tail 1\ntail 2\ntail 3\ntail 4"})

	m = updateModel(t, m, ToggleModeMsg{})

	visible := m.visibleSelectableDetailEntries()
	if len(visible) == 0 {
		t.Fatal("expected visible selectable detail entries")
	}
	want := visible[len(visible)-1]
	owners := m.detailViewProjection().DetailViewport(m.currentDetailViewportState()).Owners
	if !m.detailSelectedActive || m.detailSelectedEntry != want {
		t.Fatalf("expected multiline tail block selected on detail open, got active=%v entry=%d want=%d visible=%+v owners=%+v", m.detailSelectedActive, m.detailSelectedEntry, want, visible, owners)
	}
	selectedLines := 0
	for _, owner := range owners {
		if owner == want {
			selectedLines++
		}
	}
	if selectedLines < 2 {
		t.Fatalf("expected bottom-selected tail block to own multiple visible lines, got %d owners=%+v", selectedLines, owners)
	}
}

func TestCompactDetailViewportShrinkKeepsBottomSelectionVisible(t *testing.T) {
	m := newSizedCompactDetailModel(t, 8)
	m = appendAssistantLines(t, m, 24, "line %02d")
	m = updateModel(t, m, ToggleModeMsg{})

	m = updateModel(t, m, SetViewportSizeMsg{Lines: 6, Width: 80})

	visible := m.visibleSelectableDetailEntries()
	if len(visible) == 0 {
		t.Fatal("expected visible selectable detail entries")
	}
	want := visible[len(visible)-1]
	if !m.detailSelectedActive || m.detailSelectedEntry != want {
		t.Fatalf("expected bottom visible entry selected after viewport shrink, got active=%v entry=%d want=%d visible=%+v owners=%+v", m.detailSelectedActive, m.detailSelectedEntry, want, visible, m.detailViewProjection().DetailViewport(m.currentDetailViewportState()).Owners)
	}
}

func TestCompactDetailArrowScrollsExpandedItemByLineAndTracksCenterSelection(t *testing.T) {
	m := newSizedCompactDetailModel(t, 6)
	m = appendShellToolCall(t, m, "call_1", "long-command")
	m = appendToolResultLines(t, m, "call_1", 30, "output line %02d")
	m = updateModel(t, m, ToggleModeMsg{})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	beforeSelected := m.detailSelectedEntry

	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if got, want := m.DetailScroll(), 1; got != want {
		t.Fatalf("expected arrow scroll to move by one rendered line, got %d want %d", got, want)
	}
	topVisible := leadingViewportSelectableDetailEntry(t, m)
	centerVisible := centerVisibleSelectableDetailEntry(t, m)
	if !m.detailSelectedActive || m.detailSelectedEntry != centerVisible {
		t.Fatalf("expected arrow scroll to select center visible entry %d, got active=%v entry=%d", centerVisible, m.detailSelectedActive, m.detailSelectedEntry)
	}
	if m.detailSelectedEntry != beforeSelected {
		t.Fatalf("expected one-line scroll inside expanded command to keep same selected item, got %d want %d", m.detailSelectedEntry, beforeSelected)
	}
	if topVisible != beforeSelected {
		t.Fatalf("expected expanded command to remain top visible, got %d want %d", topVisible, beforeSelected)
	}
}

func TestCompactDetailSelectedSpacerRowsAreVisualOnlyWithTallExpandedEntry(t *testing.T) {
	m := updateModel(t, newCompactDetailModel(t, 6, WithTheme("dark")), SetViewportSizeMsg{Lines: 6, Width: 80})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "intro"})
	m = appendShellToolCall(t, m, "call_1", "long-command")
	m = appendToolResultLines(t, m, "call_1", 10, "output line %02d")
	m = appendShellToolCall(t, m, "call_2", "target-command")
	m = appendShellToolCall(t, m, "call_3", "after-target-command")
	m = appendShellToolCall(t, m, "call_4", "after-target-command-2")
	m = appendShellToolCall(t, m, "call_5", "after-target-command-3")
	m = updateModel(t, m, ToggleModeMsg{})
	m.detailSelectedEntry = 1
	m.detailSelectedActive = true
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	targetEntry := 3
	targetStart, _, ok := m.detailLineRangeForEntry(targetEntry)
	if !ok {
		t.Fatal("expected target row detail range")
	}
	center := m.viewportLines / 2
	m.detailBottomAnchor = false
	m.detailScroll = targetStart - center
	m.refreshDetailViewport()
	m.detailSelectedEntry = targetEntry
	m.detailSelectedActive = true

	beforeScroll := m.DetailScroll()
	beforeFirst, beforeLast, beforeRangeOK := m.DetailVisibleEntryRange()
	owners := m.detailViewProjection().DetailViewport(m.currentDetailViewportState()).Owners
	if center <= 0 || center >= len(owners)-1 {
		t.Fatalf("center line %d outside spacer assertion range, owners=%d", center, len(owners))
	}
	if owners[center] != targetEntry {
		t.Fatalf("expected target entry %d owned by center viewport line, owners=%+v", targetEntry, owners)
	}
	_ = m.View()
	afterFirst, afterLast, afterRangeOK := m.DetailVisibleEntryRange()
	if got := m.DetailScroll(); got != beforeScroll {
		t.Fatalf("expected visual spacers not to mutate detail scroll, got %d want %d", got, beforeScroll)
	}
	if beforeFirst != afterFirst || beforeLast != afterLast || beforeRangeOK != afterRangeOK {
		t.Fatalf("expected visual spacers not to mutate visible range, before=(%d,%d,%v) after=(%d,%d,%v)", beforeFirst, beforeLast, beforeRangeOK, afterFirst, afterLast, afterRangeOK)
	}
}

func TestCompactDetailLineScrollSelectionTracksCenterItem(t *testing.T) {
	m := newSizedCompactDetailModel(t, 6)
	m = appendShellToolCall(t, m, "call_1", "first-command")
	m = appendToolResultLines(t, m, "call_1", 20, "first output line %02d")
	m = appendShellToolCall(t, m, "call_2", "second-command")
	m = updateModel(t, m, AppendTranscriptMsg{Role: "tool_result_ok", ToolCallID: "call_2", Text: "second output"})
	m = appendShellToolCall(t, m, "call_3", "third-command")
	m = updateModel(t, m, AppendTranscriptMsg{Role: "tool_result_ok", ToolCallID: "call_3", Text: "third output"})
	m = appendAssistantLines(t, m, 10, "tail entry %02d")
	m = updateModel(t, m, ToggleModeMsg{})
	m.detailSelectedEntry = 0
	m.detailSelectedActive = true
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	for step := 1; step <= 10; step++ {
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
		if got := m.DetailScroll(); got != step {
			t.Fatalf("step %d: expected one-line scroll, got %d", step, got)
		}
		if topVisible := leadingViewportSelectableDetailEntry(t, m); topVisible != 0 {
			t.Fatalf("step %d: expected top expanded item to remain top visible, got %d", step, topVisible)
		}
		centerVisible := centerVisibleSelectableDetailEntry(t, m)
		if !m.detailSelectedActive || m.detailSelectedEntry != centerVisible {
			t.Fatalf("step %d: expected selection to track center visible item %d, active=%v entry=%d", step, centerVisible, m.detailSelectedActive, m.detailSelectedEntry)
		}
	}

	for guard := 0; guard < 40 && leadingViewportSelectableDetailEntry(t, m) == 0; guard++ {
		before := m.DetailScroll()
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
		if got := m.DetailScroll(); got != before+1 {
			t.Fatalf("expected crossing scroll to move by one rendered line, got %d want %d", got, before+1)
		}
	}
	if topVisible := leadingViewportSelectableDetailEntry(t, m); topVisible != 2 {
		t.Fatalf("expected second item top visible after crossing expanded item, got %d", topVisible)
	}
	centerVisible := centerVisibleSelectableDetailEntry(t, m)
	if !m.detailSelectedActive || m.detailSelectedEntry != centerVisible {
		t.Fatalf("expected selection to track center visible item %d after crossing expanded item, active=%v entry=%d", centerVisible, m.detailSelectedActive, m.detailSelectedEntry)
	}
}

func TestCompactDetailLineScrollFocusesCenterVisibleSelection(t *testing.T) {
	m := newSizedCompactDetailModel(t, 6)
	m = appendAssistantLines(t, m, 10, "entry %02d")
	m = updateModel(t, m, ToggleModeMsg{})
	m.ensureDetailScrollResolved()
	m.detailScroll = max(1, m.maxDetailScroll()/2)
	m.refreshDetailViewport()
	visible := m.visibleSelectableDetailEntries()
	if len(visible) < 2 {
		t.Fatalf("expected at least two visible detail entries, got %+v", visible)
	}
	selected := visible[1]
	m.detailSelectedEntry = selected
	m.detailSelectedActive = true
	beforeScroll := m.DetailScroll()

	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if got := m.DetailScroll(); got != beforeScroll-1 {
		t.Fatalf("expected up to scroll by one line while selection remains visible, got %d want %d", got, beforeScroll-1)
	}
	centerVisible := centerVisibleSelectableDetailEntry(t, m)
	if !m.detailSelectedActive || m.detailSelectedEntry != centerVisible {
		t.Fatalf("expected line scroll to focus center visible selection %d, got active=%v entry=%d", centerVisible, m.detailSelectedActive, m.detailSelectedEntry)
	}
}

func TestCompactDetailReverseInputWalksOffCenterSelectionBeforeCameraScroll(t *testing.T) {
	m := newSizedCompactDetailModel(t, 8)
	m = appendAssistantLines(t, m, 16, "entry %02d")
	m = updateModel(t, m, ToggleModeMsg{})
	m.ensureDetailScrollResolved()
	m.detailScroll = 4
	m.refreshDetailViewport()
	visible := m.visibleSelectableDetailEntries()
	if len(visible) < 6 {
		t.Fatalf("expected visible entries, got %+v", visible)
	}
	m.detailSelectedEntry = visible[len(visible)-2]
	m.detailSelectedActive = true
	beforeScroll := m.DetailScroll()
	beforeDistance := selectedDetailDistanceFromCenter(t, m)
	if beforeDistance <= 1 {
		t.Fatalf("expected selected entry below center, distance=%d visible=%+v", beforeDistance, visible)
	}

	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyUp})

	if got := m.DetailScroll(); got != beforeScroll {
		t.Fatalf("expected camera to hold while off-center selection walks toward center, got %d want %d", got, beforeScroll)
	}
	if got := selectedDetailDistanceFromCenter(t, m); got != beforeDistance-1 {
		t.Fatalf("expected selected row to move one visual line toward center, got distance=%d want=%d", got, beforeDistance-1)
	}
}

func TestCompactDetailSelectionMovesWithinViewportAtTranscriptEnd(t *testing.T) {
	assertCompactDetailSelectionMovesWithinViewportAtTranscriptEnd(t, tea.KeyMsg{Type: tea.KeyDown})
}

func TestCompactDetailWheelSelectionMovesWithinViewportAtTranscriptEnd(t *testing.T) {
	assertCompactDetailSelectionMovesWithinViewportAtTranscriptEnd(t, tea.MouseMsg{Button: tea.MouseButtonWheelDown})
}

func TestCompactDetailKeyReverseFromEndWalksTowardCenterBeforeLineScroll(t *testing.T) {
	assertCompactDetailReverseFromEndWalksTowardCenterBeforeLineScroll(t, tea.KeyMsg{Type: tea.KeyUp})
}

func TestCompactDetailWheelReverseFromEndWalksTowardCenterBeforeLineScroll(t *testing.T) {
	assertCompactDetailReverseFromEndWalksTowardCenterBeforeLineScroll(t, tea.MouseMsg{Button: tea.MouseButtonWheelUp})
}

func assertCompactDetailReverseFromEndWalksTowardCenterBeforeLineScroll(t *testing.T, reverse tea.Msg) {
	t.Helper()
	m := newSizedCompactDetailModel(t, 8)
	m = appendAssistantLines(t, m, 14, "entry %02d")
	m = updateModel(t, m, ToggleModeMsg{})
	m.ensureDetailScrollResolved()
	for guard := 0; guard < 40 && m.DetailScroll() < m.maxDetailScroll(); guard++ {
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	for idx := 0; idx < 3; idx++ {
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	beforeScroll := m.DetailScroll()
	beforeDistance := selectedDetailDistanceFromCenter(t, m)
	if beforeDistance <= 1 {
		t.Fatalf("expected setup to place selection below center by more than one entry, distance=%d", beforeDistance)
	}

	m = updateModel(t, m, reverse)

	if got := m.DetailScroll(); got != beforeScroll {
		t.Fatalf("expected reverse input from bottom edge to hold camera before scrolling, got %d want %d", got, beforeScroll)
	}
	if got := selectedDetailDistanceFromCenter(t, m); got != beforeDistance-1 {
		t.Fatalf("expected reverse input to move selection one visual row toward center, got distance=%d want=%d", got, beforeDistance-1)
	}

	for guard := 0; guard < 20 && selectedDetailDistanceFromCenter(t, m) > 0; guard++ {
		if before := m.DetailScroll(); before != beforeScroll {
			t.Fatalf("expected camera pinned until selection reaches center, got %d want %d", before, beforeScroll)
		}
		m = updateModel(t, m, reverse)
	}
	if got := selectedDetailDistanceFromCenter(t, m); got != 0 {
		t.Fatalf("expected repeated reverse input to reach center, got distance=%d", got)
	}
	m = updateModel(t, m, reverse)
	if got := m.DetailScroll(); got != beforeScroll-1 {
		t.Fatalf("expected reverse input after center to resume line scroll, got %d want %d", got, beforeScroll-1)
	}
}

func assertCompactDetailSelectionMovesWithinViewportAtTranscriptEnd(t *testing.T, scroll tea.Msg) {
	t.Helper()
	m := newSizedCompactDetailModel(t, 6)
	m = appendAssistantLines(t, m, 8, "entry %02d")
	m = updateModel(t, m, ToggleModeMsg{})
	m.ensureDetailScrollResolved()
	for guard := 0; guard < 20 && m.DetailScroll() < m.maxDetailScroll(); guard++ {
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	if got, want := m.DetailScroll(), m.maxDetailScroll(); got != want {
		t.Fatalf("expected setup to reach bottom scroll, got %d want %d", got, want)
	}
	topVisible := leadingViewportSelectableDetailEntry(t, m)
	m.detailSelectedEntry = topVisible
	m.detailSelectedActive = true
	visible := m.visibleSelectableDetailEntries()
	topVisibleIndex := detailVisibleEntryIndex(visible, topVisible)
	if topVisibleIndex < 0 || topVisibleIndex+1 >= len(visible) {
		t.Fatalf("expected selectable entry below top visible entry, visible=%+v top=%d", visible, topVisible)
	}

	beforeScroll := m.DetailScroll()
	m = updateModel(t, m, scroll)
	if got := m.DetailScroll(); got != beforeScroll {
		t.Fatalf("expected down at transcript bottom to keep line scroll pinned, got %d want %d", got, beforeScroll)
	}
	if want := visible[topVisibleIndex+1]; !m.detailSelectedActive || m.detailSelectedEntry != want {
		t.Fatalf("expected down at transcript bottom to move selection one visible entry to %d, got active=%v entry=%d visible=%+v", want, m.detailSelectedActive, m.detailSelectedEntry, visible)
	}
}

func TestCompactDetailSelectionMovesWithinViewportAtTranscriptStart(t *testing.T) {
	assertCompactDetailSelectionMovesWithinViewportAtTranscriptStart(t, tea.KeyMsg{Type: tea.KeyUp})
}

func TestCompactDetailWheelSelectionMovesWithinViewportAtTranscriptStart(t *testing.T) {
	assertCompactDetailSelectionMovesWithinViewportAtTranscriptStart(t, tea.MouseMsg{Button: tea.MouseButtonWheelUp})
}

func TestCompactDetailKeyReverseFromStartWalksTowardCenterBeforeLineScroll(t *testing.T) {
	assertCompactDetailReverseFromStartWalksTowardCenterBeforeLineScroll(t, tea.KeyMsg{Type: tea.KeyDown})
}

func TestCompactDetailWheelReverseFromStartWalksTowardCenterBeforeLineScroll(t *testing.T) {
	assertCompactDetailReverseFromStartWalksTowardCenterBeforeLineScroll(t, tea.MouseMsg{Button: tea.MouseButtonWheelDown})
}

func assertCompactDetailReverseFromStartWalksTowardCenterBeforeLineScroll(t *testing.T, reverse tea.Msg) {
	t.Helper()
	m := newSizedCompactDetailModel(t, 8)
	m = appendAssistantLines(t, m, 14, "entry %02d")
	m = updateModel(t, m, ToggleModeMsg{})
	m.ensureDetailScrollResolved()
	m.detailScroll = 0
	m.refreshDetailViewport()
	visible := m.visibleSelectableDetailEntries()
	if len(visible) < 2 {
		t.Fatalf("expected visible entries, got %+v", visible)
	}
	m.detailSelectedEntry = centerVisibleSelectableDetailEntry(t, m)
	m.detailSelectedActive = true
	for idx := 0; idx < 3; idx++ {
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyUp})
	}
	beforeScroll := m.DetailScroll()
	beforeDistance := selectedDetailDistanceFromCenter(t, m)
	if beforeDistance >= -1 {
		t.Fatalf("expected setup to place selection above center by more than one entry, distance=%d", beforeDistance)
	}

	m = updateModel(t, m, reverse)

	if got := m.DetailScroll(); got != beforeScroll {
		t.Fatalf("expected reverse input from top edge to hold camera before scrolling, got %d want %d", got, beforeScroll)
	}
	if got := selectedDetailDistanceFromCenter(t, m); got != beforeDistance+1 {
		t.Fatalf("expected reverse input to move selection one visual row toward center, got distance=%d want=%d", got, beforeDistance+1)
	}

	for guard := 0; guard < 20 && selectedDetailDistanceFromCenter(t, m) < 0; guard++ {
		if before := m.DetailScroll(); before != beforeScroll {
			t.Fatalf("expected camera pinned until selection reaches center, got %d want %d", before, beforeScroll)
		}
		m = updateModel(t, m, reverse)
	}
	if got := selectedDetailDistanceFromCenter(t, m); got != 0 {
		t.Fatalf("expected repeated reverse input to reach center, got distance=%d", got)
	}
	m = updateModel(t, m, reverse)
	if got := m.DetailScroll(); got != beforeScroll+1 {
		t.Fatalf("expected reverse input after center to resume line scroll, got %d want %d", got, beforeScroll+1)
	}
}

func assertCompactDetailSelectionMovesWithinViewportAtTranscriptStart(t *testing.T, scroll tea.Msg) {
	t.Helper()
	m := newSizedCompactDetailModel(t, 6)
	m = appendAssistantLines(t, m, 8, "entry %02d")
	m = updateModel(t, m, ToggleModeMsg{})
	m.ensureDetailScrollResolved()
	m.detailScroll = 0
	m.refreshDetailViewport()
	for guard := 0; guard < 20 && m.DetailScroll() > 0; guard++ {
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyUp})
	}
	if got := m.DetailScroll(); got != 0 {
		t.Fatalf("expected setup to reach top scroll, got %d", got)
	}
	visible := m.visibleSelectableDetailEntries()
	if len(visible) < 2 {
		t.Fatalf("expected at least two visible selectable entries at top, got %+v", visible)
	}
	m.detailSelectedEntry = visible[len(visible)-1]
	m.detailSelectedActive = true

	m = updateModel(t, m, scroll)
	if got := m.DetailScroll(); got != 0 {
		t.Fatalf("expected up at transcript top to keep line scroll pinned, got %d", got)
	}
	if want := visible[len(visible)-2]; !m.detailSelectedActive || m.detailSelectedEntry != want {
		t.Fatalf("expected up at transcript top to move selection one visible entry to %d, got active=%v entry=%d visible=%+v", want, m.detailSelectedActive, m.detailSelectedEntry, visible)
	}
}

func TestCompactDetailReconcilesSelectionAndExpansionAfterRefresh(t *testing.T) {
	m := newCompactDetailModel(t, 12)
	m = updateModel(t, m, SetConversationMsg{BaseOffset: 10, Entries: []TranscriptEntry{
		{Role: "user", Text: "older"},
		{Role: "assistant", Text: "newer\nhidden line 1\nhidden line 2\nhidden line 3"},
	}})
	m = updateModel(t, m, ToggleModeMsg{})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if _, ok := m.detailExpandedEntries[11]; !ok {
		t.Fatalf("expected entry 11 expanded, got %+v", m.detailExpandedEntries)
	}

	m = updateModel(t, m, SetConversationMsg{BaseOffset: 20, Entries: []TranscriptEntry{{Role: "assistant", Text: "replacement"}}})
	if !m.detailSelectedActive || m.detailSelectedEntry != 20 {
		t.Fatalf("expected detail selection re-anchored to replacement, got active=%v entry=%d", m.detailSelectedActive, m.detailSelectedEntry)
	}
	if len(m.detailExpandedEntries) != 0 {
		t.Fatalf("expected stale expanded entries cleared, got %+v", m.detailExpandedEntries)
	}
}

func TestCompactDetailClearsExpandedEntriesWhenReplacementReusesIndexes(t *testing.T) {
	m := newCompactDetailModel(t, 12)
	m = updateModel(t, m, SetConversationMsg{BaseOffset: 0, Entries: []TranscriptEntry{
		{Role: "assistant", Text: "old intro"},
		{Role: "assistant", Text: "old expanded\nold hidden"},
	}})
	m = updateModel(t, m, ToggleModeMsg{})
	m.detailExpandedEntries = make(map[int]struct{})
	m.detailExpandedEntries[1] = struct{}{}

	m = updateModel(t, m, SetConversationMsg{BaseOffset: 0, Entries: []TranscriptEntry{
		{Role: "assistant", Text: "new intro"},
		{Role: "assistant", Text: "new unrelated\nnew hidden"},
	}})

	if len(m.detailExpandedEntries) != 0 {
		t.Fatalf("expected replacement at same indexes to clear expanded entries, got %+v", m.detailExpandedEntries)
	}
}

func TestCompactDetailScrollFocusesCenterVisibleEntryForExpansion(t *testing.T) {
	tests := []struct {
		name   string
		setup  []tea.Msg
		scroll tea.Msg
	}{
		{
			name:   "mouse wheel up",
			scroll: tea.MouseMsg{Button: tea.MouseButtonWheelUp},
		},
		{
			name:   "page up",
			scroll: tea.KeyMsg{Type: tea.KeyPgUp},
		},
		{
			name: "page down",
			setup: []tea.Msg{
				tea.KeyMsg{Type: tea.KeyPgUp},
			},
			scroll: tea.KeyMsg{Type: tea.KeyPgDown},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newCompactDetailModel(t, 4)
			for idx := 0; idx < 8; idx++ {
				m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf("entry %d\nhidden %d a\nhidden %d b\nhidden %d c", idx, idx, idx, idx)})
			}
			m = updateModel(t, m, ToggleModeMsg{})
			for _, msg := range tt.setup {
				m = updateModel(t, m, msg)
			}

			m = updateModel(t, m, tt.scroll)
			centerVisible := centerVisibleSelectableDetailEntry(t, m)
			if !m.detailSelectedActive || m.detailSelectedEntry != centerVisible {
				t.Fatalf("expected scroll to focus center visible entry %d, got active=%v entry=%d", centerVisible, m.detailSelectedActive, m.detailSelectedEntry)
			}

			m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			if _, ok := m.detailExpandedEntries[centerVisible]; !ok {
				t.Fatalf("expected enter after scroll to expand center visible entry %d, got %+v", centerVisible, m.detailExpandedEntries)
			}
		})
	}
}

func TestCompactDetailShortSelectedMessagesDoNotShowExpansionAffordance(t *testing.T) {
	m := newCompactDetailModel(t, 6, WithTheme("dark"))
	m = updateModel(t, m, AppendTranscriptMsg{Role: "user", Text: "short user"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "short assistant"})
	m = updateModel(t, m, ToggleModeMsg{})

	if action, ok := m.DetailSelectedExpansionAction(); ok || action != "" {
		t.Fatalf("did not expect expansion action for short message, got %q ok=%v", action, ok)
	}
	before := m.View()
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.detailExpandedEntries) != 0 {
		t.Fatalf("did not expect enter on short message to mutate expansion state, got %+v", m.detailExpandedEntries)
	}
	if action, ok := m.DetailSelectedExpansionAction(); ok || action != "" {
		t.Fatalf("did not expect enter on short message to introduce expansion action, got %q ok=%v", action, ok)
	}
	after := m.View()
	if xansi.Strip(after) != xansi.Strip(before) {
		t.Fatalf("did not expect enter on short message to change view, before=%q after=%q", xansi.Strip(before), xansi.Strip(after))
	}
}
