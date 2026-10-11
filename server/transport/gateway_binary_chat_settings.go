package transport

import (
	"context"
	"errors"

	"core/server/session"
	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/chat_settings"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"

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
					sharedpb.ScopePolicy_SCOPE_POLICY_PROJECT_WORKSPACE_BINDING, readOperation.Name,
					routeScopeParams{projectID: target.ProjectId, workspaceID: target.WorkspaceId})
			} else {
				id, resolveErr := g.deps.MetadataStore().ResolveSessionTarget(ctx, request.GetSession().SessionId)
				if resolveErr != nil {
					return nil, resolveErr
				}
				err = g.requireSessionInActiveProject(ctx, state, id.String())
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
			id, err := g.deps.MetadataStore().ResolveSessionTarget(ctx, request.Session.SessionId)
			if err != nil {
				return nil, err
			}
			if err := g.requireSessionInActiveProject(ctx, state, id.String()); err != nil {
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
}](g *Gateway, _ *connectionState, request Request, err error) proto.Message {
	var sessionID *string
	if request.GetSession() != nil {
		id, lookupErr := g.deps.MetadataStore().ResolveSessionTarget(context.Background(), request.GetSession().SessionId)
		if lookupErr != nil {
			if errors.Is(lookupErr, session.ErrSessionNotFound) {
				return binaryChatDomainFailure(nil, err)
			}
			return binaryInternalFailure(errors.Join(err, lookupErr))
		}
		value := id.String()
		sessionID = &value
	}
	return binaryChatDomainFailure(sessionID, err)
}
