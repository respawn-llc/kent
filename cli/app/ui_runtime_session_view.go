package app

import runtimepb "core/shared/protoapi/gen/kent/api/runtime"

func (m *uiModel) localRuntimeSessionView() *runtimepb.SessionView {
	return &runtimepb.SessionView{
		SessionId:             m.sessionID,
		SessionName:           m.sessionName,
		ConversationFreshness: m.conversationFreshness,
	}
}
