package runtime

import (
	"strings"
	"sync"
)

type handoffRuntimeState struct {
	mu      sync.Mutex
	request *handoffRequest
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
