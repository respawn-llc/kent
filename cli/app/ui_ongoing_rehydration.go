package app

import (
	"time"

	"core/cli/tui/ongoing"
	tea "github.com/charmbracelet/bubbletea"
)

type ongoingResizeDebounceMsg struct{ token uint64 }
type ongoingPendingResize struct {
	deadline time.Time
	due      bool
}
type ongoingReopenPhase uint8

const (
	ongoingReopenRequested ongoingReopenPhase = iota
	ongoingReopenHydrating
)

type ongoingRehydrationState struct {
	token   uint64
	pending *ongoingPendingResize
	reopen  *ongoingReopenPhase
	closed  bool
}

func (s ongoingRehydrationState) resizeDue() bool {
	return s.pending != nil && (s.pending.due || !time.Now().Before(s.pending.deadline))
}

func (m *uiModel) scheduleOngoingResizeRehydration() tea.Cmd {
	if m.ongoingRehydration.closed {
		return nil
	}
	m.ongoingRehydration.token++
	token := m.ongoingRehydration.token
	m.ongoingRehydration.pending = &ongoingPendingResize{deadline: time.Now().Add(time.Second)}
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return ongoingResizeDebounceMsg{token: token} })
}

func (m *uiModel) admitOngoingRehydration(reason ongoing.RehydrateReason) {
	state := &m.ongoingRehydration
	if state.closed || state.reopen != nil || m.requestOngoingOpen == nil {
		return
	}
	if state.resizeDue() && m.nativeOngoingSurfaceActive() {
		reason = ongoing.RehydrateReasonResize
		state.pending = nil
	}
	phase := ongoingReopenRequested
	state.reopen = &phase
	m.requestOngoingOpen(reason)
}

func (m *uiModel) admitDueOngoingResize() {
	state := &m.ongoingRehydration
	if state.resizeDue() && m.nativeOngoingSurfaceActive() {
		m.admitOngoingRehydration(ongoing.RehydrateReasonResize)
	}
}

func (m *uiModel) finishOngoingRehydration() {
	if m.ongoingRehydration.reopen != nil && *m.ongoingRehydration.reopen == ongoingReopenRequested {
		return
	}
	m.ongoingRehydration.reopen = nil
	m.admitDueOngoingResize()
}

func (m *uiModel) closeOngoingRehydration() {
	m.ongoingRehydration.closed = true
	m.ongoingRehydration.reopen = nil
	m.ongoingRehydration.pending = nil
	m.ongoingRehydration.token++
}
