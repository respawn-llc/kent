package client

import (
	"context"

	"builder/shared/serverapi"
	"builder/shared/servicecontract"
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
