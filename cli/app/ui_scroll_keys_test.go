package app

import (
	"fmt"
	"strings"
	"testing"

	"core/cli/tui"
	"core/server/runtime"
	"core/shared/clientui"
	"core/shared/theme"
	"core/shared/transcript"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPageKeysScrollTranscriptWhileInputFocused(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.termHeight = 8

	for i := 0; i < 12; i++ {
		m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf("line %d", i)})
	}
	m.forwardToView(tui.ToggleModeMsg{}) // detail mode starts at scroll=0

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	updated := next.(*uiModel)
	if down := updated.view.View(); down == "" {
		t.Fatal("expected detail transcript to remain visible after pgdown")
	}

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	updated = next.(*uiModel)
	if up := updated.view.View(); up == "" {
		t.Fatal("expected detail transcript to remain visible after pgup")
	}
}

func TestDetailModeUpDownScrollTranscript(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.termHeight = 8
	m.layout().syncViewport()

	for i := 0; i < 16; i++ {
		m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf("line %d", i)})
	}
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})

	initial := stripDetailSelectionRail(stripANSIAndTrimRight(m.view.View()))
	if initial == "" {
		t.Fatal("expected detail transcript visible before scrolling")
	}
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyUp})
	afterUp := stripDetailSelectionRail(stripANSIAndTrimRight(m.view.View()))
	if afterUp == initial {
		t.Fatal("expected detail transcript to change after up")
	}

	beforeDownScroll := m.view.DetailScroll()
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if got := m.view.DetailScroll(); got > beforeDownScroll {
		t.Fatalf("expected detail down after prior up not to move away from bottom, got %d from %d", got, beforeDownScroll)
	}
	if afterDown := stripDetailSelectionRail(stripANSIAndTrimRight(m.view.View())); afterDown == afterUp {
		t.Fatal("expected detail transcript to change after down")
	}
}

func TestDetailModeLineScrollRoundTripsScrollAndSelectionState(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.termHeight = 8
	m.layout().syncViewport()

	for idx := 0; idx < 20; idx++ {
		m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf("state entry %02d", idx)})
	}
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	startScroll := m.view.DetailScroll()
	startSelected, startSelectedOK := m.view.DetailSelectedEntry()
	if !startSelectedOK {
		t.Fatal("expected selected detail entry before state round-trip")
	}

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if got := m.view.DetailScroll(); got != startScroll+1 {
		t.Fatalf("expected up to move detail scroll state by one line, got %d want %d", got, startScroll+1)
	}
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if got := m.view.DetailScroll(); got != startScroll {
		t.Fatalf("expected up/down detail scroll state to round-trip, got %d want %d", got, startScroll)
	}
	selected, selectedOK := m.view.DetailSelectedEntry()
	if !selectedOK || selected != startSelected {
		t.Fatalf("expected selected entry state to round-trip, got %d ok=%v want %d", selected, selectedOK, startSelected)
	}
}

