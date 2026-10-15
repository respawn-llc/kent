package runtime

import (
	"strings"
	"sync"
)

type handoffRuntimeState struct {
	mu            sync.Mutex
	request       *handoffRequest
	futureMessage string
}

func newHandoffRuntimeState() *handoffRuntimeState {
	return &handoffRuntimeState{}
}

func (s *handoffRuntimeState) QueueRequest(summarizerPrompt string, futureAgentMessage string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.request = &handoffRequest{
		summarizerPrompt:   strings.TrimSpace(summarizerPrompt),
		futureAgentMessage: strings.TrimSpace(futureAgentMessage),
	}
	s.mu.Unlock()
}

func (s *handoffRuntimeState) ClearRequest() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.request = nil
	s.mu.Unlock()
}

func (s *handoffRuntimeState) RequestSnapshot() *handoffRequest {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.request == nil {
		return nil
	}
	req := *s.request
	return &req
}

func (s *handoffRuntimeState) QueueFutureMessage(message string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.futureMessage = strings.TrimSpace(message)
	s.mu.Unlock()
}

func (s *handoffRuntimeState) ClearFutureMessage() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.futureMessage = ""
	s.mu.Unlock()
}

func (s *handoffRuntimeState) FutureMessageSnapshot() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.futureMessage)
}
