package app

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	authpb "core/shared/protoapi/gen/kent/api/auth"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/textutil"

	tea "github.com/charmbracelet/bubbletea"
)

func TestStatusLineGitStartupUsesRuntimeWorktreeRootBranch(t *testing.T) {
	processRoot := initStatusLineGitRepo(t, "main")
	workspaceRoot := initStatusLineGitRepo(t, "workspace-branch")
	worktreeRoot := initStatusLineGitRepo(t, "worktree-branch")
	t.Chdir(processRoot)

	runtimeClient := &runtimeControlFakeClient{sessionView: &runtimepb.SessionView{
		ExecutionTarget: &worktreepb.SessionExecutionTarget{
			WorkspaceRoot:    workspaceRoot,
			Worktree:         &worktreepb.SessionExecutionWorktreeTarget{Id: "worktree-1", Root: worktreeRoot},
			EffectiveWorkdir: processRoot}}}
	search := newStubUIPathReferenceSearch()
	close(search.events)
	model := newProjectedTestUIModel(
		runtimeClient,
		WithUIPathReferenceSearch(search),
		WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: workspaceRoot}))

	updated := drainStatusLineStartupCommands(t, model, model.Init())
	rendered := stripANSIAndTrimRight(updated.layout().renderStatusLine(120, uiThemeStyles("dark")))
	if !strings.Contains(rendered, "worktree-branch") {
		t.Fatalf("status line did not use runtime worktree branch: %q", rendered)
	}
	for _, unexpected := range []string{"main", "workspace-branch"} {
		if strings.Contains(rendered, unexpected) {
			t.Fatalf("status line used non-authoritative branch %q: %q", unexpected, rendered)
		}
	}
}

func TestTranscriptSessionIdentityUpdatesStatusExecutionTarget(t *testing.T) {
	initial := &worktreepb.SessionExecutionTarget{WorkspaceId: textutil.Value("workspace-1"),
		WorkspaceRoot:         "/repo",
		WorkspaceAvailability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_AVAILABLE,
		CwdRelpath:            ".",
		EffectiveWorkdir:      "/repo"}
	entered := &worktreepb.SessionExecutionTarget{WorkspaceId: textutil.Value("workspace-1"),
		WorkspaceRoot:         "/repo",
		WorkspaceAvailability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_AVAILABLE,
		Worktree:              &worktreepb.SessionExecutionWorktreeTarget{Id: "worktree-1", Root: "/repo/feature"},
		CwdRelpath:            ".",
		EffectiveWorkdir:      "/repo/feature"}
	runtimeClient := &sessionRuntimeClient{
		sessionID:   "session-1",
		mainView:    &runtimepb.MainView{Session: &runtimepb.SessionView{SessionId: "session-1"}, Status: &runtimepb.Status{}},
		hasMainView: true}
	model := newProjectedTestUIModel(runtimeClient, WithUIStatusConfig(uiStatusConfig{
		WorkspaceRoot:   initial.EffectiveWorkdir,
		ExecutionTarget: initial}))
	sessionID, err := runtimeids.ParseSessionID("session-1")
	if err != nil {
		t.Fatalf("ParseSessionId: %v", err)
	}
	if _, err := runtimeClient.admitTranscriptMessageState(transcriptTestMessage(0, &transcriptpb.SessionIdentity{SessionId: sessionID.String(),
		ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED,
		ExecutionTarget:       entered})); err != nil {
		t.Fatalf("admit transcript session identity: %v", err)
	}

	request := model.newStatusRequest(time.Now())
	if request.ExecutionTarget.EffectiveWorkdir != entered.EffectiveWorkdir {
		t.Fatalf("status execution target = %+v, want %q", request.ExecutionTarget, entered.EffectiveWorkdir)
	}
	if request.WorkspaceRoot != entered.EffectiveWorkdir {
		t.Fatalf("status workspace root = %q, want %q", request.WorkspaceRoot, entered.EffectiveWorkdir)
	}
}

