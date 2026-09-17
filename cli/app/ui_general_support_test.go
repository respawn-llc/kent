package app

import (
	"context"
	"core/shared/apicontract"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"testing"
	"time"
)

func disableTransientStatusClearForTest(t *testing.T) {
	t.Helper()
	originalClear := scheduleTransientStatusClear
	scheduleTransientStatusClear = func(time.Duration, uint64) tea.Cmd { return nil }
	t.Cleanup(func() {
		scheduleTransientStatusClear = originalClear
	})
}

type stubSessionViewClient struct {
	apicontract.SessionViewService
	getSessionMainView   func(context.Context, *sessionpb.MainViewRequest) (*sessionpb.MainViewSuccess, error)
	getLatestFinalAnswer func(context.Context, *transcriptpb.LatestFinalAnswerRequest) (*transcriptpb.LatestFinalAnswerSuccess, error)
}

func (s stubSessionViewClient) GetSessionMainView(ctx context.Context, req *sessionpb.MainViewRequest) (*sessionpb.MainViewSuccess, error) {
	if s.getSessionMainView == nil {
		return &sessionpb.MainViewSuccess{}, errors.New("session view stub is required")
	}
	return s.getSessionMainView(ctx, req)
}

func (s stubSessionViewClient) GetSessionTranscriptPage(context.Context, *transcriptpb.PageRequest) (*transcriptpb.PageSuccess, error) {
	return &transcriptpb.PageSuccess{}, nil
}

func (s stubSessionViewClient) GetLatestCommittedAssistantFinalAnswer(ctx context.Context, req *transcriptpb.LatestFinalAnswerRequest) (*transcriptpb.LatestFinalAnswerSuccess, error) {
	if s.getLatestFinalAnswer == nil {
		return &transcriptpb.LatestFinalAnswerSuccess{}, errors.New("latest final answer stub is required")
	}
	return s.getLatestFinalAnswer(ctx, req)
}

func updateUIModel(t *testing.T, m *uiModel, msg tea.Msg) *uiModel {
	t.Helper()
	next, command := m.Update(msg)
	updated, ok := next.(*uiModel)
	if !ok {
		t.Fatalf("unexpected model type %T", next)
	}
	if _, isAskEvent := msg.(askEventMsg); isAskEvent && command != nil {
		if projection, isProjection := command().(questionRenderResultMsg); isProjection {
			next, _ = updated.Update(projection)
			updated, ok = next.(*uiModel)
			if !ok {
				t.Fatalf("unexpected model type after ask projection %T", next)
			}
		}
	}
	if _, isAskEvent := msg.(askEventMsg); isAskEvent &&
		updated.ask.current != nil &&
		updated.ask.activeProjection == nil &&
		updated.ask.inFlightProjection == nil {
		testInstallCurrentAskProjection(updated)
	}
	return updated
}

func collectCmdMessages(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	msgs := make([]tea.Msg, 0)
	var runMsg func(tea.Msg)
	var runCmd func(tea.Cmd)
	runCmd = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		runMsg(cmd())
	}
	runMsg = func(msg tea.Msg) {
		if msg == nil {
			return
		}
		msgs = append(msgs, msg)
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, nested := range batch {
				runCmd(nested)
			}
		}
	}
	runCmd(cmd)
	return msgs
}

type stubClipboardTextCopier struct {
	text  string
	err   error
	calls int
}

func (s *stubClipboardTextCopier) CopyText(_ context.Context, text string) error {
	s.calls++
	s.text = text
	return s.err
}

func withTrueColor(t *testing.T) {
	t.Helper()
	previousProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previousProfile) })
}

type stubProgressiveStatusCollector struct {
	base       uiStatusSnapshot
	authResult uiStatusAuthStageResult
	gitResult  uiStatusGitStageResult
	envResult  uiStatusEnvironmentStageResult
	gitCalls   int
}

func (s *stubProgressiveStatusCollector) Collect(_ context.Context, _ uiStatusRequest) (uiStatusSnapshot, error) {
	snapshot := s.base
	snapshot.Auth = s.authResult.Auth
	snapshot.Subscription = s.authResult.Subscription
	snapshot.Git = s.gitResult.Git
	snapshot.Skills = s.envResult.Skills
	snapshot.SkillTokenCounts = s.envResult.SkillTokenCounts
	snapshot.AgentsPaths = s.envResult.AgentsPaths
	snapshot.AgentTokenCounts = s.envResult.AgentTokenCounts
	snapshot.CollectorWarning = s.envResult.CollectorWarning
	return snapshot, nil
}

func (s *stubProgressiveStatusCollector) CollectBase(_ uiStatusRequest) uiStatusSnapshot {
	return s.base
}

func (s *stubProgressiveStatusCollector) CollectAuth(_ context.Context, _ uiStatusRequest, _ uiStatusSnapshot) uiStatusAuthStageResult {
	return s.authResult
}

func (s *stubProgressiveStatusCollector) CollectGit(_ context.Context, _ uiStatusRequest, _ uiStatusSnapshot) uiStatusGitStageResult {
	s.gitCalls++
	return s.gitResult
}

func (s *stubProgressiveStatusCollector) CollectEnvironment(_ context.Context, _ uiStatusRequest, _ uiStatusSnapshot) uiStatusEnvironmentStageResult {
	return s.envResult
}

type statusRequestOption func(*uiStatusRequest)

func newStatusRequestForTest(options ...statusRequestOption) uiStatusRequest {
	var req uiStatusRequest
	for _, option := range options {
		if option != nil {
			option(&req)
		}
	}
	return populateStatusRequestCacheKeys(req)
}

func withStatusWorkspaceRoot(root string) statusRequestOption {
	return func(req *uiStatusRequest) {
		req.WorkspaceRoot = root
	}
}
