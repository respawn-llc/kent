package serverapi

import (
	"context"

	"core/shared/clientui"
)

type SessionActivitySubscribeRequest struct {
	SessionID     string
	AfterSequence uint64
}

type SessionActivitySubscription interface {
	Next(ctx context.Context) (clientui.Event, error)
	Close() error
}

func (r SessionActivitySubscribeRequest) Validate() error {
	return validateRequiredSessionID(r.SessionID)
}