func TestTranscriptSessionIdentityReplacesConflictingConversationFreshnessCaches(t *testing.T) {
	sessionID, err := runtimeids.ParseSessionID("session-1")
	if err != nil {
		t.Fatalf("ParseSessionId: %v", err)
	}
	runtimeClient := &sessionRuntimeClient{
		sessionID: "session-1",
		mainView: &runtimepb.MainView{
			Session: &runtimepb.SessionView{SessionId: "session-1",
				ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED},
			Status: &runtimepb.Status{
				ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED}},
		hasMainView: true}
	_, err = runtimeClient.admitTranscriptMessageState(transcriptTestMessage(0, &transcriptpb.SessionIdentity{SessionId: sessionID.String(),
		ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_FRESH}))
	if err != nil {
		t.Fatalf("admit transcript session identity: %v", err)
	}
	if runtimeClient.mainView.Session.ConversationFreshness != runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_FRESH {
		t.Fatalf("session-view conversation freshness = %q, want fresh", runtimeClient.mainView.Session.ConversationFreshness)
	}
	if runtimeClient.mainView.Status.ConversationFreshness != runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_FRESH {
		t.Fatalf("status conversation freshness = %q, want fresh", runtimeClient.mainView.Status.ConversationFreshness)
	}
	if got := runtimeClient.Status().ConversationFreshness; got != runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_FRESH {
		t.Fatalf("cached conversation freshness = %q, want fresh", got)
	}
}

func TestStatusRequestCarriesCachedRuntimeAgentRole(t *testing.T) {
	role := "worker"
	model := newProjectedTestUIModel(&runtimeControlFakeClient{})
	model.status.snapshot.AgentRole = &role
	request := model.newStatusRequest(time.Now())
	if request.AgentRole == nil || *request.AgentRole != role {
		t.Fatalf("status request agent role = %v, want %q", request.AgentRole, role)
	}
	if request.ConfiguredModelName != nil {
		t.Fatalf("status request configured model = %q, want absent", *request.ConfiguredModelName)
	}
}

func TestStatusRefreshUsesCurrentSessionAgentRoleWhenRuntimeCacheIsCold(t *testing.T) {
	role := "qa_tester"
	sessionViews := stubSessionViewClient{
		getSessionMainView: func(_ context.Context, request *sessionpb.MainViewRequest) (*sessionpb.MainViewSuccess, error) {
			if request.SessionId != "session-1" {
				t.Fatalf("current session view request = %+v, want session-1", request)
			}
			return &sessionpb.MainViewSuccess{
				MainView: &runtimepb.MainView{
					Session: &runtimepb.SessionView{SessionId: "session-1", AgentRole: &role}, Status: &runtimepb.Status{}}}, nil
		}}
	runtimeClient := &sessionRuntimeClient{
		sessionID: "session-1",
		mainView:  &runtimepb.MainView{Session: &runtimepb.SessionView{SessionId: "session-1"}, Status: &runtimepb.Status{}}}
	model := newProjectedTestUIModel(
		runtimeClient,
		WithUISessionID("session-1"),
		WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: t.TempDir(), SessionViews: sessionViews}))

	messages := collectCmdMessages(t, model.statusRefreshCmd())
	for _, message := range messages {
		base, ok := message.(statusBaseRefreshDoneMsg)
		if !ok {
			continue
		}
		if base.snapshot.AgentRole == nil || *base.snapshot.AgentRole != role {
			t.Fatalf("status agent role = %v, want %q", base.snapshot.AgentRole, role)
		}
		return
	}
	t.Fatal("status refresh did not emit a base snapshot")
}

