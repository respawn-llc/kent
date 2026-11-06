package app

import (
	"context"
	"core/shared/apicontract"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	invocationpb "core/shared/protoapi/gen/kent/api/invocation"
	"core/shared/runtimeids"
)

type uiPresentationTarget interface {
	isUIPresentationTarget()
}

type uiNewSessionTarget struct {
	creation     *chatpb.NewChatTarget
	auth         apicontract.AuthBootstrapService
	login        func(context.Context) error
	settingsLoad uiNewSessionSettingsLoad
}

func (uiNewSessionTarget) isUIPresentationTarget() {}

type uiKnownSessionTarget struct {
	id         runtimeids.SessionID
	invocation *invocationpb.RunPromptOverrides
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
	if current, ok := m.presentationTarget.(uiKnownSessionTarget); ok && current.id == parsed {
		return
	}
	m.presentationTarget = uiKnownSessionTarget{id: parsed}
}
