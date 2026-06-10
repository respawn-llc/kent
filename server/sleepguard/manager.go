package sleepguard

import (
	"sync"

	"builder/shared/config"
)

// Manager tracks active agent sessions and acquires/releases the Guard accordingly.
type Manager struct {
	mode   config.SleepPreventionMode
	guard  Guard
	mu     sync.Mutex
	active map[string]bool
}

func NewManager(mode config.SleepPreventionMode) *Manager {
	m := &Manager{
		mode:   mode,
		active: make(map[string]bool),
	}
	if mode == config.SleepPreventionModeAlways {
		m.guard.Acquire()
	}
	return m
}

// OnRunStateChanged is called by the registry whenever a session's run state changes.
// Only acts when mode is "active"; "always" is handled at construction/Close time.
func (m *Manager) OnRunStateChanged(sessionID string, running bool) {
	if m == nil || m.mode != config.SleepPreventionModeActive {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if running {
		wasEmpty := len(m.active) == 0
		m.active[sessionID] = true
		if wasEmpty {
			m.guard.Acquire()
		}
	} else {
		if !m.active[sessionID] {
			return
		}
		delete(m.active, sessionID)
		if len(m.active) == 0 {
			m.guard.Release()
		}
	}
}

func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.guard.Release()
}