func TestStatusRefreshCmdSchedulesBaseEnrichmentForProgressiveCollector(t *testing.T) {
	previousSessionID := runtimeids.NewSessionID()
	sessionViews := stubSessionViewClient{
		getSessionMainView: func(_ context.Context, request *sessionpb.MainViewRequest) (*sessionpb.MainViewSuccess, error) {
			if request.SessionId != previousSessionID.String() {
				t.Fatalf("previous session view request = %+v, want %s", request, previousSessionID)
			}
			return &sessionpb.MainViewSuccess{
				MainView: &runtimepb.MainView{
					Session: &runtimepb.SessionView{SessionId: previousSessionID.String(),
						SessionName: textutil.Value("incident-root")}}}, nil
		}}
	collector := &stubProgressiveStatusCollector{base: uiStatusSnapshot{PreviousSessionID: &previousSessionID}}
	model := newProjectedStaticUIModel(
		WithUIStatusConfig(uiStatusConfig{SessionViews: sessionViews}),
		WithUIStatusCollector(collector))
	cmd := model.statusRefreshCmd()
	if cmd == nil {
		t.Fatal("expected progressive status refresh to schedule base enrichment")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("status refresh message = %T, want non-empty tea.BatchMsg", cmd())
	}
	baseMsg, ok := batch[0]().(statusBaseRefreshDoneMsg)
	if !ok {
		t.Fatalf("batched message type = %T, want statusBaseRefreshDoneMsg", batch[0]())
	}
	if baseMsg.snapshot.PreviousSessionName != "incident-root" {
		t.Fatalf("previous session name = %q", baseMsg.snapshot.PreviousSessionName)
	}
}

func TestStatusRefreshDefersRuntimeAndAuthReadsToCommands(t *testing.T) {
	authStatus := &staticAuthStatusClient{response: authStatusResponse(authpb.AuthMethod_AUTH_METHOD_NONE)}
	runtimeClient := &statusRefreshRuntimeClient{}
	model := newProjectedStaticUIModel(WithUIStatusConfig(uiStatusConfig{AuthStatus: authStatus}))
	model.engine = runtimeClient

	cmd := model.statusRefreshCmd()
	if cmd == nil {
		t.Fatal("expected status refresh command")
	}
	if runtimeClient.statusCalls != 0 || authStatus.calls != 0 {
		t.Fatalf("status refresh performed eager reads: runtime=%d auth=%d", runtimeClient.statusCalls, authStatus.calls)
	}
	_ = collectCmdMessages(t, cmd)
	if runtimeClient.statusCalls == 0 || authStatus.calls == 0 {
		t.Fatalf("status refresh command reads: runtime=%d auth=%d", runtimeClient.statusCalls, authStatus.calls)
	}
}

func TestStatusRefreshAlwaysReloadsRemotelyMutableAuth(t *testing.T) {
	authStatus := &staticAuthStatusClient{response: authStatusResponse(authpb.AuthMethod_AUTH_METHOD_NONE)}
	model := newProjectedStaticUIModel(WithUIStatusConfig(uiStatusConfig{AuthStatus: authStatus}))

	for refresh := 1; refresh <= 2; refresh++ {
		for _, msg := range collectCmdMessages(t, model.statusRefreshCmd()) {
			next, _ := model.Update(msg)
			model = next.(*uiModel)
		}
		if authStatus.calls != refresh {
			t.Fatalf("auth status calls after refresh %d = %d, want %d", refresh, authStatus.calls, refresh)
		}
	}
}

type statusRefreshRuntimeClient struct {
	runtimeControlFakeClient
	statusCalls int
}

func (c *statusRefreshRuntimeClient) Status() *runtimepb.Status {
	c.statusCalls++
	return &runtimepb.Status{}
}

func initStatusLineGitRepo(t *testing.T, branch string) string {
	t.Helper()
	repoRoot := t.TempDir()
	cmd := exec.Command("git", "-C", repoRoot, "init", "-b", branch)
	cmd.Env = sanitizedGitEnv(os.Environ())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init -b %s: %v (%s)", branch, err, out)
	}
	return repoRoot
}

func drainStatusLineStartupCommands(t *testing.T, model *uiModel, cmd tea.Cmd) *uiModel {
	t.Helper()
	if cmd == nil {
		return model
	}
	msg := cmd()
	switch typed := msg.(type) {
	case nil:
		return model
	case tea.BatchMsg:
		for _, child := range typed {
			model = drainStatusLineStartupCommands(t, model, child)
		}
		return model
	default:
		next, nextCmd := model.Update(msg)
		updated, ok := next.(*uiModel)
		if !ok {
			t.Fatalf("unexpected model type %T", next)
		}
		return drainStatusLineStartupCommands(t, updated, nextCmd)
	}
}
