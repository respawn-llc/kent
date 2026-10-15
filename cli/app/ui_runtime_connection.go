package app

import (
	"core/cli/app/internal/runtimeattach"
	"core/shared/clientui"
)

const runtimeDisconnectedStatusMessage = "server connection lost"

func (m *uiModel) bindRuntimeConnectionObserver() {
	if configurable, ok := m.engine.(interface{ SetConnectionStateObserver(func(error)) }); ok {
		events := make(chan runtimeConnectionStateChangedMsg, 1)
		m.runtimeConnectionEvents = events
		configurable.SetConnectionStateObserver(func(err error) {
			enqueueRuntimeConnectionStateChange(events, err)
		})
	}
}

func (m *uiModel) observeRuntimeRequestResult(err error) {
	if m == nil || !m.hasRuntimeClient() {
		return
	}
	if err == nil {
		m.setRuntimeDisconnected(false)
		return
	}
	if runtimeattach.IsRuntimeConnectionError(err) || runtimeattach.IsRuntimeProtocolMismatch(err) {
		m.setRuntimeDisconnected(true)
		return
	}
	if runtimeattach.ConfirmsRuntimeReachability(err) {
		m.setRuntimeDisconnected(false)
	}
}

func (m *uiModel) runtimeDisconnectStatusVisible() bool {
	return m != nil && m.hasRuntimeClient() && m.runtimeDisconnectedState()
}

func (m *uiModel) runtimeDisconnectStatusText() string {
	if !m.runtimeDisconnectStatusVisible() {
		return ""
	}
	return runtimeDisconnectedStatusMessage
}

func enqueueRuntimeConnectionStateChange(ch chan runtimeConnectionStateChangedMsg, err error) {
	if ch == nil {
		return
	}
	coalesceLatest(ch, runtimeConnectionStateChangedMsg{err: err})
}

func coalesceLatest[T any](ch chan T, msg T) {
	select {
	case ch <- msg:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- msg:
	default:
	}
}

func (m *uiModel) setRuntimeDisconnected(disconnected bool) {
	if m == nil {
		return
	}
	m.runtimeConnection = clientui.NewRuntimeConnectionLifecycle(disconnected)
}

func (m *uiModel) runtimeDisconnectedState() bool {
	if m == nil {
		return false
	}
	return m.runtimeConnection.IsDisconnected()
}
