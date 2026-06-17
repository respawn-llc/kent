package client

import (
	"context"

	servicecontract "core/shared/apicontract"
	"core/shared/serverapi"
)

type SessionViewClient interface {
	GetSessionMainView(ctx context.Context, req serverapi.SessionMainViewRequest) (serverapi.SessionMainViewResponse, error)
	GetSessionTranscriptPage(ctx context.Context, req serverapi.SessionTranscriptPageRequest) (serverapi.SessionTranscriptPageResponse, error)
	GetRun(ctx context.Context, req serverapi.RunGetRequest) (serverapi.RunGetResponse, error)
}

type SessionCommittedTranscriptSuffixClient interface {
	GetSessionCommittedTranscriptSuffix(ctx context.Context, req serverapi.SessionCommittedTranscriptSuffixRequest) (serverapi.SessionCommittedTranscriptSuffixResponse, error)
}

type loopbackSessionViewClient struct {
	loopbackClient[servicecontract.SessionViewService]
}

func NewLoopbackSessionViewClient(service servicecontract.SessionViewService) SessionViewClient {
	return &loopbackSessionViewClient{loopbackClient: newLoopbackClient(service)}
}

func (c *loopbackSessionViewClient) GetSessionMainView(ctx context.Context, req serverapi.SessionMainViewRequest) (serverapi.SessionMainViewResponse, error) {
	return callLoopbackClient(c, "session view service is required", ctx, req, servicecontract.SessionViewService.GetSessionMainView)
}

func (c *loopbackSessionViewClient) GetSessionTranscriptPage(ctx context.Context, req serverapi.SessionTranscriptPageRequest) (serverapi.SessionTranscriptPageResponse, error) {
	return callLoopbackClient(c, "session view service is required", ctx, req, servicecontract.SessionViewService.GetSessionTranscriptPage)
}

func (c *loopbackSessionViewClient) GetSessionCommittedTranscriptSuffix(ctx context.Context, req serverapi.SessionCommittedTranscriptSuffixRequest) (serverapi.SessionCommittedTranscriptSuffixResponse, error) {
	return callLoopbackClient(c, "session view service is required", ctx, req, servicecontract.SessionViewService.GetSessionCommittedTranscriptSuffix)
}

func (c *loopbackSessionViewClient) GetRun(ctx context.Context, req serverapi.RunGetRequest) (serverapi.RunGetResponse, error) {
	return callLoopbackClient(c, "session view service is required", ctx, req, servicecontract.SessionViewService.GetRun)
}
