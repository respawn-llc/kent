package app

import (
	"fmt"
	"time"

	"core/cli/tui/ongoing"

	tea "github.com/charmbracelet/bubbletea"
)

type ongoingWidthRehydrationDebounceMsg struct {
	token uint64
}

type ongoingNormalBufferOwnedMsg struct {
	owned bool
}

func WithUIOngoingSurface(surface *ongoing.Surface) UIOption {
	return func(m *uiModel) {
		m.ongoingSurface = surface
		m.syncRendererOutputGate()
	}
}

func WithUIOngoingTranscriptEvents(events <-chan ongoingTranscriptEvent) UIOption {
	return func(m *uiModel) {
		m.eventDispatcher.transcriptEvents = events
	}
}

func WithUIClientLifecycleIssues(issues <-chan lifecycleHookIssue, done <-chan struct{}) UIOption {
	return func(m *uiModel) {
		m.eventDispatcher.lifecycleHookIssues = issues
		m.eventDispatcher.lifecycleHookDone = done
	}
}

func WithUIOngoingTranscriptReopen(request func()) UIOption {
	return func(m *uiModel) {
		m.requestOngoingOpen = request
	}
}

func (m *uiModel) nativeOngoingSurfaceActive() bool {
	return m.nativeOngoingSurfaceSelected() &&
		desiredOngoingOwnership(m.terminalGeometry, terminalDestinationForSurface(m.surface()))
}

func (m *uiModel) nativeOngoingSurfaceSelected() bool {
	return m != nil && m.ongoingSurface != nil && m.surface() == uiSurfaceOngoingTranscript
}

func (m *uiModel) scheduleOngoingWidthRehydration() tea.Cmd {
	if m == nil {
		return nil
	}
	m.ongoingWidthToken++
	token := m.ongoingWidthToken
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return ongoingWidthRehydrationDebounceMsg{token: token}
	})
}

func (m *uiModel) handleOngoingSurfaceError(err error) tea.Cmd {
	if err == nil {
		return nil
	}
	if m != nil {
		m.logf("ongoing.surface.error err=%q", err.Error())
	}
	return m.handleFatalUIError(fmt.Sprintf("ongoing terminal surface failed: %v", err), err)
}

func (m *uiModel) handleOngoingDeveloperError(err ongoing.DeveloperError) tea.Cmd {
	if m != nil {
		m.logf("ongoing.developer_error err=%q", err.Error())
	}
	ongoing.PanicDeveloperError(err)
	return nil
}

func (m *uiModel) renderNativeOngoingSurface() tea.Cmd {
	if m == nil || m.ongoingSurface == nil || !m.nativeOngoingSurfaceActive() {
		return nil
	}
	var result ongoing.Result
	var err error
	if m.ongoingTranscript != nil {
		result, err = m.ongoingTranscript.Render()
	} else {
		result, err = m.ongoingSurface.Render(m.ongoingFrameInput())
	}
	if err != nil {
		return m.handleOngoingSurfaceError(err)
	}
	return m.handleOngoingResult(result)
}

func (m *uiModel) batchWithNativeOngoingRepaint(cmd tea.Cmd) tea.Cmd {
	repaintCmd := m.renderNativeOngoingSurface()
	return tea.Batch(cmd, repaintCmd)
}

func (m *uiModel) ongoingOwnershipAfterSurfaceTransitionCmd(prev, next uiSurface) tea.Cmd {
	if m == nil || m.ongoingTranscript == nil || prev == next {
		return nil
	}
	return m.queueOngoingOwnershipReconciliation()
}

func (m *uiModel) setOngoingNormalBufferOwned(owned bool) tea.Cmd {
	if m == nil || m.ongoingTranscript == nil {
		return nil
	}
	var widthRehydrationCmd tea.Cmd
	if owned {
		if cmd := m.applyPendingOngoingScratchReset(); cmd != nil {
			return cmd
		}
		if m.pendingOngoingWidthReset {
			m.pendingOngoingWidthReset = false
			m.pendingOngoingResizeRepaint = false
			widthRehydrationCmd = m.scheduleOngoingWidthRehydration()
		}
	}
	resizeRepaint := owned && m.pendingOngoingResizeRepaint && widthRehydrationCmd == nil
	if resizeRepaint {
		m.pendingOngoingResizeRepaint = false
	}
	result, err := m.ongoingTranscript.SetNormalBufferOwned(owned)
	if err != nil {
		return m.handleOngoingSurfaceError(err)
	}
	resultCmd := m.handleOngoingResult(result)
	if resizeRepaint && result.Action == ongoing.ResultNoop {
		repaintResult, repaintErr := m.ongoingTranscript.Render()
		if repaintErr != nil {
			return m.handleOngoingSurfaceError(repaintErr)
		}
		return tea.Batch(resultCmd, widthRehydrationCmd, m.handleOngoingResult(repaintResult))
	}
	return tea.Batch(resultCmd, widthRehydrationCmd)
}

