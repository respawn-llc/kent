package client

import (
	"context"

	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
)

func (c *Remote) Steer(ctx context.Context, request *chatpb.SteerRequest) (*chatpb.InputMutationSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(chatpb.File_kent_api_chat_chat_proto, "ChatService", "Steer"),
		request, &chatpb.SteerResult{}, protoapi.ChatSettingsErrorFromProto[*chatpb.ChatOperationError])
}

func (c *Remote) Queue(ctx context.Context, request *chatpb.QueueRequest) (*chatpb.InputMutationSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(chatpb.File_kent_api_chat_chat_proto, "ChatService", "Queue"),
		request, &chatpb.QueueResult{}, protoapi.ChatSettingsErrorFromProto[*chatpb.ChatOperationError])
}

func (c *Remote) Compact(ctx context.Context, request *chatpb.CompactRequest) (*chatpb.CompactionMutationSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(chatpb.File_kent_api_chat_chat_proto, "ChatService", "Compact"),
		request, &chatpb.CompactResult{}, protoapi.ChatSettingsErrorFromProto[*chatpb.ChatOperationError])
}
