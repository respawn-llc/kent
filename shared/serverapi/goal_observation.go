package serverapi

import (
	"context"

	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
)

type GoalObservationSubscription interface {
	Next(context.Context) (*runtimepb.GoalObservation, error)
	Close() error
}
