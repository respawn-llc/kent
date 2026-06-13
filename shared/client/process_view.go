package client

import (
	"context"

	"core/shared/serverapi"
	"core/shared/servicecontract"
)

type ProcessViewClient = servicecontract.ProcessViewService

type loopbackProcessViewClient struct {
	loopbackClient[servicecontract.ProcessViewService]
}

func NewLoopbackProcessViewClient(service servicecontract.ProcessViewService) ProcessViewClient {
	return &loopbackProcessViewClient{loopbackClient: newLoopbackClient(service)}
}

func (c *loopbackProcessViewClient) ListProcesses(ctx context.Context, req serverapi.ProcessListRequest) (serverapi.ProcessListResponse, error) {
	return callLoopbackClient(c, "process view service is required", ctx, req, servicecontract.ProcessViewService.ListProcesses)
}

func (c *loopbackProcessViewClient) GetProcess(ctx context.Context, req serverapi.ProcessGetRequest) (serverapi.ProcessGetResponse, error) {
	return callLoopbackClient(c, "process view service is required", ctx, req, servicecontract.ProcessViewService.GetProcess)
}