func TestDetailModeArrowScrollsDetailByLineAndTracksCenterSelection(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.termHeight = 8
	m.layout().syncViewport()
	m.forwardToView(tui.AppendTranscriptMsg{
		Role:       "tool_call",
		Text:       "first-command",
		ToolCallID: "call_1",
		ToolCall:   &transcript.ToolCallMeta{ToolName: "exec_command", IsShell: true, Command: "first-command"},
	})
	firstOutput := make([]string, 0, 20)
	for idx := 0; idx < 20; idx++ {
		firstOutput = append(firstOutput, fmt.Sprintf("first output line %02d", idx))
	}
	m.forwardToView(tui.AppendTranscriptMsg{Role: "tool_result_ok", ToolCallID: "call_1", Text: strings.Join(firstOutput, "\n")})
	m.forwardToView(tui.AppendTranscriptMsg{
		Role:       "tool_call",
		Text:       "second-command",
		ToolCallID: "call_2",
		ToolCall:   &transcript.ToolCallMeta{ToolName: "exec_command", IsShell: true, Command: "second-command"},
	})
	m.forwardToView(tui.AppendTranscriptMsg{Role: "tool_result_ok", ToolCallID: "call_2", Text: "second output"})
	m.forwardToView(tui.AppendTranscriptMsg{
		Role:       "tool_call",
		Text:       "third-command",
		ToolCallID: "call_3",
		ToolCall:   &transcript.ToolCallMeta{ToolName: "exec_command", IsShell: true, Command: "third-command"},
	})
	m.forwardToView(tui.AppendTranscriptMsg{Role: "tool_result_ok", ToolCallID: "call_3", Text: "third output"})
	for idx := 0; idx < 10; idx++ {
		m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf("tail entry %02d", idx)})
	}

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	topVisible := 0
	ok := false
	for guard := 0; guard < 20; guard++ {
		topVisible, _, ok = m.view.DetailVisibleEntryRange()
		if ok && topVisible == 0 {
			break
		}
		m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	}
	topVisible, _, ok = m.view.DetailVisibleEntryRange()
	if !ok || topVisible != 0 {
		t.Fatalf("expected top command visible before expansion, range=(%d, ok=%v) view=%q", topVisible, ok, stripANSIAndTrimRight(m.view.View()))
	}
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	beforeScroll := m.view.DetailScroll()
	scrolled := 0
	for guard := 0; guard < 20 && scrolled < 5; guard++ {
		before := m.view.DetailScroll()
		m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
		got := m.view.DetailScroll()
		if got == before {
			continue
		}
		scrolled++
		if want := before + 1; got != want {
			t.Fatalf("scroll step %d: expected detail arrow scroll by one line, got %d want %d", scrolled, got, want)
		}
		if selected := selectedDetailContentLine(t, m.view.View()); selected == "" {
			t.Fatalf("scroll step %d: expected center selection to remain visible", scrolled)
		}
	}
	if scrolled != 5 {
		t.Fatalf("expected five one-line scroll steps after selection walked to center, got %d from start scroll %d", scrolled, beforeScroll)
	}

	if selected := selectedDetailContentLine(t, m.view.View()); selected == "" {
		t.Fatal("expected centered selection after line scrolling")
	}
	if spacer := selectedDetailSpacerLine(t, m.view.View()); spacer == "" {
		t.Fatal("expected selected card spacer rail after line scrolling")
	}
}

func TestDetailModeEnterDoesNotRequestTranscriptPage(t *testing.T) {
	client := &recordingTranscriptRuntimeClient{}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.termWidth = 80
	m.termHeight = 12
	m.layout().syncViewport()
	for idx := 0; idx < 4; idx++ {
		m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf("entry %02d\nhidden", idx)})
	}
	m.detailTranscript.replace(clientui.TranscriptPage{
		SessionID:    "session-1",
		Offset:       100,
		TotalEntries: 200,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
	})
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})

	controller := uiInputController{model: m}
	next, cmd := controller.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("expected detail enter to avoid transcript paging command, got %T", cmd)
	}
	_ = next
	if got := len(client.loadRequests); got != 0 {
		t.Fatalf("expected no transcript page loads on detail enter, got %d", got)
	}
}

func TestDetailModeMouseWheelScrollTranscript(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.termHeight = 8
	m.layout().syncViewport()

	for i := 0; i < 16; i++ {
		m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf("line %d", i)})
	}
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})

	initial := stripDetailSelectionRail(stripANSIAndTrimRight(m.view.View()))
	if initial == "" {
		t.Fatal("expected detail transcript visible before mouse scrolling")
	}

	m = updateUIModel(t, m, tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	afterWheelUp := stripDetailSelectionRail(stripANSIAndTrimRight(m.view.View()))
	if afterWheelUp == initial {
		t.Fatal("expected detail transcript to change after mouse wheel up")
	}

	beforeWheelDownScroll := m.view.DetailScroll()
	m = updateUIModel(t, m, tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if got := m.view.DetailScroll(); got > beforeWheelDownScroll {
		t.Fatalf("expected detail wheel down after prior wheel up not to move away from bottom, got %d from %d", got, beforeWheelDownScroll)
	}
	if afterWheelDown := stripDetailSelectionRail(stripANSIAndTrimRight(m.view.View())); afterWheelDown == afterWheelUp {
		t.Fatal("expected detail transcript to change after mouse wheel down")
	}
}

func TestDetailModeUpAfterBottomScrollbackWalksHighlightOneVisualLine(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.termHeight = 8
	m.layout().syncViewport()

	for idx := 0; idx < 24; idx++ {
		m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf("line %02d", idx)})
	}
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})

	centerLine := m.termHeight / 2
	for guard := 0; guard < 12; guard++ {
		if selectedDetailLineIndex(t, m.view.View()) > centerLine {
			break
		}
		m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyUp})
	}
	beforeLine := selectedDetailLineIndex(t, m.view.View())
	if beforeLine <= centerLine {
		t.Fatalf("expected bottom scrollback repro to place selected row below center, line=%d center=%d view=%q", beforeLine, centerLine, stripANSIAndTrimRight(m.view.View()))
	}
	beforeScroll := m.view.DetailScroll()

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyUp})

	if got := m.view.DetailScroll(); got != beforeScroll {
		t.Fatalf("expected camera to hold while selected row walks toward center, got scroll %d want %d", got, beforeScroll)
	}
	if got := selectedDetailLineIndex(t, m.view.View()); got != beforeLine-1 {
		t.Fatalf("expected selected row to move up one visual line, got %d want %d view=%q", got, beforeLine-1, stripANSIAndTrimRight(m.view.View()))
	}
}

