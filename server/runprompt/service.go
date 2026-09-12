package runprompt

import (
	"context"
	"errors"
	"strings"
	"time"

	"core/server/metadata"
	servicecontract "core/shared/apicontract"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	"core/shared/serverapi"

	"google.golang.org/protobuf/types/known/durationpb"
)

type inProcessRunPromptService struct {
	launcher *headlessPromptLauncher
}

func (s *inProcessRunPromptService) RunPrompt(ctx context.Context, req serverapi.RunPromptRequest, progress serverapi.RunPromptProgressSink) (*runpromptpb.Success, error) {
	return s.runPrompt(ctx, req, progress)
}

func (s *inProcessRunPromptService) runPrompt(ctx context.Context, req serverapi.RunPromptRequest, progress serverapi.RunPromptProgressSink) (response *runpromptpb.Success, err error) {
	if s == nil || s.launcher == nil {
		return nil, errors.New("run prompt service is not configured")
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	if err := req.Validate(); err != nil {
		return nil, err
	}

	runtimeHandle, err := s.launcher.prepareHeadlessPrompt(ctx, req, progress)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, runtimeHandle.plan.CloseWithFailure(err != nil))
	}()

	runCtx := ctx
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	startedAt := time.Now()
	if history := s.launcher.boot.PromptHistory; history != nil {
		_, err := history.RecordPromptHistoryEntry(runCtx, metadata.PromptHistoryEntry{
			SessionID: runtimeHandle.plan.sessionID,
			Text:      runtimeHandle.plan.PromptHistoryText(req.Prompt),
		})
		if err != nil {
			return nil, err
		}
	}
	response, runErr := runtimeHandle.submitUserMessage(runCtx, req.Prompt)
	response.Duration = durationpb.New(time.Since(startedAt))
	if runErr != nil {
		return response, runErr
	}
	return response, nil
}

var _ servicecontract.RunPromptService = (*inProcessRunPromptService)(nil)
