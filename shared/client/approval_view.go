package client

import (
	"context"

	"core/shared/serverapi"
	"core/shared/servicecontract"
)

type ApprovalViewClient = servicecontract.ApprovalViewService

type loopbackApprovalViewClient struct {
	loopbackClient[servicecontract.ApprovalViewService]
}

func NewLoopbackApprovalViewClient(service servicecontract.ApprovalViewService) ApprovalViewClient {
	return &loopbackApprovalViewClient{loopbackClient: newLoopbackClient(service)}
}

func (c *loopbackApprovalViewClient) ListPendingApprovalsBySession(ctx context.Context, req serverapi.ApprovalListPendingBySessionRequest) (serverapi.ApprovalListPendingBySessionResponse, error) {
	return callLoopbackClient(c, "approval view service is required", ctx, req, servicecontract.ApprovalViewService.ListPendingApprovalsBySession)
}
