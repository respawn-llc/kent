package client

import (
	"context"
	"errors"

	"builder/shared/serverapi"
	"builder/shared/servicecontract"
)

type RunPromptClient = servicecontract.RunPromptService

type loopbackRunPromptClient struct {
	loopbackClient[servicecontract.RunPromptService]
}

func NewLoopbackRunPromptClient(service servicecontract.RunPromptService) RunPromptClient {
	return &loopbackRunPromptClient{loopbackClient: newLoopbackClient(service)}
}

func (c *loopbackRunPromptClient) RunPrompt(ctx context.Context, req serverapi.RunPromptRequest, progress serverapi.RunPromptProgressSink) (serverapi.RunPromptResponse, error) {
	service, ok := requireLoopbackService(c)
	if !ok {
		return serverapi.RunPromptResponse{}, errors.New("run prompt service is required")
	}
	return service.RunPrompt(ctx, req, progress)
}
