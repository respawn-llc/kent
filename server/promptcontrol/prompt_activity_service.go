package promptcontrol

import (
	"context"
	"errors"

	servicecontract "core/shared/apicontract"
	"core/shared/serverapi"
)

type PromptActivitySubscriber interface {
	SubscribePromptActivityFrom(ctx context.Context, req serverapi.PromptActivitySubscribeRequest) (serverapi.PromptActivitySubscription, error)
}

type PromptActivityService struct {
	subscriber PromptActivitySubscriber
}

func NewPromptActivityService(subscriber PromptActivitySubscriber) *PromptActivityService {
	return &PromptActivityService{subscriber: subscriber}
}

func (s *PromptActivityService) SubscribePromptActivity(ctx context.Context, req serverapi.PromptActivitySubscribeRequest) (serverapi.PromptActivitySubscription, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if s == nil || s.subscriber == nil {
		return nil, errors.New("prompt activity subscriber is required")
	}
	return s.subscriber.SubscribePromptActivityFrom(ctx, req)
}

var _ servicecontract.PromptActivityService = (*PromptActivityService)(nil)
