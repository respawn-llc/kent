package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type uiKeyFeatureReducer struct {
	model *uiModel
}

func (m *uiModel) keyReducer() uiKeyFeatureReducer {
	return uiKeyFeatureReducer{model: m}
}

func (r uiKeyFeatureReducer) Update(msg tea.Msg) uiFeatureUpdateResult {
	m := r.model
	if keyMsg, ok, source := normalizeKeyMsgWithSource(msg); ok {
		if m.debugKeys {
			m.setDebugKeyTransientStatus(msg, keyMsg, source)
		}
		if m.helpVisible {
			m.helpVisible = false
			if isHelpKey(keyMsg, m) && m.canShowHelp() {
				m.lastEscAt = time.Time{}
				m.layout().syncViewport()
				return handledUIFeatureUpdate(m, nil)
			}
		}
		if isHelpKey(keyMsg, m) && m.canShowHelp() {
			m.lastEscAt = time.Time{}
			m.helpVisible = !m.helpVisible
			m.layout().syncViewport()
			return handledUIFeatureUpdate(m, nil)
		}
		switch m.inputModeState().Mode {
		case uiInputModeAsk:
			next, cmd := m.askController().handleKey(keyMsg)
			nextModel := next.(*uiModel)
			nextModel.layout().syncViewport()
			return handledUIFeatureUpdate(nextModel, cmd)
		default:
			next, cmd := m.inputController().handleKey(keyMsg)
			nextModel := next.(*uiModel)
			nextModel.layout().syncViewport()
			return handledUIFeatureUpdate(nextModel, cmd)
		}
	}
	if _, isKey := msg.(tea.KeyMsg); isKey {
		if m.helpVisible {
			m.helpVisible = false
		}
		m.lastEscAt = time.Time{}
		m.layout().syncViewport()
		return handledUIFeatureUpdate(m, nil)
	}
	return uiFeatureUpdateResult{}
}
