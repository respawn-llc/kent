package client

import (
	"context"

	contextpb "core/shared/protoapi/gen/kent/api/chat_context"
)

func (c *Remote) GetChatContext(ctx context.Context, request *contextpb.GetRequest) (*contextpb.GetSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(contextpb.File_kent_api_chat_context_chat_context_proto, "ChatContextService", "Get"),
		request, &contextpb.GetResult{}, func(failure *contextpb.GetError) error {
			return projectInternalGeneratedError(failure.Code, failure.GetInternalFailure())
		})
}
