package sleepguard

import (
	"log"
	"sync"

	"builder/shared/config"
)

// Manager tracks active agent sessions and acquires/releases the Guard accordingly.
type Manager struct {
	mode    config.SleepPreventionMode
	guard   Guard
	mu      sync.Mutex
	active  map[string]bool
	onError func(sessionID string, err error)
}

// NewManager creates a Manager. onError is called whenever guard acquisition fails;
// sessionID is empty for "always" mode startup failures (no session context yet).
// Returns a non-nil error when mode is "always" and the guard cannot be acquired at
// startup — the manager is still returned and usable.
func NewManager(mode config.SleepPreventionMode, onError func(sessionID string, err error)) (*Manager, error) {
	m := &Manager{
		mode:    mode,
		active:  make(map[string]bool),
		onError: onError,
	}
	if mode == config.SleepPreventionModeAlways {
		if err := m.guard.Acquire(); err != nil {
			log.Printf("sleepguard: always-mode acquire failed: %v", err)
			if onError != nil {
				onError("", err)
			}
			return m, err
		}
	}
	return m, nil
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
			if err := m.guard.Acquire(); err != nil {
				log.Printf("sleepguard: active-mode acquire failed: %v", err)
				if m.onError != nil {
					m.onError(sessionID, err)
				}
			}
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
