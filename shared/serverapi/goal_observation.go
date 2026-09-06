package serverapi

import (
	"context"

	"core/shared/clientui"
)

type GoalObserveRequest struct {
	SessionID string `json:"session_id"`
}

func (r GoalObserveRequest) Validate() error {
	return validateRequiredSessionID(r.SessionID)
}

type GoalObservationSubscription interface {
	Next(context.Context) (clientui.GoalObservation, error)
	Close() error
}
