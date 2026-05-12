package runtime

import "sync"

type goalLoopState struct {
	mu        sync.Mutex
	lifecycle goalLoopLifecycleState
}

func newGoalLoopState() *goalLoopState {
	return &goalLoopState{}
}

func (s *goalLoopState) Suspend() {
	if s == nil {
		return
	}
	s.mu.Lock()
	switch s.lifecycle {
	case goalLoopLifecycleRunning, goalLoopLifecycleRestartPending:
		s.lifecycle = goalLoopLifecycleSuspending
	default:
		s.lifecycle = goalLoopLifecycleSuspended
	}
	s.mu.Unlock()
}

func (s *goalLoopState) Resume() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.lifecycle.IsSuspended() {
		s.lifecycle = goalLoopLifecycleIdle
	}
	s.mu.Unlock()
}

func (s *goalLoopState) Start() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.lifecycle {
	case goalLoopLifecycleRunning, goalLoopLifecycleRestartPending:
		return false
	case goalLoopLifecycleSuspending:
		s.lifecycle = goalLoopLifecycleRestartPending
		return false
	}
	s.lifecycle = goalLoopLifecycleRunning
	return true
}

func (s *goalLoopState) Finish(active bool) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.lifecycle {
	case goalLoopLifecycleRestartPending:
		if active {
			s.lifecycle = goalLoopLifecycleRunning
			return true
		}
		s.lifecycle = goalLoopLifecycleIdle
	case goalLoopLifecycleSuspending:
		s.lifecycle = goalLoopLifecycleSuspended
	case goalLoopLifecycleRunning:
		s.lifecycle = goalLoopLifecycleIdle
	}
	return false
}

func (s *goalLoopState) Suspended() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lifecycle.IsSuspended()
}

func (s *goalLoopState) Running() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lifecycle.IsRunning()
}
