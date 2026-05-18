package runprompt

import (
	"context"
	"strings"

	"builder/server/requestmemo"
	"builder/shared/serverapi"
	"builder/shared/servicecontract"
)

type runPromptMemoRequest struct {
	SelectedSessionID string
	ParentSessionID   string
	Prompt            string
	Timeout           string
	Overrides         serverapi.RunPromptOverrides
}

type memoizingPromptService struct {
	inner servicecontract.RunPromptService
	runs  *requestmemo.Memo[runPromptMemoRequest, serverapi.RunPromptResponse]
}

func newMemoizingPromptService(inner servicecontract.RunPromptService) servicecontract.RunPromptService {
	if inner == nil {
		return nil
	}
	return &memoizingPromptService{
		inner: inner,
		runs:  requestmemo.New[runPromptMemoRequest, serverapi.RunPromptResponse](),
	}
}

func (s *memoizingPromptService) RunPrompt(ctx context.Context, req serverapi.RunPromptRequest, progress serverapi.RunPromptProgressSink) (serverapi.RunPromptResponse, error) {
	memoReq := runPromptMemoRequest{
		SelectedSessionID: strings.TrimSpace(req.SelectedSessionID),
		ParentSessionID:   strings.TrimSpace(req.ParentSessionID),
		Prompt:            strings.TrimSpace(req.Prompt),
		Timeout:           req.Timeout.String(),
		Overrides:         req.Overrides,
	}
	return s.runs.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameRunPromptMemoRequest, func(ctx context.Context) (serverapi.RunPromptResponse, error) {
		return s.inner.RunPrompt(ctx, req, progress)
	})
}

func sameRunPromptMemoRequest(a runPromptMemoRequest, b runPromptMemoRequest) bool {
	return a.SelectedSessionID == b.SelectedSessionID &&
		a.ParentSessionID == b.ParentSessionID &&
		a.Prompt == b.Prompt &&
		a.Timeout == b.Timeout &&
		a.Overrides == b.Overrides
}

var _ servicecontract.RunPromptService = (*memoizingPromptService)(nil)
