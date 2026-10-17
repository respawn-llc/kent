package runprompt

import (
	"context"
	"errors"
	"strings"
	"time"

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
	startedAt := time.Now()
	response, err = runtimeHandle.Complete(req.Timeout)
	if response != nil {
		response.Duration = durationpb.New(time.Since(startedAt))
	}
	return response, err
}

var _ servicecontract.RunPromptService = (*inProcessRunPromptService)(nil)