func TestRollbackForkSubmissionUsesPagedDetailAbsoluteIndex(t *testing.T) {
	m := newProjectedStaticUIModel(WithUIInitialTranscript([]UITranscriptEntry{
		{Role: "user", Text: "tail user"},
		{Role: "assistant", Text: "tail answer"},
	}))
	m.termWidth = 80
	m.termHeight = 10
	m.layout().syncViewport()
	detailPage := clientui.TranscriptPage{
		Offset:       40,
		TotalEntries: 44,
		Entries: []clientui.ChatEntry{
			{Role: "user", Text: "first paged user"},
			{Role: "assistant", Text: "first answer"},
			{Role: "user", Text: "second paged user"},
			{Role: "assistant", Text: "second answer"},
		},
	}
	m.detailTranscript.replace(detailPage)
	m.forwardToView(tui.SetModeMsg{Mode: tui.ModeDetail})
	m.forwardToView(tui.SetConversationMsg{
		BaseOffset:   detailPage.Offset,
		TotalEntries: detailPage.TotalEntries,
		Entries:      transcriptEntriesFromPage(detailPage),
	})

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !testRollbackEditing(m) {
		t.Fatal("expected rollback editing mode before fork submission")
	}

	m.input = "edited second paged user"
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if updated.exitAction != UIActionForkRollback {
		t.Fatalf("expected fork rollback action, got %q", updated.exitAction)
	}
	if updated.nextForkRollbackTargetID == "" {
		t.Fatal("expected rollback target id")
	}
	if updated.nextSessionInitialPrompt != "edited second paged user" {
		t.Fatalf("expected edited prompt to be used for fork, got %q", updated.nextSessionInitialPrompt)
	}
}

type pagedRollbackRuntimeClient struct {
	runtimeControlFakeClient
	requests []clientui.TranscriptPageRequest
}

func (c *pagedRollbackRuntimeClient) LoadTranscriptPage(req clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	c.requests = append(c.requests, req)
	entries := make([]clientui.ChatEntry, req.Limit)
	for idx := range entries {
		absolute := req.Offset + idx
		entries[idx] = clientui.ChatEntry{Role: "assistant", Text: fmt.Sprintf("answer %04d", absolute)}
	}
	if len(entries) > 0 {
		entries[0] = clientui.ChatEntry{Role: "user", Text: fmt.Sprintf("user %04d", req.Offset), RollbackTargetID: rollbackTargetIDForTestSelection(req.Offset)}
	}
	return clientui.TranscriptPage{
		SessionID:    "session-1",
		Offset:       req.Offset,
		TotalEntries: 1502,
		Entries:      entries,
	}, nil
}

