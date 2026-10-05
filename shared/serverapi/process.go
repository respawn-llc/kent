package serverapi

import (
	"context"

	processpb "core/shared/protoapi/gen/kent/api/process"
)

type ProcessObservationSubscription interface {
	Next(context.Context) (*processpb.ListSuccess, error)
	Close() error
}
