package app

import (
	"context"
	"sync"
	"sync/atomic"

	"core/shared/apicontract"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
)

type countingSessionViewClient struct {
	apicontract.SessionViewService
	view            *runtimepb.MainView
	page            *transcriptpb.Page
	count           atomic.Int32
	mainViewCount   atomic.Int32
	pageCount       atomic.Int32
	lastMainViewReq *sessionpb.MainViewRequest
	lastPageReq     *transcriptpb.PageRequest
	lastFinalReq    *transcriptpb.LatestFinalAnswerRequest
	finalAnswer     *string
	finalAnswerErr  error
}

func (c *countingSessionViewClient) GetSessionMainView(_ context.Context, req *sessionpb.MainViewRequest) (*sessionpb.MainViewSuccess, error) {
	c.lastMainViewReq = req
	c.count.Add(1)
	c.mainViewCount.Add(1)
	return &sessionpb.MainViewSuccess{MainView: c.view}, nil
}

func (c *countingSessionViewClient) GetSessionTranscriptPage(_ context.Context, req *transcriptpb.PageRequest) (*transcriptpb.PageSuccess, error) {
	c.lastPageReq = req
	c.pageCount.Add(1)
	return &transcriptpb.PageSuccess{Transcript: c.page}, nil
}

func (c *countingSessionViewClient) GetLatestCommittedAssistantFinalAnswer(_ context.Context, req *transcriptpb.LatestFinalAnswerRequest) (*transcriptpb.LatestFinalAnswerSuccess, error) {
	c.lastFinalReq = req
	if c.finalAnswerErr != nil {
		return &transcriptpb.LatestFinalAnswerSuccess{}, c.finalAnswerErr
	}
	return &transcriptpb.LatestFinalAnswerSuccess{Answer: c.finalAnswer}, nil
}

type controlledTranscriptPageResult struct {
	response *transcriptpb.PageSuccess
	err      error
}

type controlledTranscriptPageClient struct {
	apicontract.SessionViewService
	started chan *transcriptpb.PageRequest
	results chan controlledTranscriptPageResult
}

func newControlledTranscriptPageClient() *controlledTranscriptPageClient {
	return &controlledTranscriptPageClient{
		started: make(chan *transcriptpb.PageRequest, 8),
		results: make(chan controlledTranscriptPageResult, 8),
	}
}

func (c *controlledTranscriptPageClient) GetSessionMainView(context.Context, *sessionpb.MainViewRequest) (*sessionpb.MainViewSuccess, error) {
	return &sessionpb.MainViewSuccess{}, nil
}

func (c *controlledTranscriptPageClient) GetSessionTranscriptPage(ctx context.Context, req *transcriptpb.PageRequest) (*transcriptpb.PageSuccess, error) {
	c.started <- req
	select {
	case result := <-c.results:
		return result.response, result.err
	case <-ctx.Done():
		return &transcriptpb.PageSuccess{}, ctx.Err()
	}
}

func (c *controlledTranscriptPageClient) GetLatestCommittedAssistantFinalAnswer(context.Context, *transcriptpb.LatestFinalAnswerRequest) (*transcriptpb.LatestFinalAnswerSuccess, error) {
	return &transcriptpb.LatestFinalAnswerSuccess{}, nil
}

type flakySessionViewClient struct {
	apicontract.SessionViewService
	mu        sync.Mutex
	responses []*sessionpb.MainViewSuccess
	errs      []error
	count     int
}

func (c *flakySessionViewClient) GetSessionMainView(context.Context, *sessionpb.MainViewRequest) (*sessionpb.MainViewSuccess, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	idx := c.count
	c.count++
	if idx < len(c.errs) && c.errs[idx] != nil {
		return &sessionpb.MainViewSuccess{}, c.errs[idx]
	}
	if idx < len(c.responses) {
		return c.responses[idx], nil
	}
	if len(c.responses) > 0 {
		return c.responses[len(c.responses)-1], nil
	}
	return &sessionpb.MainViewSuccess{}, nil
}

func (c *flakySessionViewClient) GetSessionTranscriptPage(context.Context, *transcriptpb.PageRequest) (*transcriptpb.PageSuccess, error) {
	return &transcriptpb.PageSuccess{}, nil
}

func (c *flakySessionViewClient) GetLatestCommittedAssistantFinalAnswer(context.Context, *transcriptpb.LatestFinalAnswerRequest) (*transcriptpb.LatestFinalAnswerSuccess, error) {
	return &transcriptpb.LatestFinalAnswerSuccess{}, nil
}
