package runtime

import (
	"core/shared/runtimeids"
	"sync"
)

type pendingToolCallStartStore struct {
	mu     sync.Mutex
	starts map[runtimeids.ToolCallID]int
}

func newPendingToolCallStartStore() *pendingToolCallStartStore {
	return &pendingToolCallStartStore{starts: make(map[runtimeids.ToolCallID]int)}
}

func (s *pendingToolCallStartStore) Remember(starts map[runtimeids.ToolCallID]int) {
	if s == nil || len(starts) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.starts == nil {
		s.starts = make(map[runtimeids.ToolCallID]int, len(starts))
	}
	for callID, start := range starts {
		if callID == "" {
			continue
		}
		s.starts[callID] = start
	}
}

func (s *pendingToolCallStartStore) Lookup(callID runtimeids.ToolCallID) (int, bool) {
	if s == nil || callID == "" {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	start, ok := s.starts[callID]
	return start, ok
}

func (s *pendingToolCallStartStore) Forget(callID runtimeids.ToolCallID) {
	if s == nil || callID == "" {
		return
	}
	s.mu.Lock()
	delete(s.starts, callID)
	s.mu.Unlock()
}

func (s *pendingToolCallStartStore) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.starts)
}
