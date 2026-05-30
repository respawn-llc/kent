package client

import (
	"context"

	"builder/shared/serverapi"
	"builder/shared/servicecontract"
)

type SessionActivityClient = servicecontract.SessionActivityService

type loopbackSessionActivityClient struct {
	loopbackClient[servicecontract.SessionActivityService]
}

func NewLoopbackSessionActivityClient(service servicecontract.SessionActivityService) SessionActivityClient {
	return &loopbackSessionActivityClient{loopbackClient: newLoopbackClient(service)}
}

func (c *loopbackSessionActivityClient) SubscribeSessionActivity(ctx context.Context, req serverapi.SessionActivitySubscribeRequest) (serverapi.SessionActivitySubscription, error) {
	return callLoopbackClient(c, "session activity service is required", ctx, req, servicecontract.SessionActivityService.SubscribeSessionActivity)
}
