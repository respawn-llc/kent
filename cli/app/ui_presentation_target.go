package app

import (
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	"core/shared/runtimeids"
)

type uiPresentationTarget interface {
	isUIPresentationTarget()
}

type uiNewSessionTarget struct {
	creation *chatpb.NewChatTarget
}

func (uiNewSessionTarget) isUIPresentationTarget() {}

type uiKnownSessionTarget struct {
	id runtimeids.SessionID
}

func (uiKnownSessionTarget) isUIPresentationTarget() {}

func (m *uiModel) newChatTarget() *chatpb.NewChatTarget {
	target, ok := m.presentationTarget.(uiNewSessionTarget)
	if !ok {
		return nil
	}
	return target.creation
}

func (m *uiModel) setKnownSessionTarget(id string) {
	parsed, err := runtimeids.ParseSessionID(id)
	if err != nil {
		panic(err)
	}
	m.presentationTarget = uiKnownSessionTarget{id: parsed}
}
