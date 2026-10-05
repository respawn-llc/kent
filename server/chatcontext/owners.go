package chatcontext

import (
	"context"

	contextpb "core/shared/protoapi/gen/kent/api/chat_context"
	"core/shared/runtimeids"
)

type SessionOwner interface {
	ReadSessionChatContext(context.Context, runtimeids.SessionID) (*contextpb.Context, error)
}
