package client

import (
	"context"

	servicecontract "core/shared/apicontract"
	"core/shared/serverapi"
)

type PromptActivityClient = servicecontract.PromptActivityService

type loopbackPromptActivityClient struct {
	loopbackClient[servicecontract.PromptActivityService]
}

func NewLoopbackPromptActivityClient(service servicecontract.PromptActivityService) PromptActivityClient {
	if service == nil {
		return nil
	}
	return &loopbackPromptActivityClient{loopbackClient: newLoopbackClient(service)}
}

func (c *loopbackPromptActivityClient) SubscribePromptActivity(ctx context.Context, req serverapi.PromptActivitySubscribeRequest) (serverapi.PromptActivitySubscription, error) {
	return callLoopbackClient(c, "prompt activity service is required", ctx, req, servicecontract.PromptActivityService.SubscribePromptActivity)
}
