package app

import tea "github.com/charmbracelet/bubbletea"

func (m *uiModel) acknowledgePendingInterrupt() tea.Cmd {
	if m == nil || !m.hasPendingInterrupt() {
		return nil
	}
	m.pendingInterrupt = nil
	m.activity = uiActivityInterrupted
	return m.interruptedStatusNoticeCmd()
}
