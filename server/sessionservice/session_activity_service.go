package sessionservice

import (
	"context"
	"errors"

	servicecontract "core/shared/apicontract"
	"core/shared/serverapi"
)

type SessionActivitySubscriber interface {
	SubscribeSessionActivity(ctx context.Context, sessionID string) (serverapi.SessionActivitySubscription, error)
}

type SessionActivityCursorSubscriber interface {
	SubscribeSessionActivityFrom(ctx context.Context, req serverapi.SessionActivitySubscribeRequest) (serverapi.SessionActivitySubscription, error)
}

type SessionActivityService struct {
	subscriber SessionActivitySubscriber
}

func NewSessionActivityService(subscriber SessionActivitySubscriber) *SessionActivityService {
	return &SessionActivityService{subscriber: subscriber}
}

func (s *SessionActivityService) SubscribeSessionActivity(ctx context.Context, req serverapi.SessionActivitySubscribeRequest) (serverapi.SessionActivitySubscription, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if s == nil || s.subscriber == nil {
		return nil, errors.New("session activity subscriber is required")
	}
	if subscriber, ok := s.subscriber.(SessionActivityCursorSubscriber); ok {
		return subscriber.SubscribeSessionActivityFrom(ctx, req)
	}
	if req.AfterSequence > 0 {
		return nil, serverapi.ErrStreamGap
	}
	return s.subscriber.SubscribeSessionActivity(ctx, req.SessionID)
}

var _ servicecontract.SessionActivityService = (*SessionActivityService)(nil)