func (m *uiModel) handleOngoingResult(result ongoing.Result) tea.Cmd {
	switch result.Action {
	case ongoing.ResultNoop:
		return nil
	case ongoing.ResultScheduleWidthRehydration:
		return m.scheduleOngoingWidthRehydration()
	case ongoing.ResultRequestScratchRehydration:
		if m != nil && m.ongoingSurface != nil && m.nativeOngoingSurfaceActive() {
			if _, err := m.ongoingSurface.ResetForScratchHydration(result.Reason, m.ongoingFrameInput()); err != nil {
				return m.handleOngoingSurfaceError(err)
			}
			m.pendingOngoingScratchReset = nil
		} else if m != nil {
			reason := result.Reason
			m.pendingOngoingScratchReset = &reason
		}
		if m != nil && m.ongoingTranscript != nil {
			m.ongoingTranscript.ResetForScratchHydration()
		}
		if m != nil && m.requestOngoingOpen != nil {
			m.requestOngoingOpen()
		}
		return nil
	default:
		if m != nil && m.debugMode {
			panic(fmt.Sprintf("unknown ongoing surface result action %q", result.Action))
		}
		return nil
	}
}

func (m *uiModel) applyPendingOngoingScratchReset() tea.Cmd {
	if m == nil || m.pendingOngoingScratchReset == nil {
		return nil
	}
	reason := *m.pendingOngoingScratchReset
	m.pendingOngoingScratchReset = nil
	m.pendingOngoingResizeRepaint = false
	if m.ongoingSurface == nil {
		return nil
	}
	if _, err := m.ongoingSurface.ResetForScratchHydration(reason, m.ongoingFrameInput()); err != nil {
		return m.handleOngoingSurfaceError(err)
	}
	return nil
}

func (m *uiModel) ongoingFrameInput() ongoing.FrameInput {
	if m == nil {
		return ongoing.FrameInput{}
	}
	style := uiThemeStyles(m.theme)
	layout := m.layout()
	width := layout.effectiveWidth()
	height := layout.effectiveHeight()
	var sections []ongoing.FrameSection
	appendSection := func(kind ongoing.FrameSectionKind, lines []string) {
		if len(lines) == 0 {
			return
		}
		sections = append(sections, ongoing.FrameSection{Kind: kind, Lines: append([]string(nil), lines...)})
	}
	inputPane := layout.inputPaneProjectionWithBudget(width, max(1, height-3), style)
	if entries := layout.queuedMessages(); len(entries) > 0 {
		sections = append(sections, ongoing.FrameSection{
			Kind: ongoing.FrameSectionQueuedOrSteered,
			Demand: &ongoing.FrameDemand{
				PreferredHeight: len(entries),
				MinimumHeight:   min(2, len(entries)),
				Project: func(rows int) ongoing.SectionProjection {
					return ongoing.SectionProjection{StyledLines: projectQueuedMessageLines(entries, width, rows)}
				},
			},
		})
	}
	if len(inputPane.Lines) > 0 {
		sections = append(sections, ongoing.FrameSection{
			Kind: ongoing.FrameSectionInput,
			Demand: &ongoing.FrameDemand{
				PreferredHeight: len(inputPane.Lines),
				MinimumHeight:   min(3, len(inputPane.Lines)),
				Project: func(rows int) ongoing.SectionProjection {
					pane := layout.inputPaneProjectionWithBudget(width, rows-2, style)
					return ongoing.SectionProjection{
						Lines: pane.Lines,
						Cursor: ongoing.Cursor{
							Visible: pane.Cursor.Visible, Row: pane.Cursor.Row, Column: pane.Cursor.Col + 1,
						},
					}
				},
			},
		})
	}
	statusLine := layout.renderStatusLine(width, style)
	if statusLine != "" {
		appendSection(ongoing.FrameSectionStatus, []string{statusLine})
	}
	cursor := inputPane.Cursor
	frameCursor := ongoing.Cursor{}
	if cursor.Visible {
		frameCursor = ongoing.Cursor{
			Visible: true,
			Column:  cursor.Col + 1,
			Target: &ongoing.CursorTarget{
				SectionKind: ongoing.FrameSectionInput,
				Row:         cursor.Row,
			},
		}
	}
	frame := ongoing.FrameInput{
		Size:         ongoing.Size{Width: width, Height: height},
		Theme:        m.theme,
		SpinnerFrame: m.spinnerFrame,
		Sections:     sections,
		Cursor:       frameCursor,
		Overlay:      layout.ongoingInputOverlay(width, height, style),
	}
	if cursor.Visible {
		frame.Cursor.Row = ongoingFrameInputCursorTerminalRow(frame, cursor.Row)
	}
	return frame
}

