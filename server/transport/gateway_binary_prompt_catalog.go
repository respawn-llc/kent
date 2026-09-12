package transport

import (
	"context"
	"errors"

	"core/shared/protoapi"
	promptcommandpb "core/shared/protoapi/gen/kent/api/prompt_command"
	"core/shared/runtimeids"
	"core/shared/serverapi"

	"google.golang.org/protobuf/proto"
)

func registerPromptCatalogGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	return registerGatewayBinaryUnary(bindings,
		promptcommandpb.File_kent_api_prompt_command_prompt_command_proto.Services().ByName("PromptCommandService"), "GetCatalog",
		gatewayBinaryCoreActiveOrdinary,
		func() *promptcommandpb.GetCatalogRequest { return &promptcommandpb.GetCatalogRequest{} }, nil,
		func(g *Gateway, ctx context.Context, state *connectionState, request *promptcommandpb.GetCatalogRequest) (*promptcommandpb.Catalog, error) {
			projectID, err := g.activeProjectID(ctx, state)
			if err != nil {
				return nil, err
			}
			var sessionID *runtimeids.SessionID
			if request.SessionId != nil {
				parsed, err := runtimeids.ParseSessionID(*request.SessionId)
				if err != nil {
					return nil, err
				}
				sessionID = &parsed
			}
			workspaceRoot, err := g.promptCommandWorkspaceRootForCatalog(ctx, state, sessionID)
			if err != nil {
				return nil, err
			}
			catalog, err := g.deps.PromptCommandCatalogClientForProjectWorkspace(ctx, projectID, workspaceRoot)
			if err != nil {
				return nil, err
			}
			return catalog.GetPromptCommandCatalog(ctx, request)
		},
		func(_ *Gateway, _ *connectionState, _ *promptcommandpb.GetCatalogRequest, err error) proto.Message {
			var commandErr *serverapi.PromptCommandError
			if errors.As(err, &commandErr) {
				detail, conversionErr := protoapi.PromptCommandErrorToProto(commandErr)
				if conversionErr != nil {
					return binaryInternalFailure(errors.Join(err, conversionErr))
				}
				return detail
			}
			return binaryAuthFailure(err)
		})
}
