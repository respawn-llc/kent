package client

import (
	"context"

	servicecontract "core/shared/apicontract"
	"core/shared/serverapi"
)

type ProcessControlClient = servicecontract.ProcessControlService

type loopbackProcessControlClient struct {
	loopbackClient[servicecontract.ProcessControlService]
}

func NewLoopbackProcessControlClient(service servicecontract.ProcessControlService) ProcessControlClient {
	return &loopbackProcessControlClient{loopbackClient: newLoopbackClient(service)}
}

func (c *loopbackProcessControlClient) KillProcess(ctx context.Context, req serverapi.ProcessKillRequest) (serverapi.ProcessKillResponse, error) {
	return callLoopbackClient(c, "process control service is required", ctx, req, servicecontract.ProcessControlService.KillProcess)
}

func (c *loopbackProcessControlClient) GetInlineOutput(ctx context.Context, req serverapi.ProcessInlineOutputRequest) (serverapi.ProcessInlineOutputResponse, error) {
	return callLoopbackClient(c, "process control service is required", ctx, req, servicecontract.ProcessControlService.GetInlineOutput)
}
