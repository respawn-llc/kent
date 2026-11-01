package runtime

import "sync"

type modelRequestRuntimeState struct {
	mu           sync.Mutex
	tokenUsage   *tokenUsageTracker
	requestCache *requestCacheTracker
}

func newModelRequestRuntimeState() *modelRequestRuntimeState {
	return &modelRequestRuntimeState{
		tokenUsage:   newTokenUsageTracker(),
		requestCache: newRequestCacheTracker(),
	}
}

func (s *modelRequestRuntimeState) TokenUsage() *tokenUsageTracker {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokenUsage == nil {
		s.tokenUsage = newTokenUsageTracker()
	}
	return s.tokenUsage
}

func (s *modelRequestRuntimeState) RequestCache() *requestCacheTracker {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.requestCache == nil {
		s.requestCache = newRequestCacheTracker()
	}
	return s.requestCache
}
