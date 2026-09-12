package app

import runtimepb "core/shared/protoapi/gen/kent/api/runtime"

import "core/shared/textutil"

func (m *uiModel) localRuntimeSessionView() *runtimepb.SessionView {
	return &runtimepb.SessionView{
		SessionId:             m.sessionID,
		SessionName:           textutil.OptionalTrimmedString(m.sessionName),
		ConversationFreshness: m.conversationFreshness,
	}
}
