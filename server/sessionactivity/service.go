package sessionactivity

import (
	"context"
	"errors"

	"core/shared/serverapi"
	"core/shared/servicecontract"
)

type Subscriber interface {
	SubscribeSessionActivity(ctx context.Context, sessionID string) (serverapi.SessionActivitySubscription, error)
}

type CursorSubscriber interface {
	SubscribeSessionActivityFrom(ctx context.Context, req serverapi.SessionActivitySubscribeRequest) (serverapi.SessionActivitySubscription, error)
}

type Service struct {
	subscriber Subscriber
}

func NewService(subscriber Subscriber) *Service {
	return &Service{subscriber: subscriber}
}

func (s *Service) SubscribeSessionActivity(ctx context.Context, req serverapi.SessionActivitySubscribeRequest) (serverapi.SessionActivitySubscription, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if s == nil || s.subscriber == nil {
		return nil, errors.New("session activity subscriber is required")
	}
	if subscriber, ok := s.subscriber.(CursorSubscriber); ok {
		return subscriber.SubscribeSessionActivityFrom(ctx, req)
	}
	if req.AfterSequence > 0 {
		return nil, serverapi.ErrStreamGap
	}
	return s.subscriber.SubscribeSessionActivity(ctx, req.SessionID)
}

var _ servicecontract.SessionActivityService = (*Service)(nil)
