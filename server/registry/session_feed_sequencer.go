package registry

import (
	"errors"
	"fmt"
	"sync"

	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
)

type sessionFeedSequencer struct {
	mu     sync.Mutex
	broker *transcriptSubscriptionBroker
}

type transcriptHydrationProjectionError struct {
	cause error
}

func (e transcriptHydrationProjectionError) Error() string {
	if e.cause == nil {
		return "transcript hydration projection failed"
	}
	return e.cause.Error()
}

func (e transcriptHydrationProjectionError) Unwrap() error {
	return e.cause
}

func newSessionFeedSequencer(broker *transcriptSubscriptionBroker) *sessionFeedSequencer {
	return &sessionFeedSequencer{broker: broker}
}

func (s *sessionFeedSequencer) HasSubscribers() bool {
	return s != nil && s.broker != nil && s.broker.SubscriberCount() > 0
}

func (s *sessionFeedSequencer) Subscribe(
	build func() (*transcriptpb.Hydration, error),
) (*transcriptSubscription, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if build == nil {
		return nil, fmt.Errorf("transcript hydration builder is required")
	}
	hydration, err := build()
	if err != nil {
		var projectionErr transcriptHydrationProjectionError
		if errors.As(err, &projectionErr) {
			return nil, transcriptProjectionContractError(projectionErr)
		}
		return nil, err
	}
	event := &transcriptpb.Event{Payload: &transcriptpb.Event_Hydration{Hydration: hydration}}
	if err := protoapi.Validate(event); err != nil {
		return nil, fmt.Errorf("build canonical transcript hydration: %w", err)
	}
	return s.broker.Subscribe(event)
}

func (s *sessionFeedSequencer) Publish(events []*transcriptpb.Event) {
	if s == nil || len(events) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.validateEvents(events)
	s.broker.Publish(events)
}

func (s *sessionFeedSequencer) PublishBuilt(build func() ([]*transcriptpb.Event, error)) error {
	if s == nil {
		return nil
	}
	if build == nil {
		return fmt.Errorf("transcript event builder is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	events, err := build()
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}
	s.validateEvents(events)
	s.broker.Publish(events)
	return nil
}

func (s *sessionFeedSequencer) PublishRuntimeReadModel(update *runtimepb.ReadModelUpdate) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.publishRuntimeReadModelLocked(update)
}

func (s *sessionFeedSequencer) CloseWithRuntimeReadModel(update *runtimepb.ReadModelUpdate, err error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.publishRuntimeReadModelLocked(update)
	s.broker.Close(err)
}

func (s *sessionFeedSequencer) Close(err error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broker.Close(err)
}

func (s *sessionFeedSequencer) CloseContractViolation(err error) error {
	if err == nil {
		return nil
	}
	if s == nil {
		return transcriptProjectionContractError(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	contractErr := transcriptProjectionContractError(err)
	s.broker.CloseSubscribers(contractErr)
	return contractErr
}

func (s *sessionFeedSequencer) publishRuntimeReadModelLocked(update *runtimepb.ReadModelUpdate) {
	event := &transcriptpb.Event{Payload: &transcriptpb.Event_RuntimeReadModelUpdate{RuntimeReadModelUpdate: update}}
	if err := protoapi.Validate(event); err != nil {
		panic(fmt.Sprintf("publish invalid canonical runtime read-model update: %+v: %v", update, err))
	}
	s.broker.Publish([]*transcriptpb.Event{event})
}

func (s *sessionFeedSequencer) validateEvents(events []*transcriptpb.Event) {
	for _, event := range events {
		if err := protoapi.Validate(event); err != nil {
			panic(fmt.Sprintf("publish invalid canonical transcript event before batch mutation: %v", err))
		}
	}
}
