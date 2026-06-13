package primaryrun

import (
	"context"
	"errors"
	"testing"

	"core/shared/serverapi"
)

type stubRunPromptService struct {
	calls int
	run   func(context.Context, serverapi.RunPromptRequest, serverapi.RunPromptProgressSink) (serverapi.RunPromptResponse, error)
}

func (s *stubRunPromptService) RunPrompt(ctx context.Context, req serverapi.RunPromptRequest, progress serverapi.RunPromptProgressSink) (serverapi.RunPromptResponse, error) {
	s.calls++
	if s.run == nil {
		return serverapi.RunPromptResponse{}, nil
	}
	return s.run(ctx, req, progress)
}

func TestGuardingPromptServiceGuardsSelectedSessionRuns(t *testing.T) {
	inner := &stubRunPromptService{run: func(context.Context, serverapi.RunPromptRequest, serverapi.RunPromptProgressSink) (serverapi.RunPromptResponse, error) {
		return serverapi.RunPromptResponse{SessionID: "session-1", Result: "ok"}, nil
	}}
	gate := &stubGate{}
	service := NewGuardingPromptService(gate, inner)

	resp, err := service.RunPrompt(context.Background(), serverapi.RunPromptRequest{ClientRequestID: "req-1", SelectedSessionID: "session-1", Prompt: "hello"}, nil)
	if err != nil {
		t.Fatalf("RunPrompt: %v", err)
	}
	if resp.SessionID != "session-1" || gate.acquireCalls != 1 || gate.releases != 1 || inner.calls != 1 {
		t.Fatalf("unexpected guard behavior resp=%+v acquire=%d releases=%d calls=%d", resp, gate.acquireCalls, gate.releases, inner.calls)
	}
}

func TestGuardingPromptServiceRejectsConcurrentSelectedSessionRuns(t *testing.T) {
	inner := &stubRunPromptService{}
	service := NewGuardingPromptService(&stubGate{err: ErrActivePrimaryRun}, inner)

	_, err := service.RunPrompt(context.Background(), serverapi.RunPromptRequest{ClientRequestID: "req-1", SelectedSessionID: "session-1", Prompt: "hello"}, nil)
	if !errors.Is(err, ErrActivePrimaryRun) {
		t.Fatalf("RunPrompt error = %v, want active primary run", err)
	}
	if inner.calls != 0 {
		t.Fatalf("expected no inner runs, got %d", inner.calls)
	}
}
