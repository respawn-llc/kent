package transport

import (
	"context"
	"errors"

	contextpb "core/shared/protoapi/gen/kent/api/chat_context"
	"core/shared/runtimeids"
	"google.golang.org/protobuf/proto"
)

func registerChatContextGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	return registerGatewayBinaryUnary(bindings,
		contextpb.File_kent_api_chat_context_chat_context_proto.Services().ByName("ChatContextService"), "Get",
		gatewayBinaryCoreActiveOrdinary,
		func() *contextpb.GetRequest { return &contextpb.GetRequest{} },
		func(request *contextpb.GetRequest) (routeScopeParams, error) {
			return routeScopeParams{sessionID: request.Target.GetSession().SessionId}, nil
		},
		func(g *Gateway, ctx context.Context, _ *connectionState, request *contextpb.GetRequest) (*contextpb.GetSuccess, error) {
			id, err := runtimeids.ParseSessionID(request.Target.GetSession().SessionId)
			if err != nil {
				return nil, err
			}
			owner := g.deps.SessionChatContextOwner()
			if owner == nil {
				return nil, errors.New("Session Chat Context owner is required")
			}
			facts, err := owner.ReadSessionChatContext(ctx, id)
			if err != nil {
				return nil, err
			}
			return &contextpb.GetSuccess{Context: facts}, nil
		},
		func(_ *Gateway, _ *connectionState, _ *contextpb.GetRequest, err error) proto.Message {
			return binaryAuthFailure(err)
		})
}
