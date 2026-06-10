package primaryrun

import (
	"context"
	"errors"
	"testing"

	"builder/shared/clientui"
)

type stubRuntimeClient struct {
	submitCalls       int
	submitShellCalls  int
	queuedSubmitCalls int
}

func (s *stubRuntimeClient) MainView() clientui.RuntimeMainView { return clientui.RuntimeMainView{} }
func (s *stubRuntimeClient) RefreshMainView() (clientui.RuntimeMainView, error) {
	return clientui.RuntimeMainView{}, nil
}
func (s *stubRuntimeClient) Transcript() clientui.TranscriptPage { return clientui.TranscriptPage{} }
func (s *stubRuntimeClient) RefreshTranscript() (clientui.TranscriptPage, error) {
	return clientui.TranscriptPage{}, nil
}
func (s *stubRuntimeClient) RefreshTranscriptPage(clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	return clientui.TranscriptPage{}, nil
}
func (s *stubRuntimeClient) LoadTranscriptPage(clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	return clientui.TranscriptPage{}, nil
}
func (s *stubRuntimeClient) Status() clientui.RuntimeStatus { return clientui.RuntimeStatus{} }
func (s *stubRuntimeClient) SessionView() clientui.RuntimeSessionView {
	return clientui.RuntimeSessionView{}
}
func (s *stubRuntimeClient) SetSessionName(string) error                   { return nil }
func (s *stubRuntimeClient) SetThinkingLevel(string) error                 { return nil }
func (s *stubRuntimeClient) SetFastModeEnabled(bool) (bool, error)         { return false, nil }
func (s *stubRuntimeClient) SetReviewerEnabled(bool) (bool, string, error) { return false, "", nil }
func (s *stubRuntimeClient) SetAutoCompactionEnabled(bool) (bool, bool, error) {
	return false, false, nil
}
func (s *stubRuntimeClient) SetQuestionsEnabled(bool) (bool, error) { return false, nil }
func (s *stubRuntimeClient) ShowGoal() (*clientui.RuntimeGoal, error) { return nil, nil }
func (s *stubRuntimeClient) SetGoal(string) (*clientui.RuntimeGoal, error) {
	return &clientui.RuntimeGoal{}, nil
}
func (s *stubRuntimeClient) PauseGoal() (*clientui.RuntimeGoal, error) {
	return &clientui.RuntimeGoal{}, nil
}
func (s *stubRuntimeClient) ResumeGoal() (*clientui.RuntimeGoal, error) {
	return &clientui.RuntimeGoal{}, nil
}
func (s *stubRuntimeClient) ClearGoal() (*clientui.RuntimeGoal, error) { return nil, nil }
func (s *stubRuntimeClient) AppendLocalEntry(string, string) error     { return nil }
func (s *stubRuntimeClient) AppendLocalEntryWithNoticeID(string, string, string) error {
	return nil
}
func (s *stubRuntimeClient) SubmitUserMessage(context.Context, string) (string, error) {
	s.submitCalls++
	return "ok", nil
}
func (s *stubRuntimeClient) SubmitUserShellCommand(context.Context, string) error {
	s.submitShellCalls++
	return nil
}
func (s *stubRuntimeClient) CompactContext(context.Context, string) error { return nil }
func (s *stubRuntimeClient) HasQueuedUserWork() (bool, error)             { return false, nil }
func (s *stubRuntimeClient) SubmitQueuedUserMessages(context.Context) (string, error) {
	s.queuedSubmitCalls++
	return "ok", nil
}
func (s *stubRuntimeClient) Interrupt() error { return nil }
func (s *stubRuntimeClient) QueueUserMessage(text string) (clientui.QueuedUserMessage, error) {
	return clientui.QueuedUserMessage{ID: "queue-1", Text: text}, nil
}
func (s *stubRuntimeClient) DiscardQueuedUserMessage(string) bool { return false }
func (s *stubRuntimeClient) RecordPromptHistory(string) error     { return nil }

type stubGate struct {
	err          error
	acquireCalls int
	releases     int
}

func (g *stubGate) AcquirePrimaryRun(string) (Lease, error) {
	g.acquireCalls++
	if g.err != nil {
		return nil, g.err
	}
	return LeaseFunc(func() { g.releases++ }), nil
}

func TestGatedRuntimeClientGuardsPrimaryRunMethods(t *testing.T) {
	inner := &stubRuntimeClient{}
	gate := &stubGate{}
	client := NewGatedRuntimeClient("session-1", inner, gate)

	if _, err := client.SubmitUserMessage(context.Background(), "hello"); err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}
	if err := client.SubmitUserShellCommand(context.Background(), "pwd"); err != nil {
		t.Fatalf("SubmitUserShellCommand: %v", err)
	}
	if _, err := client.SubmitQueuedUserMessages(context.Background()); err != nil {
		t.Fatalf("SubmitQueuedUserMessages: %v", err)
	}
	if gate.acquireCalls != 3 || gate.releases != 3 {
		t.Fatalf("unexpected gate usage acquire=%d releases=%d", gate.acquireCalls, gate.releases)
	}
	if inner.submitCalls != 1 || inner.submitShellCalls != 1 || inner.queuedSubmitCalls != 1 {
		t.Fatalf("unexpected inner calls submit=%d shell=%d queued=%d", inner.submitCalls, inner.submitShellCalls, inner.queuedSubmitCalls)
	}
}

func TestGatedRuntimeClientReturnsActiveRunErrorWithoutCallingInner(t *testing.T) {
	inner := &stubRuntimeClient{}
	gate := &stubGate{err: ErrActivePrimaryRun}
	client := NewGatedRuntimeClient("session-1", inner, gate)

	if _, err := client.SubmitUserMessage(context.Background(), "hello"); !errors.Is(err, ErrActivePrimaryRun) {
		t.Fatalf("SubmitUserMessage error = %v, want active primary run", err)
	}
	if inner.submitCalls != 0 {
		t.Fatalf("expected no inner submit calls, got %d", inner.submitCalls)
	}
}
