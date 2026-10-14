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

func (s *inProcessRunPromptService) RunPrompt(ctx context.Context, req *runpromptpb.Request, progress serverapi.RunPromptProgressSink) (*runpromptpb.Success, error) {
	if s == nil || s.launcher == nil || s.launcher.boot.SessionLaunch == nil {
		return nil, errors.New("run prompt service is not configured")
	}
	input, err := s.launcher.boot.SessionLaunch.ResolveRunPromptRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.runPrompt(ctx, input, progress)
}

func (s *inProcessRunPromptService) runPrompt(ctx context.Context, req serverapi.RunPromptRequest, progress serverapi.RunPromptProgressSink) (response *runpromptpb.Success, err error) {
	req.Prompt = strings.TrimSpace(req.Prompt)

	delivery := newRunPromptProgressDelivery(progress)
	defer delivery.Close()
	runtimeHandle, err := s.launcher.prepareHeadlessPrompt(ctx, req, delivery)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err == nil && response != nil && response.GetQuestion() != nil {
			return
		}
		err = errors.Join(err, runtimeHandle.plan.CloseWithFailure(err != nil))
	}()

	startedAt := time.Now()
	if history := s.launcher.boot.PromptHistory; history != nil {
		_, err := history.RecordPromptHistoryEntry(ctx, metadata.PromptHistoryEntry{
			SessionID: runtimeHandle.plan.sessionID,
			Text:      runtimeHandle.plan.PromptHistoryText(req.Prompt),
		})
		if err != nil {
			return nil, err
		}
	}

	runCtx := ctx
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	response, runErr := runtimeHandle.submitUserMessage(runCtx, req.Prompt)
	response.Duration = durationpb.New(time.Since(startedAt))
	if runErr != nil {
		return response, runErr
	}
	return response, nil
}

var _ servicecontract.RunPromptService = (*inProcessRunPromptService)(nil)
