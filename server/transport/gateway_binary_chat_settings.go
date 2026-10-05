package transport

import (
	"context"

	"core/shared/apicontract"
	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/chat_settings"

	"google.golang.org/protobuf/proto"
)

func registerChatSettingsGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	service := pb.File_kent_api_chat_settings_chat_settings_proto.Services().ByName("ChatSettingsService")
	readOperation, err := protoapi.OperationFromDescriptor(service.Methods().ByName("Read"))
	if err != nil {
		return err
	}
	if err := registerGatewayBinaryUnary(
		bindings, service, "Read", gatewayBinaryCoreActiveOrdinary,
		func() *pb.ReadRequest { return &pb.ReadRequest{} }, nil,
		func(g *Gateway, ctx context.Context, state *connectionState, request *pb.ReadRequest) (*pb.ReadSuccess, error) {
			var err error
			if target := request.GetNewChat(); target != nil {
				err = newRoutePolicyExecutor(g).authorizeScopeFacts(ctx, state,
					apicontract.ScopeProjectWorkspaceBinding, readOperation.Name,
					routeScopeParams{projectID: target.ProjectId, workspaceID: target.WorkspaceId})
			} else {
				err = g.requireSessionInActiveProject(ctx, state, request.GetSession().SessionId)
			}
			if err != nil {
				return nil, err
			}
			return g.deps.ChatSettingsClient().ReadChatSettings(ctx, request)
		},
		binaryChatSettingsFailure[*pb.ReadRequest],
	); err != nil {
		return err
	}
	return registerGatewayBinaryUnary(
		bindings, service, "Mutate", gatewayBinaryCoreActiveOrdinary,
		func() *pb.MutationRequest { return &pb.MutationRequest{} }, nil,
		func(g *Gateway, ctx context.Context, state *connectionState, request *pb.MutationRequest) (*pb.MutationSuccess, error) {
			if err := g.requireSessionInActiveProject(ctx, state, request.Session.SessionId); err != nil {
				return nil, err
			}
			return g.deps.ChatSettingsClient().MutateChatSettings(ctx, request)
		},
		binaryChatSettingsFailure[*pb.MutationRequest],
	)
}

func binaryChatSettingsFailure[Request interface {
	proto.Message
	GetSession() *pb.SessionTarget
}](_ *Gateway, _ *connectionState, request Request, err error) proto.Message {
	var sessionID *string
	if request.GetSession() != nil {
		sessionID = &request.GetSession().SessionId
	}
	return binaryChatDomainFailure(sessionID, err)
}
