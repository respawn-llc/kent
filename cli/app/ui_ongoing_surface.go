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
	return ongoing.FrameInput{
		Size:         ongoing.Size{Width: width, Height: height},
		Theme:        m.theme,
		SpinnerFrame: m.spinnerFrame,
		Sections:     sections,
		Overlay:      layout.ongoingInputOverlay(width, height, style),
	}
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
