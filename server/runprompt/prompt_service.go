package runprompt

import (
	"context"
	"errors"
	"strings"
	"time"

	"builder/shared/serverapi"
)

type PromptAssistantMessage struct {
	Content string
}

type PromptSessionRuntime interface {
	SubmitUserMessage(ctx context.Context, prompt string) (PromptAssistantMessage, error)
	SessionID() string
	SessionName() string
	DroppedEvents() uint64
	Warnings() []string
	Logf(format string, args ...any)
	Close() error
}

type HeadlessPromptLauncher interface {
	PrepareHeadlessPrompt(ctx context.Context, req serverapi.RunPromptRequest, progress serverapi.RunPromptProgressSink) (PromptSessionRuntime, error)
}

type PromptService struct {
	launcher HeadlessPromptLauncher
}

func NewPromptService(launcher HeadlessPromptLauncher) *PromptService {
	return &PromptService{launcher: launcher}
}

func (s *PromptService) RunPrompt(ctx context.Context, req serverapi.RunPromptRequest, progress serverapi.RunPromptProgressSink) (serverapi.RunPromptResponse, error) {
	if s == nil || s.launcher == nil {
		return serverapi.RunPromptResponse{}, errors.New("prompt service launcher is required")
	}
	req.ClientRequestID = strings.TrimSpace(req.ClientRequestID)
	req.SelectedSessionID = strings.TrimSpace(req.SelectedSessionID)
	req.ParentSessionID = strings.TrimSpace(req.ParentSessionID)
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.ClientRequestID == "" {
		return serverapi.RunPromptResponse{}, errors.New("client_request_id is required")
	}
	if req.Prompt == "" {
		return serverapi.RunPromptResponse{}, errors.New("prompt is required")
	}

	runtimeHandle, err := s.launcher.PrepareHeadlessPrompt(ctx, req, progress)
	if err != nil {
		return serverapi.RunPromptResponse{}, err
	}
	defer func() {
		_ = runtimeHandle.Close()
	}()

	runCtx := ctx
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	startedAt := time.Now()
	assistant, runErr := runtimeHandle.SubmitUserMessage(runCtx, req.Prompt)
	duration := time.Since(startedAt)
	result := serverapi.RunPromptResponse{
		SessionID:   runtimeHandle.SessionID(),
		SessionName: runtimeHandle.SessionName(),
		Result:      assistant.Content,
		Duration:    duration,
		Warnings:    append([]string(nil), runtimeHandle.Warnings()...),
	}
	if dropped := runtimeHandle.DroppedEvents(); dropped > 0 {
		runtimeHandle.Logf("runtime.event.drop.total=%d", dropped)
	}
	if runErr != nil {
		runtimeHandle.Logf("app.run_prompt.exit err=%q", runErr.Error())
		return result, runErr
	}
	runtimeHandle.Logf("app.run_prompt.exit ok")
	return result, nil
}
