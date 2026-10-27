package runtime

import "sync"

type phaseProtocolState struct {
	mu       sync.Mutex
	resolved bool
	enabled  bool
}

func newPhaseProtocolState() *phaseProtocolState {
	return &phaseProtocolState{}
}

func (s *phaseProtocolState) Snapshot() (bool, bool) {
	if s == nil {
		return false, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled, s.resolved
}

func (s *phaseProtocolState) Resolve(enabled bool) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.resolved {
		s.resolved = true
		s.enabled = enabled
	}
	return s.enabled
}
