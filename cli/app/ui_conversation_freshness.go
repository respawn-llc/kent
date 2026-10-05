package app

import runtimepb "core/shared/protoapi/gen/kent/api/runtime"

func (m *uiModel) currentConversationFreshness() runtimepb.ConversationFreshness {
	switch cached := m.cachedRuntimeStatus().ConversationFreshness; cached {
	case runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED:
		m.conversationFreshness = cached
		m.localConversationTurn = true
	case runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_FRESH:
		if !m.localConversationTurn {
			m.conversationFreshness = cached
		}
	}
	return m.conversationFreshness
}
