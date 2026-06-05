package app

import "builder/shared/clientui"

func (m *uiModel) currentConversationFreshness() clientui.ConversationFreshness {
	switch cached := m.cachedRuntimeStatus().ConversationFreshness; cached {
	case clientui.ConversationFreshnessEstablished:
		m.conversationFreshness = cached
		m.localConversationTurn = true
	case clientui.ConversationFreshnessFresh:
		if !m.localConversationTurn {
			m.conversationFreshness = cached
		}
	}
	return m.conversationFreshness
}