func TestRollbackSelectionPagesToFirstUserAcrossTrimmedDetailWindow(t *testing.T) {
	client := &pagedRollbackRuntimeClient{}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.termWidth = 80
	m.termHeight = 10
	m.layout().syncViewport()
	tailEntries := make([]clientui.ChatEntry, 252)
	for idx := range tailEntries {
		absolute := 1250 + idx
		tailEntries[idx] = clientui.ChatEntry{Role: "assistant", Text: fmt.Sprintf("tail answer %04d", absolute)}
	}
	tailEntries[0] = clientui.ChatEntry{Role: "user", Text: "user 1250", RollbackTargetID: rollbackTargetIDForTestSelection(1250)}
	tailEntries[250] = clientui.ChatEntry{Role: "user", Text: "user 1500", RollbackTargetID: rollbackTargetIDForTestSelection(1500)}
	detailPage := clientui.TranscriptPage{
		SessionID:    "session-1",
		Offset:       1250,
		TotalEntries: 1502,
		Entries:      tailEntries,
	}
	m.detailTranscript.replace(detailPage)
	m.forwardToView(tui.SetModeMsg{Mode: tui.ModeDetail})
	m.forwardToView(tui.SetConversationMsg{
		BaseOffset:   detailPage.Offset,
		TotalEntries: detailPage.TotalEntries,
		Entries:      transcriptEntriesFromPage(detailPage),
	})

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if !testRollbackSelecting(m) {
		t.Fatal("expected rollback selection mode")
	}
	for steps := 0; testRollbackCandidates(m)[testRollbackSelection(m)].TranscriptIndex != 0; steps++ {
		if steps > 20 {
			t.Fatalf("rollback selection did not reach first user, selection=%d candidates=%+v requests=%+v", testRollbackSelection(m), testRollbackCandidates(m), client.requests)
		}
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = next.(*uiModel)
		if cmd == nil {
			continue
		}
		msgs := collectCmdMessages(t, cmd)
		if len(msgs) != 1 {
			t.Fatalf("expected one transcript page response, got %#v", msgs)
		}
		refresh, ok := msgs[0].(runtimeTranscriptRefreshedMsg)
		if !ok {
			t.Fatalf("expected runtimeTranscriptRefreshedMsg, got %T", msgs[0])
		}
		m = updateUIModel(t, m, refresh)
	}
	if len(client.requests) < 5 {
		t.Fatalf("expected multiple page requests across trimmed detail window, got %+v", client.requests)
	}
	if got := m.detailTranscript.offset; got != 0 {
		t.Fatalf("expected detail cache to include first page after paging to start, offset=%d", got)
	}
	if got := testRollbackCandidates(m)[testRollbackSelection(m)].Text; got != "user 0000" {
		t.Fatalf("expected selection at first user message, got %q", got)
	}
}

func TestRollbackTransitionsUseFixedDetailAltScreen(t *testing.T) {
	altOnEntry := true

	t.Run("ongoing_to_picker_to_edit_to_picker", func(t *testing.T) {
		m := newProjectedStaticUIModel(
			WithUIInitialTranscript([]UITranscriptEntry{
				{Role: "user", Text: "u1"},
				{Role: "assistant", Text: "a1"},
				{Role: "user", Text: "u2"},
			}),
		)
		m.termWidth = 80
		m.termHeight = 10
		m.layout().syncViewport()

		m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		m = next.(*uiModel)
		if cmd == nil {
			t.Fatal("expected picker entry transition command")
		}
		if !testRollbackSelecting(m) || m.view.Mode() != tui.ModeDetail || m.altScreenActive != altOnEntry {
			t.Fatalf("unexpected picker entry state: selecting=%t mode=%q alt=%t", testRollbackSelecting(m), m.view.Mode(), m.altScreenActive)
		}
		m = updateUIModel(t, m, tea.MouseMsg{Button: tea.MouseButtonWheelUp})
		if testRollbackSelection(m) != 1 {
			t.Fatalf("expected mouse wheel ignored while selecting, got selection %d", testRollbackSelection(m))
		}

		next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(*uiModel)
		if cmd == nil {
			t.Fatal("expected edit transition command")
		}
		if !testRollbackEditing(m) || m.view.Mode() != tui.ModeOngoing {
			t.Fatalf("unexpected edit state: editing=%t mode=%q", testRollbackEditing(m), m.view.Mode())
		}
		beforeScroll := m.view.OngoingScroll()
		m = updateUIModel(t, m, tea.MouseMsg{Button: tea.MouseButtonWheelUp})
		if got := m.view.OngoingScroll(); got != beforeScroll {
			t.Fatalf("expected mouse wheel ignored while editing, got scroll %d want %d", got, beforeScroll)
		}

		next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		m = next.(*uiModel)
		if cmd == nil {
			t.Fatal("expected edit cancel transition command")
		}
		if !testRollbackSelecting(m) || m.view.Mode() != tui.ModeDetail || m.altScreenActive != altOnEntry {
			t.Fatalf("unexpected picker restore state: selecting=%t mode=%q alt=%t", testRollbackSelecting(m), m.view.Mode(), m.altScreenActive)
		}
	})

	t.Run("detail_to_picker_to_edit_to_picker", func(t *testing.T) {
		m := newProjectedStaticUIModel(
			WithUIInitialTranscript([]UITranscriptEntry{
				{Role: "user", Text: "u1"},
				{Role: "assistant", Text: "a1"},
				{Role: "user", Text: "u2"},
			}),
		)
		m.termWidth = 80
		m.termHeight = 10
		m.layout().syncViewport()
		m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
		if m.view.Mode() != tui.ModeDetail || m.altScreenActive != altOnEntry {
			t.Fatalf("unexpected detail state before picker: mode=%q alt=%t", m.view.Mode(), m.altScreenActive)
		}

		m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		m = next.(*uiModel)
		if !testRollbackSelecting(m) || m.view.Mode() != tui.ModeDetail || m.altScreenActive != altOnEntry {
			t.Fatalf("unexpected detail picker state: selecting=%t mode=%q alt=%t", testRollbackSelecting(m), m.view.Mode(), m.altScreenActive)
		}

		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(*uiModel)
		if !testRollbackEditing(m) || m.view.Mode() != tui.ModeDetail {
			t.Fatalf("unexpected detail edit state: editing=%t mode=%q", testRollbackEditing(m), m.view.Mode())
		}
		beforeScroll := m.view.DetailScroll()
		m = updateUIModel(t, m, tea.MouseMsg{Button: tea.MouseButtonWheelUp})
		if got := m.view.DetailScroll(); got != beforeScroll {
			t.Fatalf("expected mouse wheel ignored while detail edit active, got scroll %d want %d", got, beforeScroll)
		}

		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		m = next.(*uiModel)
		if !testRollbackSelecting(m) || m.view.Mode() != tui.ModeDetail || m.altScreenActive != altOnEntry {
			t.Fatalf("unexpected detail picker restore state: selecting=%t mode=%q alt=%t", testRollbackSelecting(m), m.view.Mode(), m.altScreenActive)
		}
	})
}

