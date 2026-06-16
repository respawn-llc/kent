package runtime

import (
	"core/server/llm"
	"strings"
	"sync"

	"github.com/google/uuid"
)

type queuedUserMessageStore struct {
	mu      sync.Mutex
	pending []queuedUserSteeringIntent
}

type queuedUserSteeringIntent struct {
	message QueuedUserMessage
	intent  steeringIntent
}

func newQueuedUserMessageStore() *queuedUserMessageStore {
	return &queuedUserMessageStore{}
}

func (s *queuedUserMessageStore) Queue(text string) QueuedUserMessage {
	item := QueuedUserMessage{ID: uuid.NewString(), Text: text}
	return s.QueueItem(item)
}

func (s *queuedUserMessageStore) QueueItem(item QueuedUserMessage) QueuedUserMessage {
	item.ID = strings.TrimSpace(item.ID)
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	intent := steerUserMessageWithoutDerivedEventIntent(llm.Message{Role: llm.RoleUser, Content: item.Text})
	s.mu.Lock()
	s.pending = append(s.pending, queuedUserSteeringIntent{message: item, intent: intent})
	s.mu.Unlock()
	return item
}

func (s *queuedUserMessageStore) EnsurePending(item QueuedUserMessage) QueuedUserMessage {
	item.ID = strings.TrimSpace(item.ID)
	if item.ID == "" || s == nil {
		return QueuedUserMessage{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, pending := range s.pending {
		if pending.message.ID == item.ID {
			return pending.message
		}
	}
	intent := steerUserMessageWithoutDerivedEventIntent(llm.Message{Role: llm.RoleUser, Content: item.Text})
	s.pending = append(s.pending, queuedUserSteeringIntent{message: item, intent: intent})
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
		if pending.message.ID == id {
			removed = true
			continue
		}
		filtered = append(filtered, pending)
	}
	s.pending = filtered
	return removed
}

func (s *queuedUserMessageStore) Drain() []queuedUserSteeringIntent {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	pending := append([]queuedUserSteeringIntent(nil), s.pending...)
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
	out := make([]QueuedUserMessage, 0, len(s.pending))
	for _, pending := range s.pending {
		out = append(out, pending.message)
	}
	return out
}
