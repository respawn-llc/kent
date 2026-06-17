package client

import (
	"context"

	servicecontract "core/shared/apicontract"
	"core/shared/serverapi"
)

type PromptControlClient = servicecontract.PromptControlService

type loopbackPromptControlClient struct {
	loopbackClient[servicecontract.PromptControlService]
}

func NewLoopbackPromptControlClient(service servicecontract.PromptControlService) PromptControlClient {
	return &loopbackPromptControlClient{loopbackClient: newLoopbackClient(service)}
}

func (c *loopbackPromptControlClient) AnswerAsk(ctx context.Context, req serverapi.AskAnswerRequest) error {
	return callLoopbackClientNoResponse(c, "prompt control service is required", ctx, req, servicecontract.PromptControlService.AnswerAsk)
}

func (c *loopbackPromptControlClient) AnswerApproval(ctx context.Context, req serverapi.ApprovalAnswerRequest) error {
	return callLoopbackClientNoResponse(c, "prompt control service is required", ctx, req, servicecontract.PromptControlService.AnswerApproval)
}