func TestUpDownRouteByTranscriptMode(t *testing.T) {
	m := newProjectedStaticUIModel(WithUIPromptHistory([]string{"hello"}))
	m.termWidth = 80
	m.termHeight = 8
	m.layout().syncViewport()
	for i := 0; i < 20; i++ {
		m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf("line %d", i)})
	}

	ongoingStart := m.view.OngoingScroll()
	if ongoingStart == 0 {
		t.Fatal("expected ongoing transcript to be scrollable")
	}

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.input != "hello" {
		t.Fatalf("expected ongoing mode up to recall prompt history, got %q", m.input)
	}
	if got := m.view.OngoingScroll(); got != ongoingStart {
		t.Fatalf("expected ongoing mode up not to scroll transcript, got %d from %d", got, ongoingStart)
	}

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected detail mode, got %q", m.view.Mode())
	}
	initialDetail := m.view.View()

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyUp})
	afterDetailUp := m.view.View()
	if afterDetailUp == initialDetail {
		t.Fatal("expected detail mode up to scroll transcript")
	}
	if m.input != "hello" {
		t.Fatalf("expected detail mode scrolling not to mutate recalled input, got %q", m.input)
	}
}

func selectedDetailContentLine(t *testing.T, view string) string {
	t.Helper()

	for _, line := range strings.Split(stripANSIAndTrimRight(view), "\n") {
		if strings.HasPrefix(line, theme.SelectionRailGlyph) && strings.TrimSpace(strings.TrimPrefix(line, theme.SelectionRailGlyph)) != "" {
			return line
		}
	}
	t.Fatalf("expected selected detail line in %q", stripANSIAndTrimRight(view))
	return ""
}

func selectedDetailLineIndex(t *testing.T, view string) int {
	t.Helper()

	for idx, line := range strings.Split(stripANSIAndTrimRight(view), "\n") {
		if strings.HasPrefix(line, theme.SelectionRailGlyph) && strings.TrimSpace(strings.TrimPrefix(line, theme.SelectionRailGlyph)) != "" {
			return idx
		}
	}
	t.Fatalf("expected selected detail line in %q", stripANSIAndTrimRight(view))
	return -1
}

func selectedDetailSpacerLine(t *testing.T, view string) string {
	t.Helper()

	for _, line := range strings.Split(stripANSIAndTrimRight(view), "\n") {
		if strings.HasPrefix(line, theme.SelectionRailGlyph) && strings.TrimSpace(strings.TrimPrefix(line, theme.SelectionRailGlyph)) == "" {
			return line
		}
	}
	t.Fatalf("expected selected detail spacer line in %q", stripANSIAndTrimRight(view))
	return ""
}

