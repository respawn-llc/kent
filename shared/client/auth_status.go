package client

import (
	"context"

	"core/shared/serverapi"
	"core/shared/servicecontract"
)

type AuthStatusClient = servicecontract.AuthStatusService

type loopbackAuthStatusClient struct {
	loopbackClient[servicecontract.AuthStatusService]
}

func NewLoopbackAuthStatusClient(service servicecontract.AuthStatusService) AuthStatusClient {
	return &loopbackAuthStatusClient{loopbackClient: newLoopbackClient(service)}
}

func (c *loopbackAuthStatusClient) GetAuthStatus(ctx context.Context, req serverapi.AuthStatusRequest) (serverapi.AuthStatusResponse, error) {
	return callLoopbackClient(c, "auth status service is required", ctx, req, servicecontract.AuthStatusService.GetAuthStatus)
}
