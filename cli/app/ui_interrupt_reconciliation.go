package app

import tea "github.com/charmbracelet/bubbletea"

func (m *uiModel) acknowledgePendingInterrupt() tea.Cmd {
	if m == nil || !m.hasPendingInterrupt() {
		return nil
	}
	m.setPendingInterrupt(false)
	m.activity = uiActivityInterrupted
	return m.interruptedStatusNoticeCmd()
}
