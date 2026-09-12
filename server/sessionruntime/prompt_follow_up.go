package sessionruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"core/shared/clientui"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

type promptFollowUpState struct {
	descriptor   *validatedQuestionBatchDescriptor
	resolved     bool
	subscription *promptFollowUpSubscription
}

type promptFollowUpKey struct {
	sessionID  runtimeids.SessionID
	stepID     runtimeids.StepID
	toolCallID clientui.ToolCallID
}

type promptFollowUpSubscription struct {
	mu       sync.Mutex
	events   chan *promptpb.FollowUpEvent
	canceled chan struct{}
	closed   bool
	onClose  func()
}

func (a *Authority) SubscribePromptFollowUp(
	_ context.Context,
	sessionID runtimeids.SessionID,
	stepID runtimeids.StepID,
	toolCallID clientui.ToolCallID,
) (serverapi.PromptFollowUpSubscription, error) {
	if a == nil {
		return nil, errors.New("session runtime authority is required")
	}
	execution := a.sessionExecution(sessionID)
	if execution == nil {
		return nil, serverapi.ErrPromptNotFound
	}
	return execution.prompts.subscribePromptFollowUp(stepID, toolCallID)
}

func (s *executionPromptStore) subscribePromptFollowUp(
	stepID runtimeids.StepID,
	toolCallID clientui.ToolCallID,
) (serverapi.PromptFollowUpSubscription, error) {
	if s == nil || s.authority == nil {
		return nil, errors.New("session runtime authority is required")
	}
	if stepID.IsZero() {
		return nil, errors.New("step id is required")
	}
	if err := toolCallID.Validate(); err != nil {
		return nil, err
	}
	key, err := s.promptFollowUpKey(stepID, toolCallID)
	if err != nil {
		return nil, err
	}
	rawToolCallID := string(key.toolCallID)
	s.mu.Lock()
	if s.promptFollowUps[key] != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf(
			"prompt follow-up subscription is already active for session %s step %s prompt %s",
			key.sessionID,
			key.stepID,
			key.toolCallID,
		)
	}
	entry := s.pending[rawToolCallID]
	if entry == nil || entry.snapshot.Request.StepID != key.stepID.String() {
		s.mu.Unlock()
		return nil, serverapi.ErrPromptNotFound
	}
	subscription := s.registerPromptFollowUpLocked(key)
	s.mu.Unlock()
	return subscription, nil
}

func (s *executionPromptStore) promptFollowUpKey(
	stepID runtimeids.StepID,
	toolCallID clientui.ToolCallID,
) (promptFollowUpKey, error) {
	resource, ok := s.scope.Resource()
	if !ok {
		return promptFollowUpKey{}, errors.New("prompt follow-up requires an agent Session scope")
	}
	return promptFollowUpKey{
		sessionID:  resource.SessionID(),
		stepID:     stepID,
		toolCallID: toolCallID,
	}, nil
}

func (s *executionPromptStore) registerPromptFollowUpLocked(
	key promptFollowUpKey,
) *promptFollowUpSubscription {
	if s.promptFollowUps == nil {
		s.promptFollowUps = make(map[promptFollowUpKey]*promptFollowUpState)
	}
	subscription := &promptFollowUpSubscription{
		events:   make(chan *promptpb.FollowUpEvent, 1),
		canceled: make(chan struct{}),
	}
	state := &promptFollowUpState{subscription: subscription}
	subscription.onClose = func() {
		s.mu.Lock()
		if s.promptFollowUps[key] == state {
			delete(s.promptFollowUps, key)
		}
		s.mu.Unlock()
	}
	s.promptFollowUps[key] = state
	return subscription
}

func (s *executionPromptStore) resolvePromptFollowUpLocked(
	stepID runtimeids.StepID,
	toolCallID clientui.ToolCallID,
	descriptor *validatedQuestionBatchDescriptor,
) {
	if len(s.promptFollowUps) == 0 {
		return
	}
	key, err := s.promptFollowUpKey(stepID, toolCallID)
	if err != nil {
		return
	}
	state := s.promptFollowUps[key]
	if state == nil {
		return
	}
	state.descriptor = descriptor
	state.resolved = true
	if descriptor == nil {
		s.emitPromptFollowUpLocked(key, promptpb.FollowUpKind_FOLLOW_UP_KIND_NO_PREPARED_SUCCESSOR)
		return
	}
	successorIDs := descriptor.successorToolCallIDs()
	if len(successorIDs) == 0 {
		s.emitPromptFollowUpLocked(key, promptpb.FollowUpKind_FOLLOW_UP_KIND_NO_PREPARED_SUCCESSOR)
		return
	}
	for _, successorID := range successorIDs {
		if _, pending := s.pending[successorID]; pending {
			s.emitPromptFollowUpLocked(key, promptpb.FollowUpKind_FOLLOW_UP_KIND_SUCCESSOR_READY)
			return
		}
	}
}

func (s *executionPromptStore) observePromptFollowUpsLocked(rawStepID string, successorID string) {
	stepID, err := runtimeids.ParseStepID(rawStepID)
	if err != nil {
		return
	}
	keys := make([]promptFollowUpKey, 0)
	for key, state := range s.promptFollowUps {
		if !state.resolved || key.stepID != stepID {
			continue
		}
		for _, expectedID := range state.descriptor.successorToolCallIDs() {
			if expectedID == successorID {
				keys = append(keys, key)
				break
			}
		}
	}
	for _, key := range keys {
		s.emitPromptFollowUpLocked(key, promptpb.FollowUpKind_FOLLOW_UP_KIND_SUCCESSOR_READY)
	}
}

func (s *executionPromptStore) closePromptFollowUpsLocked() {
	keys := make([]promptFollowUpKey, 0, len(s.promptFollowUps))
	for key := range s.promptFollowUps {
		keys = append(keys, key)
	}
	for _, key := range keys {
		s.emitPromptFollowUpLocked(key, promptpb.FollowUpKind_FOLLOW_UP_KIND_EXECUTION_CLOSED)
	}
}

func (s *executionPromptStore) emitPromptFollowUpLocked(
	key promptFollowUpKey,
	kind promptpb.FollowUpKind,
) {
	state := s.promptFollowUps[key]
	if state == nil {
		return
	}
	delete(s.promptFollowUps, key)
	event := &promptpb.FollowUpEvent{Kind: kind}
	state.subscription.publish(event)
}

func (s *promptFollowUpSubscription) publish(event *promptpb.FollowUpEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.events <- event
	close(s.events)
}

func (s *promptFollowUpSubscription) Next(ctx context.Context) (*promptpb.FollowUpEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case event, ok := <-s.events:
		if !ok {
			return nil, io.EOF
		}
		return event, nil
	case <-s.canceled:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
}

func (s *promptFollowUpSubscription) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.canceled)
	onClose := s.onClose
	s.mu.Unlock()
	if onClose != nil {
		onClose()
	}
	return nil
}
