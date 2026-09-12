package serverapi

import (
	"context"

	attentionpb "core/shared/protoapi/gen/kent/api/attention"
)

type SessionAttentionNotificationSubscription interface {
	Next(context.Context) (*attentionpb.NotificationEvent, error)
	Close() error
}
