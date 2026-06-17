package client

import (
	"context"

	servicecontract "core/shared/apicontract"
	"core/shared/serverapi"
)

type AskViewClient = servicecontract.AskViewService

type loopbackAskViewClient struct {
	loopbackClient[servicecontract.AskViewService]
}

func NewLoopbackAskViewClient(service servicecontract.AskViewService) AskViewClient {
	return &loopbackAskViewClient{loopbackClient: newLoopbackClient(service)}
}

func (c *loopbackAskViewClient) ListPendingAsksBySession(ctx context.Context, req serverapi.AskListPendingBySessionRequest) (serverapi.AskListPendingBySessionResponse, error) {
	return callLoopbackClient(c, "ask view service is required", ctx, req, servicecontract.AskViewService.ListPendingAsksBySession)
}
