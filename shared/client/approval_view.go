package client

import (
	"context"

	servicecontract "core/shared/apicontract"
	"core/shared/serverapi"
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
