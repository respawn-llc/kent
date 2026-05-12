package runtime

import (
	"strings"
	"sync"

	"github.com/google/uuid"
)

type queuedUserMessageStore struct {
	mu      sync.Mutex
	pending []QueuedUserMessage
}

func newQueuedUserMessageStore() *queuedUserMessageStore {
	return &queuedUserMessageStore{}
}

func (s *queuedUserMessageStore) Queue(text string) QueuedUserMessage {
	item := QueuedUserMessage{ID: uuid.NewString(), Text: text}
	s.mu.Lock()
	s.pending = append(s.pending, item)
	s.mu.Unlock()
	return item
}

func (s *queuedUserMessageStore) Discard(queueItemID string) bool {
	id := strings.TrimSpace(queueItemID)
	if id == "" || s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := s.pending[:0]
	removed := false
	for _, pending := range s.pending {
		if pending.ID == id {
			removed = true
			continue
		}
		filtered = append(filtered, pending)
	}
	s.pending = filtered
	return removed
}

func (s *queuedUserMessageStore) Drain() []QueuedUserMessage {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	pending := append([]QueuedUserMessage(nil), s.pending...)
	s.pending = nil
	s.mu.Unlock()
	return pending
}

func (s *queuedUserMessageStore) HasPending() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending) > 0
}

func (s *queuedUserMessageStore) Snapshot() []QueuedUserMessage {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]QueuedUserMessage(nil), s.pending...)
}
