package runprompt

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"core/shared/serverapi"
)

func TestPromptServiceRejectsEmptyPrompt(t *testing.T) {
	service := NewPromptService(&stubHeadlessPromptLauncher{})

	_, err := service.RunPrompt(context.Background(), serverapi.RunPromptRequest{ClientRequestID: "req-1", Prompt: " \n\t "}, nil)
	if !errors.Is(err, ErrPromptRequired) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPromptServiceRejectsMissingClientRequestID(t *testing.T) {
	service := NewPromptService(&stubHeadlessPromptLauncher{})

	_, err := service.RunPrompt(context.Background(), serverapi.RunPromptRequest{Prompt: "hello"}, nil)
	if !errors.Is(err, ErrClientRequestIDRequired) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPromptServiceAppliesTimeoutToSubmittedRun(t *testing.T) {
	launcher := &stubHeadlessPromptLauncher{
		runtime: &stubPromptSessionRuntime{
			assistant: PromptAssistantMessage{SessionID: "session-timeout", SessionName: "timeout"},
			onSubmit: func(ctx context.Context) {
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Fatal("expected timeout deadline on run context")
				}
				if time.Until(deadline) <= 0 {
					t.Fatal("expected future deadline")
				}
			},
		},
	}
	service := NewPromptService(launcher)

	_, err := service.RunPrompt(context.Background(), serverapi.RunPromptRequest{ClientRequestID: "req-1", Prompt: "hello", Timeout: 5 * time.Second}, nil)
	if err != nil {
		t.Fatalf("RunPrompt: %v", err)
	}
}

type stubHeadlessPromptLauncher struct {
	runtime     *stubPromptSessionRuntime
	lastRequest serverapi.RunPromptRequest
}

func (s *stubHeadlessPromptLauncher) PrepareHeadlessPrompt(_ context.Context, req serverapi.RunPromptRequest, progress serverapi.RunPromptProgressSink) (PromptSessionRuntime, error) {
	s.lastRequest = req
	if progress != nil {
		progress.PublishRunPromptProgress(serverapi.RunPromptProgress{Kind: serverapi.RunPromptProgressKindStatus, Message: "Prepared run context"})
	}
	if s.runtime == nil {
		s.runtime = &stubPromptSessionRuntime{}
	}
	return s.runtime, nil
}

type stubPromptSessionRuntime struct {
	assistant        PromptAssistantMessage
	err              error
	prompt           string
	closed           bool
	logs             []string
	onSubmit         func(context.Context)
	historyRequestID string
	historyPrompt    string
}

func (s *stubPromptSessionRuntime) RecordPromptHistory(_ context.Context, clientRequestID string, prompt string) error {
	s.historyRequestID = clientRequestID
	s.historyPrompt = prompt
	return nil
}

func (s *stubPromptSessionRuntime) SubmitUserMessage(ctx context.Context, prompt string) (PromptAssistantMessage, error) {
	s.prompt = prompt
	if s.onSubmit != nil {
		s.onSubmit(ctx)
	}
	return s.assistant, s.err
}

func (s *stubPromptSessionRuntime) Logf(format string, args ...any) {
	s.logs = append(s.logs, fmt.Sprintf(format, args...))
}

func (s *stubPromptSessionRuntime) Close() error {
	s.closed = true
	return nil
}
