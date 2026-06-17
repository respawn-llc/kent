package client

import (
	"context"

	servicecontract "core/shared/apicontract"
	"core/shared/serverapi"
)

type SessionLaunchClient = servicecontract.SessionLaunchService

type loopbackSessionLaunchClient struct {
	loopbackClient[servicecontract.SessionLaunchService]
}

func NewLoopbackSessionLaunchClient(service servicecontract.SessionLaunchService) SessionLaunchClient {
	return &loopbackSessionLaunchClient{loopbackClient: newLoopbackClient(service)}
}

func (c *loopbackSessionLaunchClient) PlanSession(ctx context.Context, req serverapi.SessionPlanRequest) (serverapi.SessionPlanResponse, error) {
	return callLoopbackClient(c, "session launch service is required", ctx, req, servicecontract.SessionLaunchService.PlanSession)
}
