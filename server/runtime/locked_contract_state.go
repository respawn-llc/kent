package runtime

import (
	"strings"
	"sync"

	"core/server/session"
)

type lockedContractState struct {
	mu     sync.Mutex
	locked *session.LockedContract
}

func newLockedContractState() *lockedContractState {
	return &lockedContractState{}
}

func (s *lockedContractState) Snapshot() (session.LockedContract, bool) {
	if s == nil {
		return session.LockedContract{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked == nil {
		return session.LockedContract{}, false
	}
	return *s.locked, true
}

func (s *lockedContractState) Set(locked session.LockedContract) {
	if s == nil {
		return
	}
	copyLocked := locked
	s.mu.Lock()
	s.locked = &copyLocked
	s.mu.Unlock()
}

func (s *lockedContractState) Model() string {
	locked, ok := s.Snapshot()
	if !ok {
		return ""
	}
	return strings.TrimSpace(locked.Model)
}

func (s *lockedContractState) MaxOutputToken() int {
	locked, ok := s.Snapshot()
	if !ok || locked.MaxOutputToken <= 0 {
		return 0
	}
	return locked.MaxOutputToken
}

func (s *lockedContractState) FillSystemPrompt(prompt string) {
	if s == nil {
		return
	}
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return
	}
	s.mu.Lock()
	if s.locked != nil && !s.locked.HasSystemPrompt {
		s.locked.SystemPrompt = trimmed
		s.locked.HasSystemPrompt = true
	}
	s.mu.Unlock()
}

func (s *lockedContractState) FillReviewerPrompt(prompt string) {
	if s == nil {
		return
	}
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return
	}
	s.mu.Lock()
	if s.locked != nil && !s.locked.HasReviewerPrompt {
		s.locked.ReviewerPrompt = trimmed
		s.locked.HasReviewerPrompt = true
	}
	s.mu.Unlock()
}

func (s *lockedContractState) ReviewerPromptSnapshot() (string, bool) {
	locked, ok := s.Snapshot()
	if !ok {
		return "", false
	}
	if locked.HasReviewerPrompt {
		return strings.TrimSpace(locked.ReviewerPrompt), true
	}
	if prompt := strings.TrimSpace(locked.ReviewerPrompt); prompt != "" {
		return prompt, true
	}
	return "", false
}