func (l uiViewLayout) ongoingInputOverlay(width, height int, style uiStyles) *ongoing.FrameDemand {
	if l.model.helpVisible && l.model.canShowHelp() {
		return &ongoing.FrameDemand{
			PreferredHeight: len(l.renderHelpPane(width, height, style)),
			MinimumHeight:   3,
			Project: func(rows int) ongoing.SectionProjection {
				return ongoing.SectionProjection{Lines: l.renderHelpPane(width, rows, style)}
			},
		}
	}
	picker := l.model.activePickerPresentation()
	if !picker.visible {
		return nil
	}
	return &ongoing.FrameDemand{
		PreferredHeight: min(slashCommandPickerLines, len(picker.rows)),
		MinimumHeight:   1,
		Project: func(rows int) ongoing.SectionProjection {
			picker.lineCount = rows
			picker.start = pickerViewportStart(picker.selection, len(picker.rows), rows)
			return ongoing.SectionProjection{Lines: l.renderPicker(picker, width)}
		},
	}
}

func ongoingFrameInputCursorSectionRow(frame ongoing.FrameInput) (int, bool) {
	if !frame.Cursor.Visible {
		return 0, false
	}
	if frame.Cursor.Target != nil && frame.Cursor.Target.SectionKind == ongoing.FrameSectionInput && frame.Cursor.Target.Row > 0 {
		return frame.Cursor.Target.Row, true
	}
	start, end, ok := ongoingFrameSectionTerminalRows(frame, ongoing.FrameSectionInput)
	if !ok || frame.Cursor.Row < start || frame.Cursor.Row > end {
		return 0, false
	}
	return frame.Cursor.Row - start + 1, true
}

func ongoingFrameInputCursorTerminalRow(frame ongoing.FrameInput, cursorSectionRow int) int {
	start, end, ok := ongoingFrameSectionTerminalRows(frame, ongoing.FrameSectionInput)
	if !ok {
		return clampTerminalCursorRow(frame.Cursor.Row, frame.Size.Height)
	}
	if cursorSectionRow <= 0 {
		cursorSectionRow = 1
	}
	inputSectionLines := end - start + 1
	if cursorSectionRow > inputSectionLines {
		cursorSectionRow = inputSectionLines
	}
	return clampTerminalCursorRow(start+cursorSectionRow-1, frame.Size.Height)
}

func ongoingFrameSectionTerminalRows(frame ongoing.FrameInput, kind ongoing.FrameSectionKind) (int, int, bool) {
	totalSectionLines := 0
	for _, section := range frame.Sections {
		totalSectionLines += ongoingFrameSectionLineCount(section)
	}
	row := frame.Size.Height - totalSectionLines + 1
	for _, section := range frame.Sections {
		count := ongoingFrameSectionLineCount(section)
		if section.Kind == kind {
			return row, row + count - 1, true
		}
		row += count
	}
	return 0, 0, false
}

func ongoingFrameSectionLineCount(section ongoing.FrameSection) int {
	if section.Demand != nil {
		return section.Demand.PreferredHeight
	}
	return len(section.StyledLines) + len(section.Lines)
}

func clampTerminalCursorRow(row int, height int) int {
	if height <= 0 {
		return row
	}
	if row < 1 {
		return 1
	}
	if row > height {
		return height
	}
	return row
}
