package serverapi

import (
	"context"

	sessionpb "core/shared/protoapi/gen/kent/api/session"
)

type QuestionHistorySubscription interface {
	Next(context.Context) (*sessionpb.QuestionHistoryEvent, error)
	Close() error
}
