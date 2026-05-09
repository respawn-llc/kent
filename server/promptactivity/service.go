package promptactivity

import (
	"context"
	"errors"

	"builder/shared/serverapi"
)

type Subscriber interface {
	SubscribePromptActivityFrom(ctx context.Context, req serverapi.PromptActivitySubscribeRequest) (serverapi.PromptActivitySubscription, error)
}

type Service struct {
	subscriber Subscriber
}

func NewService(subscriber Subscriber) *Service {
	return &Service{subscriber: subscriber}
}

func (s *Service) SubscribePromptActivity(ctx context.Context, req serverapi.PromptActivitySubscribeRequest) (serverapi.PromptActivitySubscription, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if s == nil || s.subscriber == nil {
		return nil, errors.New("prompt activity subscriber is required")
	}
	return s.subscriber.SubscribePromptActivityFrom(ctx, req)
}

var _ serverapi.PromptActivityService = (*Service)(nil)
