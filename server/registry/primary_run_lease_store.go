package registry

import (
	"strings"
	"sync"

	"core/server/primaryrun"
)

type primaryRunLeaseStore struct {
	mu        sync.Mutex
	leases    map[string]uint64
	nextLease uint64
}

func newPrimaryRunLeaseStore() *primaryRunLeaseStore {
	return &primaryRunLeaseStore{leases: make(map[string]uint64)}
}

func (s *primaryRunLeaseStore) Acquire(sessionID string) (primaryrun.Lease, error) {
	if s == nil {
		return nil, primaryrun.ErrActivePrimaryRun
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return nil, primaryrun.ErrActivePrimaryRun
	}
	s.mu.Lock()
	if _, busy := s.leases[id]; busy {
		s.mu.Unlock()
		return nil, primaryrun.ErrActivePrimaryRun
	}
	s.nextLease++
	leaseID := s.nextLease
	s.leases[id] = leaseID
	s.mu.Unlock()
	return primaryrun.LeaseFunc(func() {
		s.release(id, leaseID)
	}), nil
}

func (s *primaryRunLeaseStore) Clear(sessionID string) {
	if s == nil {
		return
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return
	}
	s.mu.Lock()
	delete(s.leases, id)
	s.mu.Unlock()
}

func (s *primaryRunLeaseStore) release(sessionID string, leaseID uint64) {
	if s == nil {
		return
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return
	}
	s.mu.Lock()
	if current, ok := s.leases[id]; ok && current == leaseID {
		delete(s.leases, id)
	}
	s.mu.Unlock()
}
