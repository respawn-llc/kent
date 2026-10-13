package sessionservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"core/internal/testharness/requestfixture"
	"core/internal/testharness/testsetup"
	"core/server/auth"
	"core/server/launch"
	"core/server/metadata"
	"core/server/session"
	sessionruntime "core/server/sessionruntime"
	"core/shared/config"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/rollbacktarget"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/textutil"
	"core/shared/worktreecontract"

	"google.golang.org/protobuf/proto"
)

func appendSessionMessage(t *testing.T, store *session.Store, stepID string, role session.MessageRole, content string) session.EventRecord {
	t.Helper()
	eventLog, err := store.MaterializeEventLog()
	if err != nil {
		t.Fatalf("materialize event log: %v", err)
	}
	step := stepID
	message := content
	record, receipt, err := eventLog.AppendRecord(&step, session.MessageRecord{
		Role:    role,
		Content: &message,
	})
	if err != nil || !receipt.Committed {
		t.Fatalf("append typed message: receipt=%+v error=%v", receipt, err)
	}
	return record
}

func userMessageSeqAt(t *testing.T, store *session.Store, n int) int64 {
	t.Helper()
	eventLog, err := store.MaterializeEventLog()
	if err != nil {
		t.Fatalf("materialize event log: %v", err)
	}
	window, err := eventLog.ReadRecentRecords(10_000)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	visible := 0
	for _, record := range window.Records {
		payload, err := record.Payload()
		if err != nil {
			t.Fatalf("read persisted event payload: %v", err)
		}
		message, ok := payload.(session.MessageRecord)
		if !ok {
			continue
		}
		if message.Role == session.MessageRoleUser {
			visible++
			if visible == n {
				return record.Seq()
			}
		}
	}
	t.Fatalf("user message %d not found among %d events", n, len(window.Records))
	return 0
}

func newTestSessionLifecycleService(root string, authManager *auth.Manager, owner *metadata.Store) *SessionLifecycleService {
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{
		PersistenceRoot:               root,
		ProjectWorkspaceContextReader: owner,
		StoreOptions:                  owner.AuthoritativeSessionStoreOptions(),
	})
	return NewGlobalSessionLifecycleService(root, authority, authManager, owner)
}

type sessionLifecycleRetargeterStub struct {
	result    metadata.SessionWorkspaceRetargetResult
	err       error
	req       metadata.SessionWorkspaceRetargetRequest
	scheduled bool
}

func (s *sessionLifecycleRetargeterStub) ScheduleWorkspaceRetarget(
	_ context.Context,
	req metadata.SessionWorkspaceRetargetRequest,
	_ *sessionlaunchpb.RuntimeStepOrigin,
	operationID worktreecontract.OperationID,
) (*worktreepb.ScheduledAcknowledgement, error) {
	s.req = req
	s.scheduled = true
	return &worktreepb.ScheduledAcknowledgement{OperationId: operationID.String()}, s.err
}

type sessionNavigationTargetResolverStub struct {
	target       *sessionlaunchpb.SessionNavigationBinding
	err          error
	mu           sync.Mutex
	calls        []string
	firstStarted chan struct{}
	releaseFirst <-chan struct{}
}

func (s *sessionNavigationTargetResolverStub) ResolveSessionNavigationBinding(ctx context.Context, sessionID string) (*sessionlaunchpb.SessionNavigationBinding, error) {
	s.mu.Lock()
	s.calls = append(s.calls, sessionID)
	first := len(s.calls) == 1
	s.mu.Unlock()
	if first && s.firstStarted != nil {
		close(s.firstStarted)
		select {
		case <-s.releaseFirst:
		case <-ctx.Done():
			return &sessionlaunchpb.SessionNavigationBinding{}, context.Cause(ctx)
		}
	}
	return s.target, s.err
}

func (s *sessionLifecycleRetargeterStub) RetargetWorkspace(_ context.Context, req metadata.SessionWorkspaceRetargetRequest) (*sessionlaunchpb.SessionRetargetWorkspaceSuccess, error) {
	s.req = req
	if s.err != nil {
		return nil, s.err
	}
	return completedWorkspaceRetargetResponse(s.result)
}

