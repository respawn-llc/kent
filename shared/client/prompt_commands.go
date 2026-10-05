package client

import (
	"context"
	"errors"

	"core/shared/apicontract"
	"core/shared/protoapi"
	promptcommandpb "core/shared/protoapi/gen/kent/api/prompt_command"
	"core/shared/runtimeids"
)

var _ apicontract.PromptCommandCatalogService = (*Remote)(nil)

func (c *Remote) GetPromptCommandCatalog(ctx context.Context, req *promptcommandpb.GetCatalogRequest) (*promptcommandpb.Catalog, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(promptcommandpb.File_kent_api_prompt_command_prompt_command_proto, "PromptCommandService", "GetCatalog"),
		req, &promptcommandpb.GetCatalogResult{}, promptCommandCatalogError)
}

func promptCommandCatalogError(value *promptcommandpb.GetCatalogError) error {
	switch detail := value.Detail.(type) {
	case *promptcommandpb.GetCatalogError_CatalogRead:
		return protoapi.PromptCommandErrorFromProto(detail.CatalogRead)
	case *promptcommandpb.GetCatalogError_CommandNotFound:
		return protoapi.PromptCommandErrorFromProto(detail.CommandNotFound)
	case *promptcommandpb.GetCatalogError_CommandRead:
		return protoapi.PromptCommandErrorFromProto(detail.CommandRead)
	default:
		return errors.New(value.Code)
	}
}

type sessionPromptCommandCatalogClient struct {
	remote    *Remote
	sessionID runtimeids.SessionID
}

func (c sessionPromptCommandCatalogClient) GetPromptCommandCatalog(ctx context.Context, _ *promptcommandpb.GetCatalogRequest) (*promptcommandpb.Catalog, error) {
	sessionID := c.sessionID.String()
	return c.remote.GetPromptCommandCatalog(ctx, &promptcommandpb.GetCatalogRequest{SessionId: &sessionID})
}

func (c *Remote) PromptCommandCatalogClientForSession(sessionID string) (apicontract.PromptCommandCatalogService, error) {
	if c == nil {
		return nil, errors.New("remote client is required")
	}
	parsed, err := runtimeids.ParseSessionID(sessionID)
	if err != nil {
		return nil, err
	}
	return sessionPromptCommandCatalogClient{remote: c, sessionID: parsed}, nil
}
