package serverapi

import (
	"context"

	"builder/shared/clientui"
)

type PromptActivitySubscribeRequest struct {
	SessionID     string
	AfterSequence uint64
}

type PromptActivitySubscription interface {
	Next(ctx context.Context) (clientui.PendingPromptEvent, error)
	Close() error
}

type PromptActivityService interface {
	SubscribePromptActivity(ctx context.Context, req PromptActivitySubscribeRequest) (PromptActivitySubscription, error)
}

func (r PromptActivitySubscribeRequest) Validate() error {
	return validateRequiredSessionID(r.SessionID)
}
