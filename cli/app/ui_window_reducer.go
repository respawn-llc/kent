package app

import tea "github.com/charmbracelet/bubbletea"

type uiWindowFeatureReducer struct {
	model *uiModel
}

func (m *uiModel) windowReducer() uiWindowFeatureReducer {
	return uiWindowFeatureReducer{model: m}
}

func (r uiWindowFeatureReducer) Update(msg tea.Msg) uiFeatureUpdateResult {
	m := r.model
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		previousWidth := m.termWidth
		windowWasKnown := m.windowSizeKnown
		historyWasReplayed := m.nativeHistoryReplayed()
		m.termWidth = msg.Width
		m.termHeight = msg.Height
		m.windowSizeKnown = true
		if msg.Width > 0 {
			m.nativeReplayWidth = msg.Width
			m.nativeFormatterWidth = msg.Width
		}
		m.layout().syncViewport()
		if !m.nativeHistoryReplayed() {
			return handledUIFeatureUpdate(m, m.syncNativeHistoryFromTranscriptAndTrackCommittedDelivery())
		}
		if historyWasReplayed && windowWasKnown && previousWidth > 0 && msg.Width > 0 && msg.Width != previousWidth {
			return handledUIFeatureUpdate(m, m.reflowNativeHistoryForResize())
		}
	}
	return uiFeatureUpdateResult{}
}
