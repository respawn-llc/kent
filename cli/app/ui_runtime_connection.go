package app

import (
	"strings"

	"core/cli/app/internal/runtimeconn"
	"core/shared/clientui"
)

const runtimeDisconnectedStatusMessage = "server disconnected"

func (m *uiModel) observeRuntimeRequestResult(err error) {
	if m == nil || !m.hasRuntimeClient() {
		return
	}
	if err == nil {
		m.setRuntimeDisconnected(false)
		return
	}
	if runtimeconn.IsConnectionError(err) {
		m.setRuntimeDisconnected(true)
		return
	}
	if runtimeconn.ConfirmsReachability(err) {
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
	msg := runtimeConnectionStateChangedMsg{err: err}
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

func enqueueRuntimeLeaseRecoveryWarning(ch chan runtimeLeaseRecoveryWarningMsg, text string, visibility clientui.EntryVisibility) {
	if ch == nil || strings.TrimSpace(text) == "" {
		return
	}
	msg := runtimeLeaseRecoveryWarningMsg{text: strings.TrimSpace(text), visibility: visibility}
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
	if disconnected {
		m.clearDeferredCommittedTail("disconnect")
	}
}

func (m *uiModel) runtimeDisconnectedState() bool {
	if m == nil {
		return false
	}
	return m.runtimeConnection.IsDisconnected()
}
