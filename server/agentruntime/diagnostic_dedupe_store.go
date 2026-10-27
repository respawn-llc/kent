package runtime

import (
	"strings"
	"sync"
)

type diagnosticDedupeStore struct {
	mu        sync.Mutex
	local     map[string]struct{}
	persisted map[string]struct{}
}

func newDiagnosticDedupeStore() *diagnosticDedupeStore {
	return &diagnosticDedupeStore{
		local:     make(map[string]struct{}),
		persisted: make(map[string]struct{}),
	}
}

func (s *diagnosticDedupeStore) BeginLocal(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.local[key]; exists {
		return false
	}
	s.local[key] = struct{}{}
	return true
}

func (s *diagnosticDedupeStore) ClearLocal(key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	s.mu.Lock()
	delete(s.local, key)
	delete(s.persisted, key)
	s.mu.Unlock()
}

func (s *diagnosticDedupeStore) RestoreLocal(key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	s.mu.Lock()
	s.local[key] = struct{}{}
	s.persisted[key] = struct{}{}
	s.mu.Unlock()
}

func (s *diagnosticDedupeStore) HasPersisted(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, exists := s.persisted[key]
	return exists
}

func (s *diagnosticDedupeStore) Reset() {
	s.mu.Lock()
	s.local = make(map[string]struct{})
	s.persisted = make(map[string]struct{})
	s.mu.Unlock()
}