func stripDetailSelectionRail(view string) string {
	lines := strings.Split(view, "\n")
	for idx, line := range lines {
		if strings.HasPrefix(line, theme.SelectionRailGlyph) {
			lines[idx] = theme.SelectionRailBlank + strings.TrimPrefix(line, theme.SelectionRailGlyph)
		}
	}
	return strings.Join(lines, "\n")
}

func TestMainInputUpDownAtBoundsStayInInput(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.termHeight = 8
	m.layout().syncViewport()
	for i := 0; i < 20; i++ {
		m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf("line %d", i)})
	}
	m.input = "abcd"
	m.inputCursor = 2

	start := m.view.OngoingScroll()
	if start == 0 {
		t.Fatal("expected ongoing transcript to be scrollable")
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	updated := next.(*uiModel)
	if updated.inputCursor != 0 {
		t.Fatalf("expected first up to move cursor to start, got %d", updated.inputCursor)
	}
	if got := updated.view.OngoingScroll(); got != start {
		t.Fatalf("expected first up not to scroll transcript, got %d from %d", got, start)
	}

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyUp})
	updated = next.(*uiModel)
	if updated.inputCursor != 0 {
		t.Fatalf("expected second up at top to stay at start, got %d", updated.inputCursor)
	}
	if got := updated.view.OngoingScroll(); got != start {
		t.Fatalf("expected second up at top not to scroll transcript, got %d from %d", got, start)
	}

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(*uiModel)
	if updated.inputCursor != len([]rune(updated.input)) {
		t.Fatalf("expected first down to move cursor to end, got %d", updated.inputCursor)
	}
	if got := updated.view.OngoingScroll(); got != start {
		t.Fatalf("expected first down not to scroll transcript, got %d from %d", got, start)
	}

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(*uiModel)
	if updated.inputCursor != len([]rune(updated.input)) {
		t.Fatalf("expected second down at end to stay at end, got %d", updated.inputCursor)
	}
	if got := updated.view.OngoingScroll(); got != start {
		t.Fatalf("expected second down at end not to scroll transcript, got %d from %d", got, start)
	}
}

func TestReviewerRunStillAllowsEditingWithoutTranscriptScroll(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.termHeight = 8
	m.layout().syncViewport()
	for i := 0; i < 20; i++ {
		m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: fmt.Sprintf("line %d", i)})
	}
	m.setBusy(true)
	m.activity = uiActivityRunning
	m.input = "keep this draft"

	start := m.view.OngoingScroll()
	if start == 0 {
		t.Fatal("expected ongoing transcript to be scrollable")
	}

	next, _ := m.Update(projectedRuntimeEventMsg(runtime.Event{Kind: runtime.EventReviewerStarted}))
	locked := next.(*uiModel)
	if !locked.isReviewerBlocking() {
		t.Fatal("expected reviewer running state")
	}
	if locked.isInputSubmitLocked() {
		t.Fatal("did not expect reviewer to lock input")
	}

	next, _ = locked.Update(tea.KeyMsg{Type: tea.KeyUp})
	locked = next.(*uiModel)

	next, _ = locked.Update(tea.KeyMsg{Type: tea.KeyUp})
	locked = next.(*uiModel)
	if locked.inputCursor != 0 {
		t.Fatalf("expected up to move cursor to start while reviewer runs, got %d", locked.inputCursor)
	}
	if got := locked.view.OngoingScroll(); got != start {
		t.Fatalf("expected up not to scroll transcript while reviewer runs, got %d from %d", got, start)
	}
	if locked.input != "keep this draft" {
		t.Fatalf("expected input text preserved while reviewer runs, got %q", locked.input)
	}

	next, _ = locked.Update(tea.KeyMsg{Type: tea.KeyDown})
	locked = next.(*uiModel)

	next, _ = locked.Update(tea.KeyMsg{Type: tea.KeyDown})
	locked = next.(*uiModel)
	if locked.inputCursor != len([]rune(locked.input)) {
		t.Fatalf("expected down to move cursor to end while reviewer runs, got %d", locked.inputCursor)
	}
	if got := locked.view.OngoingScroll(); got != start {
		t.Fatalf("expected down not to scroll transcript while reviewer runs, got %d from %d", got, start)
	}

	next, _ = locked.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	locked = next.(*uiModel)
	if locked.input != "keep this draftx" {
		t.Fatalf("expected input editable while reviewer runs, got %q", locked.input)
	}
}
