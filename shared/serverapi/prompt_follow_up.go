package serverapi

import (
	"context"

	promptpb "core/shared/protoapi/gen/kent/api/prompt"
)

type PromptFollowUpSubscription interface {
	Next(context.Context) (*promptpb.FollowUpEvent, error)
	Close() error
}
