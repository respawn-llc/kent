package runtime

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"builder/server/llm"
)

type reviewerRuntimeState struct {
	mu              sync.Mutex
	client          llm.Client
	resumeFrequency string
}

func newReviewerRuntimeState(client llm.Client) *reviewerRuntimeState {
	return &reviewerRuntimeState{client: client}
}

func (s *reviewerRuntimeState) Client() llm.Client {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client
}

func (s *reviewerRuntimeState) EnsureClient(factory func() (llm.Client, error)) error {
	if s == nil {
		return errors.New("reviewer state is not configured")
	}
	s.mu.Lock()
	if s.client != nil {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	if factory == nil {
		return errors.New("reviewer client is not configured")
	}
	client, err := factory()
	if err != nil {
		return fmt.Errorf("configure reviewer client: %w", err)
	}
	s.mu.Lock()
	if s.client == nil {
		s.client = client
	}
	s.mu.Unlock()
	return nil
}

func (s *reviewerRuntimeState) RecordResumeFrequency(frequency string) {
	if s == nil {
		return
	}
	normalized, ok := NormalizeReviewerFrequency(frequency)
	if !ok || normalized == "off" {
		return
	}
	s.mu.Lock()
	s.resumeFrequency = normalized
	s.mu.Unlock()
}

func (s *reviewerRuntimeState) SetResumeFrequency(frequency string) {
	if s == nil {
		return
	}
	normalized, ok := NormalizeReviewerFrequency(frequency)
	if !ok || normalized == "" || normalized == "off" {
		normalized = "edits"
	}
	s.mu.Lock()
	s.resumeFrequency = normalized
	s.mu.Unlock()
}

func (s *reviewerRuntimeState) ResumeFrequency(defaultFrequency string) string {
	if s == nil {
		return strings.TrimSpace(defaultFrequency)
	}
	s.mu.Lock()
	resume := s.resumeFrequency
	s.mu.Unlock()
	if normalized, ok := NormalizeReviewerFrequency(resume); ok && normalized != "" && normalized != "off" {
		return normalized
	}
	if normalized, ok := NormalizeReviewerFrequency(defaultFrequency); ok && normalized != "" && normalized != "off" {
		return normalized
	}
	return "edits"
}
