package app

import (
	"core/cli/tui/ongoing"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *uiModel) reduceWindowMessage(msg tea.Msg) uiFeatureUpdateResult {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		wasNativeOngoing := m.nativeOngoingSurfaceActive()
		previousSize := m.terminalGeometry.Size()
		if msg.Width > 0 && msg.Height > 0 {
			m.terminalGeometry = terminalGeometryKnown(msg.Width, msg.Height)
		} else {
			m.terminalGeometry = terminalGeometryUnknown()
		}
		m.updateConnectionRemovalPickerSize()
		m.syncRendererOutputGate()
		m.layout().syncViewport()
		var resizeCmd tea.Cmd
		if m.ongoingSurface != nil {
			observation := m.ongoingSurface.ObserveResize(ongoing.Size{Width: msg.Width, Height: msg.Height})
			resizeCmd = m.handleOngoingResult(observation)
			if m.terminalGeometry.IsKnown() && previousSize == nil && m.ongoingRehydration.pending != nil && observation.Action != ongoing.ResultScheduleResizeRehydration {
				resizeCmd = m.scheduleOngoingResizeRehydration()
			}
		}
		if !m.terminalGeometry.IsKnown() {
			m.ongoingRehydration.token++
		}
		desiredOngoing := m.ongoingSurface != nil &&
			desiredOngoingOwnership(m.terminalGeometry, terminalDestinationForSurface(m.surface()))
		if !desiredOngoing || m.ongoingSurface == nil {
			return handledUIFeatureUpdate(m, tea.Batch(
				resizeCmd,
				m.reconcileOngoingOwnership(),
				m.scheduleCurrentQuestionProjection(),
			))
		}
		size := ongoing.Size{Width: msg.Width, Height: msg.Height}
		var result ongoing.Result
		var err error
		if m.ongoingTranscript != nil {
			result, err = m.ongoingTranscript.Resize(size)
		} else {
			result, err = m.ongoingSurface.Resize(size, m.ongoingFrameInput())
		}
		if err != nil {
			return handledUIFeatureUpdate(m, m.handleOngoingSurfaceError(err))
		}
		cmd := tea.Batch(resizeCmd, m.handleOngoingResult(result))
		if previousSize == nil || !wasNativeOngoing {
			cmd = tea.Batch(cmd, m.reconcileOngoingOwnership())
		}
		return handledUIFeatureUpdate(m, tea.Batch(cmd, m.scheduleCurrentQuestionProjection()))
	}
	return uiFeatureUpdateResult{}
}
