package client

import (
	"context"

	servicecontract "core/shared/apicontract"
	"core/shared/serverapi"
)

type ServerStatusClient = servicecontract.ServerStatusService

type loopbackServerStatusClient struct {
	loopbackClient[servicecontract.ServerStatusService]
}

func NewLoopbackServerStatusClient(service servicecontract.ServerStatusService) ServerStatusClient {
	return &loopbackServerStatusClient{loopbackClient: newLoopbackClient(service)}
}

func (c *loopbackServerStatusClient) GetServerReadiness(ctx context.Context, req serverapi.ServerReadinessRequest) (serverapi.ServerReadinessResponse, error) {
	return callLoopbackClient(c, "server status service is required", ctx, req, servicecontract.ServerStatusService.GetServerReadiness)
}
