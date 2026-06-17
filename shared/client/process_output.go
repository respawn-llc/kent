package client

import (
	"context"

	servicecontract "core/shared/apicontract"
	"core/shared/serverapi"
)

type ProcessOutputClient = servicecontract.ProcessOutputService

type loopbackProcessOutputClient struct {
	loopbackClient[servicecontract.ProcessOutputService]
}

func NewLoopbackProcessOutputClient(service servicecontract.ProcessOutputService) ProcessOutputClient {
	return &loopbackProcessOutputClient{loopbackClient: newLoopbackClient(service)}
}

func (c *loopbackProcessOutputClient) SubscribeProcessOutput(ctx context.Context, req serverapi.ProcessOutputSubscribeRequest) (serverapi.ProcessOutputSubscription, error) {
	return callLoopbackClient(c, "process output service is required", ctx, req, servicecontract.ProcessOutputService.SubscribeProcessOutput)
}