func createPersistedSession(t *testing.T) (string, string, *session.Store, *metadata.Store) {
	t.Helper()
	persistenceRoot := t.TempDir()
	sessionServiceTestPersistence := testsetup.OpenStore(t, persistenceRoot)
	workspace := t.TempDir()
	binding, err := sessionServiceTestPersistence.RegisterWorkspaceBinding(t.Context(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	containerDir := filepath.Join(persistenceRoot, "projects", binding.ProjectID, "sessions")
	store, err := session.Create(containerDir, "workspace-x", workspace, sessioncontract.SessionCategoryMain, sessionServiceTestPersistence.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	return persistenceRoot, containerDir, store, sessionServiceTestPersistence
}

func createAuthoritativeSessionLifecycleSession(t *testing.T, workspaceRoot string) (config.App, *metadata.Store, metadata.Binding, *session.Store) {
	t.Helper()
	cfg := config.App{PersistenceRoot: t.TempDir(), WorkspaceRoot: workspaceRoot}
	cfg.Settings = testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, config.DefaultOnboardingSettings())
	store := testsetup.OpenStore(t, cfg.PersistenceRoot)
	binding, err := store.RegisterWorkspaceBinding(context.Background(), cfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	sess, err := session.Create(
		filepath.Join(filepath.Join(cfg.PersistenceRoot, "projects"), binding.ProjectID, "sessions"),
		filepath.Base(cfg.WorkspaceRoot),
		cfg.WorkspaceRoot,
		sessioncontract.SessionCategoryMain,
		store.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		_ = store.Close()
		t.Fatalf("session.Create: %v", err)
	}
	if err := sess.SetName(textutil.Value("incident triage")); err != nil {
		_ = store.Close()
		t.Fatalf("SetName: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return cfg, store, binding, sess
}

func TestServiceGetInitialInputPrefersStoredDraft(t *testing.T) {
	root, _, store, sessionServiceTestPersistence := createPersistedSession(t)
	if err := store.SetInputDraft("draft from store", nil); err != nil {
		t.Fatalf("set input draft: %v", err)
	}

	service := newTestSessionLifecycleService(root, nil, sessionServiceTestPersistence)
	resp, err := service.GetInitialInput(context.Background(), &sessionlaunchpb.SessionInitialInputRequest{
		SessionId:       proto.String(store.Meta().SessionID),
		TransitionInput: "transition input",
	})
	if err != nil {
		t.Fatalf("GetInitialInput: %v", err)
	}
	if resp.Input != "draft from store" {
		t.Fatalf("input = %q, want %q", resp.Input, "draft from store")
	}
}

func TestServiceGetInitialInputOverrideReturnsOnlyExactTransitionInput(t *testing.T) {
	tests := []struct {
		name            string
		transitionInput string
	}{
		{
			name:            "byte-sensitive input",
			transitionInput: " \nExact café 👩🏽‍💻\n尾  ",
		},
		{
			name:            "intentional empty input",
			transitionInput: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, _, store, sessionServiceTestPersistence := createPersistedSession(t)
			if err := store.SetName(textutil.Value("parent session")); err != nil {
				t.Fatalf("persist parent session: %v", err)
			}
			service := newTestSessionLifecycleService(root, nil, sessionServiceTestPersistence)
			_, err := service.PersistInputDraft(context.Background(), &sessionlaunchpb.SessionPersistInputDraftRequest{
				SessionId: store.Meta().SessionID,
				Input:     "conflicting parent draft",
			})
			if err != nil {
				t.Fatalf("persist parent draft: %v", err)
			}

			resp, err := service.GetInitialInput(context.Background(), &sessionlaunchpb.SessionInitialInputRequest{
				SessionId:           proto.String(store.Meta().SessionID),
				TransitionInput:     tt.transitionInput,
				OverrideStoredDraft: true,
			})
			if err != nil {
				t.Fatalf("GetInitialInput: %v", err)
			}
			if resp.Input != tt.transitionInput {
				t.Fatalf("input = %q, want exact transition input %q", resp.Input, tt.transitionInput)
			}
		})
	}
}

func TestServiceGetInitialInputAllowsEmptySessionID(t *testing.T) {
	sessionServiceTestPersistence := testsetup.OpenStore(t, t.TempDir())
	service := newTestSessionLifecycleService(t.TempDir(), nil, sessionServiceTestPersistence)
	resp, err := service.GetInitialInput(context.Background(), &sessionlaunchpb.SessionInitialInputRequest{
		TransitionInput: "transition input",
	})
	if err != nil {
		t.Fatalf("GetInitialInput: %v", err)
	}
	if resp.Input != "transition input" {
		t.Fatalf("input = %q, want %q", resp.Input, "transition input")
	}
}

func TestServicePersistInputDraftWritesBySessionID(t *testing.T) {
	root, _, store, sessionServiceTestPersistence := createPersistedSession(t)
	if err := store.SetName(textutil.Value("session name")); err != nil {
		t.Fatalf("set session name: %v", err)
	}

	service := newTestSessionLifecycleService(root, nil, sessionServiceTestPersistence)
	if _, err := service.PersistInputDraft(context.Background(), &sessionlaunchpb.SessionPersistInputDraftRequest{
		SessionId: store.Meta().SessionID,
		Input:     "saved by service",
	}); err != nil {
		t.Fatalf("PersistInputDraft: %v", err)
	}

	reopened, err := session.Open(store.Dir(), sessionServiceTestPersistence.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("reopen session store: %v", err)
	}
	if reopened.Meta().InputDraft != "saved by service" {
		t.Fatalf("input draft = %q, want %q", reopened.Meta().InputDraft, "saved by service")
	}
}

func TestServiceRestoresAndClearsProtectedDraft(t *testing.T) {
	_, containerDir, store, sessionServiceTestPersistence := createPersistedSession(t)
	service := newTestSessionLifecycleService(containerDir, nil, sessionServiceTestPersistence)
	protected := "draft A"
	request := &sessionlaunchpb.SessionPersistInputDraftRequest{
		SessionId:      store.Meta().SessionID,
		Input:          "draft B",
		ProtectedInput: &sessionlaunchpb.ProtectedInputDraftUpdate{Text: &protected},
	}
	if _, err := service.PersistInputDraft(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	initial, err := service.GetInitialInput(t.Context(), &sessionlaunchpb.SessionInitialInputRequest{SessionId: &request.SessionId})
	if err != nil {
		t.Fatal(err)
	}
	if initial.Input != request.Input || initial.ProtectedInput == nil || *initial.ProtectedInput != protected {
		t.Fatalf("restored pair = %+v", initial)
	}
	request.Input = protected
	request.ProtectedInput = &sessionlaunchpb.ProtectedInputDraftUpdate{}
	if _, err := service.PersistInputDraft(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	initial, err = service.GetInitialInput(t.Context(), &sessionlaunchpb.SessionInitialInputRequest{SessionId: &request.SessionId})
	if err != nil {
		t.Fatal(err)
	}
	if initial.Input != protected || initial.ProtectedInput != nil {
		t.Fatalf("restored pair after clear = %+v", initial)
	}
}

func TestServiceRetargetSessionWorkspaceDelegatesAndMapsBinding(t *testing.T) {
	projectID := "project-target"
	retargeter := &sessionLifecycleRetargeterStub{result: metadata.SessionWorkspaceRetargetResult{
		Binding: metadata.Binding{
			ProjectID:       projectID,
			ProjectKey:      "TAR",
			ProjectName:     "Target",
			WorkspaceID:     "workspace-target",
			CanonicalRoot:   t.TempDir(),
			WorkspaceName:   "target",
			WorkspaceStatus: "available",
		},
		WorkspaceBindingCreated: true,
	}}
	fixture := newSessionRemovalServiceFixture(t)
	service := fixture.service.WithWorkspaceRetargeter(retargeter)
	resp, err := service.RetargetSessionWorkspace(context.Background(), &sessionlaunchpb.SessionRetargetWorkspaceRequest{
		SessionId:     requestfixture.Selector(t, "33333333-3333-4333-8333-333333333333"),
		WorkspaceRoot: retargeter.result.Binding.CanonicalRoot,
		ProjectId:     &projectID,
	})
	if err != nil {
		t.Fatalf("RetargetSessionWorkspace: %v", err)
	}
	if retargeter.req.ProjectID == nil || *retargeter.req.ProjectID != projectID {
		t.Fatalf("retarget request = %+v, want target project %q", retargeter.req, projectID)
	}
	if resp.Binding == nil || resp.Binding.ProjectId != projectID || resp.Binding.ProjectKey != "TAR" {
		t.Fatalf("binding = %+v, want mapped retarget result", resp.Binding)
	}
	if !resp.WorkspaceBindingCreated {
		t.Fatal("WorkspaceBindingCreated = false, want true")
	}
}

func TestServiceRetargetSessionWorkspacePreservesRuntimeOrigin(t *testing.T) {
	retargeter := &sessionLifecycleRetargeterStub{}
	fixture := newSessionRemovalServiceFixture(t)
	service := fixture.service.WithWorkspaceRetargeter(retargeter)
	origin := &sessionlaunchpb.RuntimeStepOrigin{
		SessionId: "33333333-3333-4333-8333-333333333333",
		RunId:     "11111111-1111-4111-8111-111111111111",
		StepId:    "22222222-2222-4222-8222-222222222222",
	}
	response, err := service.RetargetSessionWorkspace(t.Context(), &sessionlaunchpb.SessionRetargetWorkspaceRequest{
		SessionId:     requestfixture.Selector(t, "33333333-3333-4333-8333-333333333333"),
		WorkspaceRoot: t.TempDir(),
		Origin:        origin,
	})
	if err != nil {
		t.Fatalf("RetargetSessionWorkspace: %v", err)
	}
	if !retargeter.scheduled || response.Scheduled == nil || response.Binding != nil {
		t.Fatalf("scheduled response = %+v, retargeter scheduled = %t", response, retargeter.scheduled)
	}
}

func TestServiceRetargetSessionWorkspaceRequiresRetargeter(t *testing.T) {
	service := NewGlobalSessionLifecycleService(t.TempDir(), nil, nil, testsetup.OpenStore(t, t.TempDir()))
	_, err := service.RetargetSessionWorkspace(context.Background(), &sessionlaunchpb.SessionRetargetWorkspaceRequest{
		SessionId:     requestfixture.Selector(t, "33333333-3333-4333-8333-333333333333"),
		WorkspaceRoot: t.TempDir(),
	})
	if !errors.Is(err, errSessionWorkspaceRetargeterRequired) {
		t.Fatalf("RetargetSessionWorkspace error = %v, want missing retargeter", err)
	}
}

func TestServicePersistInputDraftPersistsAndDedupes(t *testing.T) {
	root, _, store, sessionServiceTestPersistence := createPersistedSession(t)
	if err := store.SetName(textutil.Value("session name")); err != nil {
		t.Fatalf("set session name: %v", err)
	}
	service := newTestSessionLifecycleService(root, nil, sessionServiceTestPersistence)
	req := &sessionlaunchpb.SessionPersistInputDraftRequest{
		SessionId: store.Meta().SessionID,
		Input:     "saved by service",
	}

	if _, err := service.PersistInputDraft(context.Background(), req); err != nil {
		t.Fatalf("PersistInputDraft first: %v", err)
	}
	if _, err := service.PersistInputDraft(context.Background(), req); err != nil {
		t.Fatalf("PersistInputDraft replay: %v", err)
	}
	reopened, err := session.Open(store.Dir(), sessionServiceTestPersistence.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("reopen session store: %v", err)
	}
	if reopened.Meta().InputDraft != "saved by service" {
		t.Fatalf("input draft = %q, want %q", reopened.Meta().InputDraft, "saved by service")
	}
}

func TestServiceResolveTransitionOpenSessionRequiresTarget(t *testing.T) {
	sessionServiceTestPersistence := testsetup.OpenStore(t, t.TempDir())
	service := newTestSessionLifecycleService(t.TempDir(), nil, sessionServiceTestPersistence)
	response, err := service.ResolveTransition(context.Background(), &sessionlaunchpb.SessionResolveTransitionRequest{
		Transition: &sessionlaunchpb.SessionTransition{
			Action: sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_OPEN_SESSION,
		},
	})
	if err == nil {
		t.Fatalf("open-session transition without target resolved as %+v", response)
	}
}

func TestServiceResolveTransitionOpenSessionAuthorizesProvenanceTargetAndReturnsBinding(t *testing.T) {
	root, containerDir, parent, sessionServiceTestPersistence := createPersistedSession(t)
	child, err := session.NewLazy(
		containerDir,
		"workspace-x",
		parent.Meta().WorkspaceRoot,
		sessioncontract.SessionCategoryMain,
		sessionServiceTestPersistence.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if err := session.InitializeCreationContext(child, parent, session.SessionCreationSourcePreviousSession, session.ChildContextOptions{}); err != nil {
		t.Fatalf("InitializeCreationContext: %v", err)
	}
	if err := child.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable child: %v", err)
	}
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	resolver := &sessionNavigationTargetResolverStub{
		target: &sessionlaunchpb.SessionNavigationBinding{
			ProjectId:   "project-target",
			WorkspaceId: "workspace-target",
		},
		firstStarted: firstStarted,
		releaseFirst: releaseFirst,
	}
	service := newTestSessionLifecycleService(root, nil, sessionServiceTestPersistence).WithNavigationTargetResolver(resolver)

	request := &sessionlaunchpb.SessionResolveTransitionRequest{
		SessionId: proto.String(child.Meta().SessionID),
		Transition: &sessionlaunchpb.SessionTransition{
			Action:          sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_OPEN_SESSION,
			TargetSessionId: proto.String(parent.Meta().SessionID),
			InitialInput:    textutil.Value("draft reply"),
		},
	}
	firstDone := make(chan error, 1)
	go func() {
		_, resolveErr := service.ResolveTransition(context.Background(), request)
		firstDone <- resolveErr
	}()
	<-firstStarted
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	response, err := service.ResolveTransition(ctx, request)
	cancel()
	if err != nil {
		t.Fatalf("duplicate ResolveTransition waited for the earlier read: %v", err)
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first ResolveTransition: %v", err)
	}
	intent, preparation := requireSessionLifecycleLaunch(t, response)
	targetID, present := intent.SessionID()
	if !present || targetID.String() != parent.Meta().SessionID {
		t.Fatalf("navigation target = %q/%t, want %q", targetID.String(), present, parent.Meta().SessionID)
	}
	binding := preparation.NavigationBinding
	if binding == nil || binding.ProjectId != "project-target" || binding.WorkspaceId != "workspace-target" {
		t.Fatalf("navigation binding = %+v", binding)
	}
	if len(resolver.calls) != 2 ||
		resolver.calls[0] != parent.Meta().SessionID ||
		resolver.calls[1] != parent.Meta().SessionID {
		t.Fatalf("target resolver calls = %#v, want parent twice", resolver.calls)
	}
}

func TestServiceResolveTransitionOpenSessionRejectsNonProvenanceTargetBeforeResolution(t *testing.T) {
	root, containerDir, parent, sessionServiceTestPersistence := createPersistedSession(t)
	child, err := session.NewLazy(
		containerDir,
		"workspace-x",
		parent.Meta().WorkspaceRoot,
		sessioncontract.SessionCategoryMain,
		sessionServiceTestPersistence.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if err := session.InitializeCreationContext(child, parent, session.SessionCreationSourcePreviousSession, session.ChildContextOptions{}); err != nil {
		t.Fatalf("InitializeCreationContext: %v", err)
	}
	if err := child.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable child: %v", err)
	}
	resolver := &sessionNavigationTargetResolverStub{}
	service := newTestSessionLifecycleService(root, nil, sessionServiceTestPersistence).WithNavigationTargetResolver(resolver)

	_, err = service.ResolveTransition(context.Background(), &sessionlaunchpb.SessionResolveTransitionRequest{
		SessionId: proto.String(child.Meta().SessionID),
		Transition: &sessionlaunchpb.SessionTransition{
			Action:          sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_OPEN_SESSION,
			TargetSessionId: proto.String("arbitrary-session"),
		},
	})
	if err == nil {
		t.Fatal("non-provenance navigation target unexpectedly succeeded")
	}
	if len(resolver.calls) != 0 {
		t.Fatalf("target resolver calls = %#v, want none", resolver.calls)
	}
}

func TestServiceResolveTransitionForkRollbackCreatesFork(t *testing.T) {
	cfg, metadataStore, _, store := createAuthoritativeSessionLifecycleSession(t, t.TempDir())
	containerDir := filepath.Dir(store.Dir())
	if err := store.SetWorkflowSnapshot(session.WorkflowSnapshot{CompletionMode: sessioncontract.WorkflowCompletionModeTool}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkModelDispatchLocked(session.LockedContract{Model: "workflow-model"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: new("workflow-agent")}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetConnectionID(config.ConnectionID("deleted-workflow-connection")); err != nil {
		t.Fatal(err)
	}
	if err := store.SetThinkingOverride(new("low")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SetGoal("finish the investigation", session.GoalActorUser); err != nil {
		t.Fatal(err)
	}
	appendSessionMessage(t, store, "step-1", session.MessageRoleUser, "u1")
	appendSessionMessage(t, store, "step-1", session.MessageRoleAssistant, "a1")
	appendSessionMessage(t, store, "step-2", session.MessageRoleUser, "u2")
	appendSessionMessage(t, store, "step-2", session.MessageRoleAssistant, "a2")

	service := newTestSessionLifecycleService(cfg.PersistenceRoot, nil, metadataStore)
	resp, err := service.ResolveTransition(context.Background(), &sessionlaunchpb.SessionResolveTransitionRequest{
		SessionId: proto.String(store.Meta().SessionID),
		Transition: &sessionlaunchpb.SessionTransition{
			Action:               sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_FORK_ROLLBACK,
			InitialPrompt:        "edited prompt",
			ForkRollbackTargetId: proto.String(rollbacktarget.EncodeUserMessageSeq(userMessageSeqAt(t, store, 2))),
		},
	})
	if err != nil {
		t.Fatalf("ResolveTransition: %v", err)
	}
	intent, preparation := requireSessionLifecycleLaunch(t, resp)
	forkID, ok := intent.SessionID()
	if !ok || forkID.String() == store.Meta().SessionID {
		t.Fatalf("unexpected fork session id %q/%v", forkID.String(), ok)
	}
	prompt := preparation.InitialPrompt
	if prompt == nil || prompt.Text != "edited prompt" {
		t.Fatalf("initial prompt = %+v, want edited prompt", prompt)
	}
	forked, err := session.Open(filepath.Join(containerDir, forkID.String()), metadataStore.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("open forked session store: %v", err)
	}
	meta := forked.Meta()
	if meta.Locked != nil || meta.WorkflowSnapshot != nil || meta.Continuation != nil || meta.ParentAgentSessionID != nil {
		t.Fatalf("interactive fork retained source execution settings: %+v", meta)
	}
	connection, err := cfg.Settings.Connection.ConcreteID()
	if err != nil {
		t.Fatal(err)
	}
	if meta.ConnectionID == nil || *meta.ConnectionID != *connection {
		t.Fatalf("interactive fork did not retain its freshly selected Connection: %v", meta.ConnectionID)
	}
	if meta.Category == nil || *meta.Category != sessioncontract.SessionCategoryMain ||
		meta.PreviousSessionID == nil || meta.PreviousSessionID.String() != store.Meta().SessionID {
		t.Fatalf("interactive fork identity/provenance = %+v", meta)
	}
	if meta.Goal == nil || meta.Goal.Objective != store.Meta().Goal.Objective {
		t.Fatalf("fork lost source Goal: %+v", meta.Goal)
	}
	taskID, err := metadataStore.WorkflowTaskIDForSession(t.Context(), forkID.String())
	if err != nil || taskID != nil {
		t.Fatalf("interactive fork Task owner = %v, %v", taskID, err)
	}
	settings, err := launch.ResolveReadOnlySessionContextSettings(cfg, meta, false)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Settings.Model != cfg.Settings.Model || settings.Settings.ThinkingLevel != "low" {
		t.Fatalf("fork did not select fresh interactive model/current Thinking: %s/%s", settings.Settings.Model, settings.Settings.ThinkingLevel)
	}
	if store.Meta().Locked == nil || store.Meta().WorkflowSnapshot == nil || store.Meta().Continuation == nil || store.Meta().ConnectionID == nil {
		t.Fatal("fork changed source settings")
	}
}

func TestServiceResolveTransitionForkRollbackUsesTargetToken(t *testing.T) {
	cfg, metadataStore, _, store := createAuthoritativeSessionLifecycleSession(t, t.TempDir())
	appendSessionMessage(t, store, "step-1", session.MessageRoleUser, "u1")
	appendSessionMessage(t, store, "step-1", session.MessageRoleAssistant, "a1")
	appendSessionMessage(t, store, "step-2", session.MessageRoleUser, "u2")
	appendSessionMessage(t, store, "step-2", session.MessageRoleAssistant, "a2")

	service := newTestSessionLifecycleService(cfg.PersistenceRoot, nil, metadataStore)
	resp, err := service.ResolveTransition(context.Background(), &sessionlaunchpb.SessionResolveTransitionRequest{
		SessionId: proto.String(store.Meta().SessionID),
		Transition: &sessionlaunchpb.SessionTransition{
			Action:               sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_FORK_ROLLBACK,
			InitialPrompt:        "edited prompt",
			ForkRollbackTargetId: proto.String(rollbacktarget.EncodeUserMessageSeq(userMessageSeqAt(t, store, 2))),
		},
	})
	if err != nil {
		t.Fatalf("ResolveTransition: %v", err)
	}
	intent, preparation := requireSessionLifecycleLaunch(t, resp)
	forkID, ok := intent.SessionID()
	if !ok {
		t.Fatal("rollback result omitted fork session ID")
	}
	if _, err := session.OpenByID(cfg.PersistenceRoot, forkID.String(), metadataStore.AuthoritativeSessionStoreOptions()...); err != nil {
		t.Fatalf("open forked session store: %v", err)
	}
	prompt := preparation.InitialPrompt
	if prompt == nil || prompt.Text != "edited prompt" {
		t.Fatalf("initial prompt = %+v, want edited prompt", prompt)
	}
}

func TestServiceForkAndCloneLeaveSourceBindingUnchanged(t *testing.T) {
	cfg, metadataStore, _, source := createAuthoritativeSessionLifecycleSession(t, t.TempDir())
	cfg.Settings.Model = "gpt-6-astra"
	selection := config.ConnectionSelection{"a", "b"}
	cfg.Settings.Connection = &selection
	cfg.Settings.ConnectionOrder = []config.ConnectionID{"a", "b"}
	cfg.Settings.Connections = map[config.ConnectionID]config.ProviderConnection{
		"a": {Protocol: config.ConnectionResponses, Endpoint: new("https://api.openai.com/v1")},
		"b": {Protocol: config.ConnectionResponses, Endpoint: new("https://example.test/v1")},
	}
	rendered, err := config.RenderSettingsTOMLForOnboarding(cfg.Settings, config.OnboardingWriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.PersistenceRoot, "config.toml"), []byte(rendered), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := source.SetConnectionID("a"); err != nil {
		t.Fatal(err)
	}
	if err := source.SetWorkflowSnapshot(session.WorkflowSnapshot{CompletionMode: sessioncontract.WorkflowCompletionModeTool}); err != nil {
		t.Fatal(err)
	}
	if err := source.MarkModelDispatchLocked(session.LockedContract{Model: cfg.Settings.Model}); err != nil {
		t.Fatal(err)
	}
	if err := source.SetThinkingOverride(new("low")); err != nil {
		t.Fatal(err)
	}
	if err := source.AdoptOriginalThinkingEffort("xhigh"); err != nil {
		t.Fatal(err)
	}
	appendSessionMessage(t, source, "step-1", session.MessageRoleUser, "original input")
	log, err := source.MaterializeEventLog()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := log.AppendRecord(nil, session.ConfigurationUpdateRecord{Item: session.ProviderHistoryItem{
		Type: session.ProviderHistoryItemTypeConfigurationUpdate, ConfigurationEffort: new("low"),
		Raw: []byte(`{"type":"configuration_update","reasoning":{"effort":"low"}}`),
	}}); err != nil {
		t.Fatal(err)
	}
	target := appendSessionMessage(t, source, "step-2", session.MessageRoleUser, "edit this")
	rotation := new(launch.ConnectionRotation)
	if allocated, err := rotation.Prepare(cfg.Settings, true); err != nil || allocated != "a" {
		t.Fatalf("initial allocation = %s, %v", allocated, err)
	}
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{
		PersistenceRoot: cfg.PersistenceRoot, StoreOptions: metadataStore.AuthoritativeSessionStoreOptions(),
		ConnectionRotation: rotation,
	})
	t.Cleanup(func() {
		if err := authority.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	service := NewGlobalSessionLifecycleService(cfg.PersistenceRoot, authority, nil, metadataStore).WithConnectionRotation(rotation)
	directive, err := service.ResolveTransition(t.Context(), &sessionlaunchpb.SessionResolveTransitionRequest{
		SessionId: new(source.Meta().SessionID),
		Transition: &sessionlaunchpb.SessionTransition{
			Action:               sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_FORK_ROLLBACK,
			ForkRollbackTargetId: new(rollbacktarget.EncodeUserMessageSeq(target.Seq())),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	intent, _ := requireSessionLifecycleLaunch(t, directive)
	forkID, _ := intent.SessionID()
	record, err := metadataStore.ResolvePersistedSession(t.Context(), forkID.String())
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.ConnectionID == nil || *record.Meta.ConnectionID != "b" || record.Meta.OriginalThinkingEffort != nil {
		t.Fatalf("fork did not project Thinking against its allocated custom Connection: %+v", record.Meta)
	}
	forked, err := session.Open(record.SessionDir, metadataStore.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	forkLog, err := forked.MaterializeEventLog()
	if err != nil {
		t.Fatal(err)
	}
	window, err := forkLog.ReadRecentRecords(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(window.Records) != 1 {
		t.Fatalf("unsupported native update remained in fork history: %d records", len(window.Records))
	}
	cloneThinking, err := launch.ResolveForkThinking(cfg, source.Meta(), true)
	if err != nil {
		t.Fatal(err)
	}
	cloned, err := session.CloneSession(log, new("workflow clone"), sessioncontract.SessionCategoryMain, cloneThinking)
	if err != nil {
		t.Fatal(err)
	}
	if cloned.Meta().ConnectionID != nil || cloned.Meta().WorkflowSnapshot != nil ||
		cloned.Meta().Locked != nil || cloned.Meta().OriginalThinkingEffort == nil || !cloneThinking.PreserveNativeUpdates {
		t.Fatalf("new clone retained source execution settings or lost native history: %+v", cloned.Meta())
	}
	if source.Meta().ConnectionID == nil || *source.Meta().ConnectionID != "a" || source.Meta().OriginalThinkingEffort == nil {
		t.Fatal("fork altered source binding or Thinking history")
	}
}

func TestServiceResolveTransitionForkRollbackPreservesExecutionTarget(t *testing.T) {
	workspaceRoot := t.TempDir()
	cfg, metadataStore, binding, sess := createAuthoritativeSessionLifecycleSession(t, workspaceRoot)
	appendSessionMessage(t, sess, "step-1", session.MessageRoleUser, "u1")
	appendSessionMessage(t, sess, "step-1", session.MessageRoleAssistant, "a1")
	appendSessionMessage(t, sess, "step-2", session.MessageRoleUser, "u2")
	appendSessionMessage(t, sess, "step-2", session.MessageRoleAssistant, "a2")

	worktreeRoot := filepath.Join(t.TempDir(), "feature-a")
	if err := os.MkdirAll(filepath.Join(worktreeRoot, "pkg"), 0o755); err != nil {
		t.Fatalf("mkdir worktree pkg: %v", err)
	}
	if err := metadataStore.UpsertWorktreeRecord(context.Background(), metadata.WorktreeRecord{
		ID:            "wt-1",
		WorkspaceID:   binding.WorkspaceID,
		CanonicalRoot: worktreeRoot,
		DisplayName:   "feature-a",
		Availability:  "available",
	}); err != nil {
		t.Fatalf("UpsertWorktreeRecord: %v", err)
	}
	if err := metadataStore.UpdateSessionExecutionTarget(context.Background(), metadata.SessionExecutionTargetUpdate{SessionID: sess.Meta().SessionID, Workspace: &metadata.SessionExecutionTargetUpdateWorkspace{ID: binding.WorkspaceID}, Worktree: &metadata.SessionExecutionTargetUpdateWorktree{ID: "wt-1"}, CwdRelpath: "pkg"}); err != nil {
		t.Fatalf("UpdateSessionExecutionTarget: %v", err)
	}

	service := newTestSessionLifecycleService(cfg.PersistenceRoot, nil, metadataStore)
	resp, err := service.ResolveTransition(context.Background(), &sessionlaunchpb.SessionResolveTransitionRequest{
		SessionId: proto.String(sess.Meta().SessionID),
		Transition: &sessionlaunchpb.SessionTransition{
			Action:               sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_FORK_ROLLBACK,
			InitialPrompt:        "edited prompt",
			ForkRollbackTargetId: proto.String(rollbacktarget.EncodeUserMessageSeq(userMessageSeqAt(t, sess, 2))),
		},
	})
	if err != nil {
		t.Fatalf("ResolveTransition: %v", err)
	}

	intent, _ := requireSessionLifecycleLaunch(t, resp)
	forkID, ok := intent.SessionID()
	if !ok {
		t.Fatal("rollback result omitted fork session ID")
	}
	target, err := metadataStore.ResolveSessionExecutionTarget(context.Background(), forkID.String())
	if err != nil {
		t.Fatalf("ResolveSessionExecutionTarget: %v", err)
	}
	canonicalWorktreeRoot, err := config.CanonicalWorkspaceRoot(worktreeRoot)
	if err != nil {
		t.Fatalf("CanonicalWorkspaceRoot: %v", err)
	}
	if target.Worktree == nil || target.Worktree.Id != "wt-1" {
		t.Fatalf("fork target worktree = %+v, want wt-1", target.Worktree)
	}
	if target.Worktree == nil || target.Worktree.Root != canonicalWorktreeRoot {
		t.Fatalf("fork target worktree root = %+v, want %q", target.Worktree, canonicalWorktreeRoot)
	}
	if target.CwdRelpath != "pkg" {
		t.Fatalf("fork target cwd_relpath = %q, want pkg", target.CwdRelpath)
	}
	if target.EffectiveWorkdir != filepath.Join(canonicalWorktreeRoot, "pkg") {
		t.Fatalf("fork effective workdir = %q, want %q", target.EffectiveWorkdir, filepath.Join(canonicalWorktreeRoot, "pkg"))
	}
}

func TestServiceResolveTransitionForkRollbackActivatesChildInPreservedWorktree(t *testing.T) {
	workspaceRoot := t.TempDir()
	cfg, metadataStore, binding, sess := createAuthoritativeSessionLifecycleSession(t, workspaceRoot)
	appendSessionMessage(t, sess, "step-1", session.MessageRoleUser, "u1")
	appendSessionMessage(t, sess, "step-1", session.MessageRoleAssistant, "a1")
	appendSessionMessage(t, sess, "step-2", session.MessageRoleUser, "u2")
	appendSessionMessage(t, sess, "step-2", session.MessageRoleAssistant, "a2")

	worktreeRoot := filepath.Join(t.TempDir(), "feature-a")
	if err := os.MkdirAll(filepath.Join(worktreeRoot, "pkg"), 0o755); err != nil {
		t.Fatalf("mkdir worktree pkg: %v", err)
	}
	if err := metadataStore.UpsertWorktreeRecord(context.Background(), metadata.WorktreeRecord{
		ID:            "wt-1",
		WorkspaceID:   binding.WorkspaceID,
		CanonicalRoot: worktreeRoot,
		DisplayName:   "feature-a",
		Availability:  "available",
	}); err != nil {
		t.Fatalf("UpsertWorktreeRecord: %v", err)
	}
	if err := metadataStore.UpdateSessionExecutionTarget(context.Background(), metadata.SessionExecutionTargetUpdate{SessionID: sess.Meta().SessionID, Workspace: &metadata.SessionExecutionTargetUpdateWorkspace{ID: binding.WorkspaceID}, Worktree: &metadata.SessionExecutionTargetUpdateWorktree{ID: "wt-1"}, CwdRelpath: "pkg"}); err != nil {
		t.Fatalf("UpdateSessionExecutionTarget: %v", err)
	}

	lifecycle := newTestSessionLifecycleService(cfg.PersistenceRoot, nil, metadataStore)
	resolved, err := lifecycle.ResolveTransition(context.Background(), &sessionlaunchpb.SessionResolveTransitionRequest{
		SessionId: proto.String(sess.Meta().SessionID),
		Transition: &sessionlaunchpb.SessionTransition{
			Action:               sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_FORK_ROLLBACK,
			InitialPrompt:        "edited prompt",
			ForkRollbackTargetId: proto.String(rollbacktarget.EncodeUserMessageSeq(userMessageSeqAt(t, sess, 2))),
		},
	})
	if err != nil {
		t.Fatalf("ResolveTransition: %v", err)
	}
	intent, _ := requireSessionLifecycleLaunch(t, resolved)
	forkID, ok := intent.SessionID()
	if !ok {
		t.Fatal("rollback result omitted fork session ID")
	}

	runtimeAuthority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{
		PersistenceRoot: cfg.PersistenceRoot,
		StoreOptions:    metadataStore.AuthoritativeSessionStoreOptions(),
	})
	t.Cleanup(func() {
		if err := runtimeAuthority.Close(context.Background()); err != nil {
			t.Errorf("close runtime authority: %v", err)
		}
	})
	runtimeService := sessionruntime.NewAPI(metadataStore, runtimeAuthority, sessionruntime.APIOptions{})
	activateSettings := cfg.Settings
	activateSettings.Model = "gpt-6-sol"
	activateSettings.ThinkingLevel = "medium"
	activateSettings.Reviewer.Frequency = "off"
	activateSettings = testsetup.WithResponsesProvider(activateSettings, "http://127.0.0.1:1/v1")
	activateSettings.Shell.PostprocessingMode = config.ShellPostprocessingModeBuiltin
	activation, err := runtimeService.ActivateSessionRuntime(context.Background(), serverapi.SessionRuntimeActivateRequest{
		SessionID:             requestfixture.SessionID(t, forkID.String()),
		OwnerID:               "test-owner",
		ActiveSettings:        activateSettings,
		QuestionsEnabled:      textutil.Value(true),
		AutoCompactionEnabled: textutil.Value(true),
		Source:                config.SourceReport{},
	})
	if err != nil {
		t.Fatalf("ActivateSessionRuntime: %v", err)
	}
	if _, err := runtimeService.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
		Attachment: activation,
		OwnerID:    "test-owner",
	}); err != nil {
		t.Fatalf("ReleaseSessionRuntime: %v", err)
	}

	childStore, err := session.OpenByID(cfg.PersistenceRoot, forkID.String(), metadataStore.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("OpenByID child: %v", err)
	}
	logBody, err := os.ReadFile(session.RunLogPath(childStore.Dir()))
	if err != nil {
		t.Fatalf("ReadFile steps.log: %v", err)
	}
	canonicalWorktreeRoot, err := config.CanonicalWorkspaceRoot(worktreeRoot)
	if err != nil {
		t.Fatalf("CanonicalWorkspaceRoot: %v", err)
	}
	wantWorkdir := filepath.Join(canonicalWorktreeRoot, "pkg")
	if !strings.Contains(string(logBody), "app.interactive.start") {
		t.Fatalf("expected activation log entry, got %q", string(logBody))
	}
	if !strings.Contains(string(logBody), "workdir="+wantWorkdir) {
		t.Fatalf("expected activation workdir %q in log, got %q", wantWorkdir, string(logBody))
	}
}

func TestServiceResolveTransitionForkRollbackRejectsInvalidTargetToken(t *testing.T) {
	root, _, store, sessionServiceTestPersistence := createPersistedSession(t)
	appendSessionMessage(t, store, "step-1", session.MessageRoleUser, "u1")
	appendSessionMessage(t, store, "step-1", session.MessageRoleAssistant, "a1")

	service := newTestSessionLifecycleService(root, nil, sessionServiceTestPersistence)
	_, err := service.ResolveTransition(context.Background(), &sessionlaunchpb.SessionResolveTransitionRequest{
		SessionId: proto.String(store.Meta().SessionID),
		Transition: &sessionlaunchpb.SessionTransition{
			Action:               sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_FORK_ROLLBACK,
			ForkRollbackTargetId: proto.String("not-valid"),
		},
	})
	if !errors.Is(err, rollbacktarget.ErrInvalidRollbackTargetID) {
		t.Fatalf("expected invalid target token rejection, got %v", err)
	}
}

func TestServiceGetInitialInputResolvesSessionAcrossProjects(t *testing.T) {
	root := t.TempDir()
	sessionServiceTestPersistence := testsetup.OpenStore(t, root)
	workspace := t.TempDir()
	binding, err := sessionServiceTestPersistence.RegisterWorkspaceBinding(t.Context(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	containerB := filepath.Join(root, "projects", binding.ProjectID, "sessions")
	store, err := session.Create(
		containerB,
		"workspace-b",
		workspace,
		sessioncontract.SessionCategoryMain,
		sessionServiceTestPersistence.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("create foreign session store: %v", err)
	}
	if err := store.SetInputDraft("foreign draft", nil); err != nil {
		t.Fatalf("set foreign input draft: %v", err)
	}

	service := newTestSessionLifecycleService(root, nil, sessionServiceTestPersistence)
	response, err := service.GetInitialInput(context.Background(), &sessionlaunchpb.SessionInitialInputRequest{SessionId: proto.String(store.Meta().SessionID)})
	if err != nil {
		t.Fatalf("GetInitialInput: %v", err)
	}
	if response.Input != "foreign draft" {
		t.Fatalf("input = %q, want stored draft", response.Input)
	}
}

func TestServicePersistInputDraftResolvesSessionAcrossProjects(t *testing.T) {
	root := t.TempDir()
	sessionServiceTestPersistence := testsetup.OpenStore(t, root)
	workspace := t.TempDir()
	binding, err := sessionServiceTestPersistence.RegisterWorkspaceBinding(t.Context(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	containerB := filepath.Join(root, "projects", binding.ProjectID, "sessions")
	store, err := session.Create(
		containerB,
		"workspace-b",
		workspace,
		sessioncontract.SessionCategoryMain,
		sessionServiceTestPersistence.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("create foreign session store: %v", err)
	}
	if err := store.SetName(textutil.Value("foreign session")); err != nil {
		t.Fatalf("persist foreign session meta: %v", err)
	}

	service := newTestSessionLifecycleService(root, nil, sessionServiceTestPersistence)
	_, err = service.PersistInputDraft(context.Background(), &sessionlaunchpb.SessionPersistInputDraftRequest{
		SessionId: store.Meta().SessionID,
		Input:     "updated draft",
	})
	if err != nil {
		t.Fatalf("PersistInputDraft: %v", err)
	}
	response, err := service.GetInitialInput(context.Background(), &sessionlaunchpb.SessionInitialInputRequest{SessionId: proto.String(store.Meta().SessionID)})
	if err != nil {
		t.Fatalf("GetInitialInput: %v", err)
	}
	if response.Input != "updated draft" {
		t.Fatalf("input = %q, want updated draft", response.Input)
	}
}

func TestServiceResolveTransitionLogoutUsesSessionIDWithoutStoreLookup(t *testing.T) {
	currentID := runtimeids.NewSessionID()
	mgr := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil)

	service := newTestSessionLifecycleService(t.TempDir(), mgr, testsetup.OpenStore(t, t.TempDir()))

	resp, err := service.ResolveTransition(context.Background(), &sessionlaunchpb.SessionResolveTransitionRequest{
		SessionId: proto.String(currentID.String()),
		Transition: &sessionlaunchpb.SessionTransition{
			Action: sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_LOGOUT,
		},
	})
	if err != nil {
		t.Fatalf("ResolveTransition logout: %v", err)
	}
	intent, preparation := requireSessionLifecycleLaunch(t, resp)
	sessionID, ok := intent.SessionID()
	if !ok || sessionID != currentID {
		t.Fatalf("next session id = %q/%v, want %q", sessionID.String(), ok, currentID.String())
	}
	if preparation.Auth != sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_REAUTHENTICATE {
		t.Fatalf("auth preparation = %v, want reauthenticate", preparation.Auth)
	}
	state, err := mgr.Load(context.Background())
	if err != nil {
		t.Fatalf("load auth state: %v", err)
	}
	if len(state.Connections) != 0 {
		t.Fatalf("logout changed credentials: %+v", state)
	}
}

func TestServiceResolveTransitionLogoutReturnsStableDirective(t *testing.T) {
	mgr := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil)

	service := newTestSessionLifecycleService(t.TempDir(), mgr, testsetup.OpenStore(t, t.TempDir()))
	req := &sessionlaunchpb.SessionResolveTransitionRequest{
		SessionId:  proto.String(runtimeids.NewSessionID().String()),
		Transition: &sessionlaunchpb.SessionTransition{Action: sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_LOGOUT},
	}

	firstResp, err := service.ResolveTransition(context.Background(), req)
	if err != nil {
		t.Fatalf("ResolveTransition first: %v", err)
	}
	secondResp, err := service.ResolveTransition(context.Background(), req)
	if err != nil {
		t.Fatalf("ResolveTransition second replay: %v", err)
	}
	_, preparation := requireSessionLifecycleLaunch(t, firstResp)
	if preparation.Auth != sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_REAUTHENTICATE {
		t.Fatalf("auth preparation = %v, want reauthenticate", preparation.Auth)
	}
	requireSessionDirectiveWireEqual(t, secondResp, firstResp)
}
