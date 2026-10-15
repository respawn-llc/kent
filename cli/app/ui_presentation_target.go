package app

import (
	"context"
	"core/shared/apicontract"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	"core/shared/runtimeids"
)

type uiPresentationTarget interface {
	isUIPresentationTarget()
}

type uiNewSessionTarget struct {
	creation     *chatpb.NewChatTarget
	settings     apicontract.ChatSettingsService
	auth         apicontract.AuthBootstrapService
	mutations    apicontract.ChatMutationService
	login        func(context.Context) error
	settingsLoad uiNewSessionSettingsLoad
}

func (uiNewSessionTarget) isUIPresentationTarget() {}

type uiKnownSessionTarget struct {
	id runtimeids.SessionID
}

func (uiKnownSessionTarget) isUIPresentationTarget() {}

func (m *uiModel) newChatTarget() *chatpb.NewChatTarget {
	target := m.newSessionTarget()
	if target == nil {
		return nil
	}
	return target.creation
}

func (m *uiModel) newSessionTarget() *uiNewSessionTarget {
	target, _ := m.presentationTarget.(*uiNewSessionTarget)
	return target
}

func (m *uiModel) setKnownSessionTarget(id string) {
	parsed, err := runtimeids.ParseSessionID(id)
	if err != nil {
		panic(err)
	}
	m.presentationTarget = uiKnownSessionTarget{id: parsed}
}
