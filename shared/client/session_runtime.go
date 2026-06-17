package client

import (
	"context"

	servicecontract "core/shared/apicontract"
	"core/shared/serverapi"
)

type SessionRuntimeClient = servicecontract.SessionRuntimeService

type loopbackSessionRuntimeClient struct {
	loopbackClient[servicecontract.SessionRuntimeService]
}

func NewLoopbackSessionRuntimeClient(service servicecontract.SessionRuntimeService) SessionRuntimeClient {
	return &loopbackSessionRuntimeClient{loopbackClient: newLoopbackClient(service)}
}

func (c *loopbackSessionRuntimeClient) ActivateSessionRuntime(ctx context.Context, req serverapi.SessionRuntimeActivateRequest) (serverapi.SessionRuntimeActivateResponse, error) {
	return callLoopbackClient(c, "session runtime service is required", ctx, req, servicecontract.SessionRuntimeService.ActivateSessionRuntime)
}

func (c *loopbackSessionRuntimeClient) ReleaseSessionRuntime(ctx context.Context, req serverapi.SessionRuntimeReleaseRequest) (serverapi.SessionRuntimeReleaseResponse, error) {
	return callLoopbackClient(c, "session runtime service is required", ctx, req, servicecontract.SessionRuntimeService.ReleaseSessionRuntime)
}
