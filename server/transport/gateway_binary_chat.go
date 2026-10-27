package transport

import (
	"context"
	"errors"

	"core/server/session"
	"core/shared/apicontract"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	chatshellpb "core/shared/protoapi/gen/kent/api/chat_shell"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/serverapi"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func registerChatGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	service := chatpb.File_kent_api_chat_chat_proto.Services().ByName("ChatService")
	if err := registerGatewayBinaryUnary(bindings, chatshellpb.File_kent_api_chat_shell_chat_shell_proto.Services().ByName("ShellService"), "SubmitUserShellCommand",
		gatewayBinaryCoreActiveOrdinary,
		func() *chatshellpb.ShellCommandRequest { return &chatshellpb.ShellCommandRequest{} },
		chatTargetScope[*chatshellpb.ShellCommandRequest],
		func(g *Gateway, ctx context.Context, _ *connectionState, request *chatshellpb.ShellCommandRequest) (*chatshellpb.ShellCommandSuccess, error) {
			return g.deps.ChatMutationClient().SubmitUserShellCommand(ctx, request)
		},
		func(_ *Gateway, _ *connectionState, _ *chatshellpb.ShellCommandRequest, err error) proto.Message {
			detail := binaryChatDomainFailure(err)
			if _, internal := detail.(*sharedpb.InternalFailureDetails); internal {
				return binaryRuntimeControlFailure(err)
			}
			return detail
		},
	); err != nil {
		return err
	}
	if err := registerChatUnary(
		bindings,
		service,
		"Steer",
		func() *chatpb.SteerRequest { return &chatpb.SteerRequest{} },
		func(client apicontract.ChatMutationService, ctx context.Context, request *chatpb.SteerRequest) (*chatpb.InputMutationSuccess, error) {
			return client.Steer(ctx, request)
		},
	); err != nil {
		return err
	}
	if err := registerChatUnary(
		bindings,
		service,
		"Queue",
		func() *chatpb.QueueRequest { return &chatpb.QueueRequest{} },
		func(client apicontract.ChatMutationService, ctx context.Context, request *chatpb.QueueRequest) (*chatpb.InputMutationSuccess, error) {
			return client.Queue(ctx, request)
		},
	); err != nil {
		return err
	}
	return registerChatUnary(
		bindings,
		service,
		"Compact",
		func() *chatpb.CompactRequest { return &chatpb.CompactRequest{} },
		func(client apicontract.ChatMutationService, ctx context.Context, request *chatpb.CompactRequest) (*chatpb.CompactionMutationSuccess, error) {
			return client.Compact(ctx, request)
		},
	)
}

func registerChatUnary[
	Request protoapi.ChatTargetRequest,
	Success proto.Message,
](
	bindings map[string]gatewayBinaryBinding,
	service protoreflect.ServiceDescriptor,
	method protoreflect.Name,
	newRequest func() Request,
	invoke func(apicontract.ChatMutationService, context.Context, Request) (Success, error),
) error {
	return registerGatewayBinaryUnary(
		bindings,
		service,
		method,
		gatewayBinaryCoreActiveOrdinary,
		newRequest,
		chatTargetScope[Request],
		func(g *Gateway, ctx context.Context, _ *connectionState, request Request) (Success, error) {
			client := g.deps.ChatMutationClient()
			if client == nil {
				var zero Success
				return zero, errors.New("Chat mutation service is required")
			}
			return invoke(client, ctx, request)
		},
		binaryChatFailure[Request],
	)
}

func chatTargetScope[Request protoapi.ChatTargetRequest](request Request) (routeScopeParams, error) {
	return routeScopeParams{chatTarget: request.GetTarget()}, nil
}

func binaryChatFailure[Request protoapi.ChatTargetRequest](
	_ *Gateway,
	_ *connectionState,
	_ Request,
	err error,
) proto.Message {
	return binaryChatDomainFailure(err)
}

func binaryChatDomainFailure(err error) proto.Message {
	var agentPreparationErr *serverapi.ChatSettingsAgentPreparationError
	switch {
	case errors.Is(err, serverapi.ErrServerAuthRequired):
		return &authpb.AuthRequiredDetails{}
	case errors.Is(err, serverapi.ErrWorkspaceNotRegistered),
		errors.Is(err, errActiveProjectRequired):
		return &chatsettingspb.WorkspaceNotRegisteredDetails{}
	case errors.As(err, &agentPreparationErr):
		category, conversionErr := binaryChatAgentPreparationCategory(agentPreparationErr.Category)
		if conversionErr != nil {
			return binaryInternalFailure(errors.Join(err, conversionErr))
		}
		return &chatsettingspb.AgentPreparationDetails{
			Agent:    agentPreparationErr.Agent,
			Category: category,
		}
	case errors.Is(err, session.ErrSessionNotFound),
		errors.Is(err, errSessionOutsideActiveProject):
		return &chatsettingspb.SessionNotFoundDetails{SessionTarget: protoapi.SessionTargetErrorContext(err)}
	}
	return binaryInternalFailure(err)
}

func binaryChatAgentPreparationCategory(
	category serverapi.ChatSettingsAgentPreparationCategory,
) (chatsettingspb.AgentPreparationCategory, error) {
	switch category {
	case serverapi.ChatSettingsAgentInvalidConfiguration:
		return chatsettingspb.AgentPreparationCategory_AGENT_PREPARATION_CATEGORY_INVALID_CONFIGURATION, nil
	case serverapi.ChatSettingsAgentProviderUnavailable:
		return chatsettingspb.AgentPreparationCategory_AGENT_PREPARATION_CATEGORY_PROVIDER_UNAVAILABLE, nil
	case serverapi.ChatSettingsAgentInternalPreparation:
		return chatsettingspb.AgentPreparationCategory_AGENT_PREPARATION_CATEGORY_INTERNAL_PREPARATION, nil
	default:
		return chatsettingspb.AgentPreparationCategory_AGENT_PREPARATION_CATEGORY_UNSPECIFIED,
			errors.New("Chat settings Agent preparation category is invalid")
	}
}
