package app

import (
	"context"
	"core/server/metadata"
	"core/server/sessionstore"
	"core/shared/apicontract"
	"core/shared/config"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"

	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"os"
	"path/filepath"
	"testing"
)

func sessionLifecycleStringPtr(value string) *string { return &value }

func sessionLifecycleSessionID(t *testing.T, raw string) runtimeids.SessionID {
	t.Helper()
	sessionID, err := runtimeids.ParseSessionID(raw)
	if err != nil {
		t.Fatalf("ParseSessionID(%q): %v", raw, err)
	}
	return sessionID
}

func createAttachedAuthoritativeAppSession(t *testing.T, persistenceRoot, projectID, workspaceRoot string) *session.Store {
	t.Helper()
	metadataStore, err := metadata.Open(persistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	if _, err := metadataStore.AttachWorkspaceToProject(context.Background(), projectID, workspaceRoot); err != nil {
		_ = metadataStore.Close()
		t.Fatalf("AttachWorkspaceToProject: %v", err)
	}
	store, err := session.Create(
		filepath.Join(persistenceRoot, "projects", projectID, "sessions"),
		filepath.Base(filepath.Clean(workspaceRoot)),
		workspaceRoot,
		sessioncontract.SessionCategoryMain,
		metadataStore.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		_ = metadataStore.Close()
		t.Fatalf("session.Create: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		_ = metadataStore.Close()
		t.Fatalf("EnsureDurable: %v", err)
	}
	t.Cleanup(func() { _ = metadataStore.Close() })
	return store
}

func TestMaybeHandlePickedSessionWorkspaceChangeCanonicalizesAliases(t *testing.T) {
	realRoot := t.TempDir()
	aliasRoot := filepath.Join(t.TempDir(), "workspace-link")
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	originalPrompt := runWorkspaceChangePromptFlow
	defer func() { runWorkspaceChangePromptFlow = originalPrompt }()
	promptCalls := 0
	runWorkspaceChangePromptFlow = func(string, string, string) (workspaceChangePromptResult, error) {
		promptCalls++
		return workspaceChangePromptResult{Rebind: true}, nil
	}

	action, err := maybeHandlePickedSessionWorkspaceChange(
		context.Background(),
		&remoteAppServer{
			connection: config.Connection{WorkspaceRoot: aliasRoot},
			retarget:   &sessionWorkspaceRetargetContext{workspaceRoot: aliasRoot, theme: "dark"}},
		"session-1", &worktreepb.SessionExecutionTarget{
			WorkspaceRoot:         realRoot,
			WorkspaceAvailability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_AVAILABLE})
	if err != nil {
		t.Fatalf("maybeHandlePickedSessionWorkspaceChange: %v", err)
	}
	if action != sessionWorkspaceChangeProceed || promptCalls != 0 {
		t.Fatalf("action/prompt calls = %v/%d, want proceed/0", action, promptCalls)
	}
}

func TestMaybeHandlePickedSessionWorkspaceChangeUsesRemoteServerBindingRoot(t *testing.T) {
	originalPrompt := runWorkspaceChangePromptFlow
	defer func() { runWorkspaceChangePromptFlow = originalPrompt }()
	var promptedCurrentRoot string
	runWorkspaceChangePromptFlow = func(_ string, currentRoot string, _ string) (workspaceChangePromptResult, error) {
		promptedCurrentRoot = currentRoot
		return workspaceChangePromptResult{}, nil
	}

	action, err := maybeHandlePickedSessionWorkspaceChange(
		context.Background(),
		&remoteAppServer{
			connection: config.Connection{WorkspaceRoot: "/source-client-workspace"},
			retarget:   &sessionWorkspaceRetargetContext{workspaceRoot: "/active-server-workspace", theme: "dark"}},
		"session-1", &worktreepb.SessionExecutionTarget{
			WorkspaceRoot:         "/target-server-workspace",
			WorkspaceAvailability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_AVAILABLE})
	if err != nil {
		t.Fatalf("maybeHandlePickedSessionWorkspaceChange: %v", err)
	}
	if action != sessionWorkspaceChangePickAgain || promptedCurrentRoot != "/active-server-workspace" {
		t.Fatalf("action/current root = %v/%q", action, promptedCurrentRoot)
	}
}

func TestMaybeHandlePickedSessionWorkspaceChangeRejectsMissingBindingContext(t *testing.T) {
	_, err := maybeHandlePickedSessionWorkspaceChange(
		context.Background(),
		narrowSessionLifecycleServer{},
		"session-1", &worktreepb.SessionExecutionTarget{
			WorkspaceRoot:         "/target-server-workspace",
			WorkspaceAvailability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_AVAILABLE})
	if err == nil {
		t.Fatal("expected missing workspace retarget context error")
	}
}

func TestResolveSessionActionPreservesInitialPromptHistoryRecorded(t *testing.T) {
	client := &recordingSessionLifecycleClient{
		resolveTransition: func(_ context.Context, req *sessionlaunchpb.SessionResolveTransitionRequest) (*sessionlaunchpb.SessionDirective, error) {
			if !req.Transition.InitialPromptHistoryRecorded {
				t.Fatal("expected transition request to preserve initial prompt-history flag")
			}
			prompt := &sessionlaunchpb.SessionInitialPromptMetadata{
				Text:            req.Transition.InitialPrompt,
				HistoryRecorded: req.Transition.InitialPromptHistoryRecorded,
			}
			directive, err := defaultSessionLaunchDirective(serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()))
			if err != nil {
				return nil, err
			}
			directive.GetLaunch().Preparation.InitialPrompt = prompt
			return directive, nil
		},
	}

	resolved, err := resolveSessionAction(
		context.Background(),
		narrowSessionLifecycleServer{lifecycle: client},
		nil,
		"session-1",
		UITransition{Action: UIActionNewSession, InitialPrompt: "expanded prompt", InitialPromptHistoryRecorded: true},
	)
	if err != nil {
		t.Fatalf("resolve session action: %v", err)
	}
	preparation := resolved.GetLaunch().GetPreparation()
	if preparation == nil {
		t.Fatal("resolved transition omitted launch preparation")
	}
	prompt := preparation.InitialPrompt
	if prompt == nil || !prompt.HistoryRecorded {
		t.Fatal("expected resolved transition to preserve initial prompt-history flag")
	}
}

func TestPersistSessionDraftIncludesOnlyComposerInput(t *testing.T) {
	var captured *sessionlaunchpb.SessionPersistInputDraftRequest
	var saveErr error
	client := &recordingSessionLifecycleClient{
		persistInputDraft: func(_ context.Context, req *sessionlaunchpb.SessionPersistInputDraftRequest) (*emptypb.Empty, error) {
			captured = req
			return &emptypb.Empty{}, saveErr
		},
	}
	model := newUIModelDefaults(nil)
	model.sessionID = "session-1"
	model.drafts = newUIDraftPersistence(client, model.sessionID, model.mainEditor.Text())
	testSetMainInput(model, "visible draft")
	model.injectedQueue = []*injectedRuntimeQueueItem{{

		Text:            "  pending injected\n",
		State:           injectedRuntimeQueuePendingCreate,
		submissionOrder: inputSubmissionOrder{sequence: 1},
	}}
	model.queued = queuedInputsForTest("\tqueued later  ")

	cmd := model.saveEditedDraft()
	if cmd == nil || captured != nil {
		t.Fatal("editing must schedule persistence without waiting for it")
	}
	testSetMainInput(model, "newer draft")
	if next := model.saveEditedDraft(); next != nil {
		t.Fatal("editing during a save must coalesce behind the in-flight write")
	}
	done := cmd().(draftSaveDoneMsg)
	if done.err != nil {
		t.Fatalf("persist draft: %v", done.err)
	}
	if captured.Input != "visible draft" || captured.SessionId != "session-1" {
		t.Fatalf("captured draft request = %+v, want composer input only", captured)
	}
	model.finishDraftSave(done)
	next := model.saveEditedDraft()
	if next == nil {
		t.Fatal("latest edit was not scheduled after the previous save finished")
	}
	done = next().(draftSaveDoneMsg)
	if done.err != nil {
		t.Fatalf("persist latest draft: %v", done.err)
	}
	if captured.Input != "newer draft" {
		t.Fatalf("saved draft = %q, want newest editor content", captured.Input)
	}
	model.finishDraftSave(done)
	saveErr = errors.New("draft storage unavailable")
	testSetMainInput(model, "unsaved draft")
	failed := model.saveEditedDraft()().(draftSaveDoneMsg)
	if !errors.Is(failed.err, saveErr) {
		t.Fatalf("save failure = %v, want %v", failed.err, saveErr)
	}
	model.finishDraftSave(failed)
	if model.mainEditor.Text() != "unsaved draft" || model.saveEditedDraft() != nil {
		t.Fatal("failed save must preserve the editor without automatic replay")
	}
	model.Close()
}

func TestRuntimeCloseStopsObservationOnce(t *testing.T) {
	stops := 0
	plan := &runtimeLaunchPlan{stopEventStreams: func() { stops++ }}
	plan.Close()
	plan.Close()
	if stops != 1 {
		t.Fatalf("observation stops = %d, want one", stops)
	}
}

func TestReopenRetargetedSessionDoesNotSaveDraftOnNavigation(t *testing.T) {
	sourceRemoteOpen := true
	released := false
	var persistedDraft string
	sourceLifecycle := &recordingSessionLifecycleClient{
		persistInputDraft: func(_ context.Context, req *sessionlaunchpb.SessionPersistInputDraftRequest) (*emptypb.Empty, error) {
			if released {
				return nil, errors.New("source runtime was released before draft persistence")
			}
			persistedDraft = req.Input
			return &emptypb.Empty{}, nil
		},
	}
	var server *reattachSessionLifecycleServer
	server = &reattachSessionLifecycleServer{
		lifecycle: sourceLifecycle,
		reattach: func(context.Context, string) error {
			if !released {
				return errors.New("source runtime owner is still attached")
			}
			sourceRemoteOpen = false
			return nil
		},
	}
	runtimePlan := &runtimeLaunchPlan{stopEventStreams: func() {
		if !sourceRemoteOpen {
			t.Error("source remote closed before observation stopped")
		}
		released = true
	}}
	sessionID := "11111111-1111-4111-8111-111111111111"
	next, err := reopenRetargetedSession(
		context.Background(),
		server,
		runtimePlan,
		sessionID,
	)
	if err != nil {
		t.Fatalf("reopen retargeted Session: %v", err)
	}
	if !released || sourceRemoteOpen {
		t.Fatalf("handoff state released=%t source_remote_open=%t", released, sourceRemoteOpen)
	}
	if persistedDraft != "" {
		t.Fatalf("navigation unexpectedly persisted draft %q", persistedDraft)
	}
	if target := requireSessionOpenDestination(t, next); target != sessionID {
		t.Fatalf("reopen destination = %q, want %q", target, sessionID)
	}
}

func TestReopenRetargetedSessionReportsReattachmentFailureWithoutSavingDraft(t *testing.T) {
	reattachErr := errors.New("destination unavailable")
	var persistedDraft string
	server := &reattachSessionLifecycleServer{
		lifecycle: &recordingSessionLifecycleClient{
			persistInputDraft: func(_ context.Context, req *sessionlaunchpb.SessionPersistInputDraftRequest) (*emptypb.Empty, error) {
				persistedDraft = req.Input
				return &emptypb.Empty{}, nil
			},
		},
		reattach: func(context.Context, string) error {
			return reattachErr
		},
	}
	released := false
	runtimePlan := &runtimeLaunchPlan{stopEventStreams: func() {
		released = true
	}}
	_, err := reopenRetargetedSession(
		context.Background(),
		server,
		runtimePlan,
		"11111111-1111-4111-8111-111111111111",
	)
	if !errors.Is(err, reattachErr) {
		t.Fatalf("reopen error = %v, want %v", err, reattachErr)
	}
	if !released {
		t.Fatal("source runtime was not released")
	}
	if persistedDraft != "" {
		t.Fatalf("failed navigation unexpectedly persisted draft %q", persistedDraft)
	}
}

type narrowSessionLifecycleServer struct {
	lifecycle      apicontract.SessionLifecycleService
	reauthenticate func(context.Context, authInteractor) error
}

func (s narrowSessionLifecycleServer) SessionLifecycleClient() apicontract.SessionLifecycleService {
	return s.lifecycle
}

func (s narrowSessionLifecycleServer) Reauthenticate(ctx context.Context) error {
	if s.reauthenticate == nil {
		return nil
	}
	return s.reauthenticate(ctx, nil)
}

type reattachSessionLifecycleServer struct {
	lifecycle           apicontract.SessionLifecycleService
	reattachedLifecycle apicontract.SessionLifecycleService
	reattach            func(context.Context, string) error
}

func (s *reattachSessionLifecycleServer) SessionLifecycleClient() apicontract.SessionLifecycleService {
	return s.lifecycle
}

func (s *reattachSessionLifecycleServer) ReattachSession(ctx context.Context, sessionID string) error {
	if err := s.reattach(ctx, sessionID); err != nil {
		return err
	}
	if s.reattachedLifecycle != nil {
		s.lifecycle = s.reattachedLifecycle
	}
	return nil
}

type recordingSessionLifecycleClient struct {
	getInitialInput          func(context.Context, *sessionlaunchpb.SessionInitialInputRequest) (*sessionlaunchpb.SessionInitialInputSuccess, error)
	persistInputDraft        func(context.Context, *sessionlaunchpb.SessionPersistInputDraftRequest) (*emptypb.Empty, error)
	retargetSessionWorkspace func(context.Context, *sessionlaunchpb.SessionRetargetWorkspaceRequest) (*sessionlaunchpb.SessionRetargetWorkspaceSuccess, error)
	resolveTransition        func(context.Context, *sessionlaunchpb.SessionResolveTransitionRequest) (*sessionlaunchpb.SessionDirective, error)
}

func (c *recordingSessionLifecycleClient) Close() error { return nil }

func (c *recordingSessionLifecycleClient) Promote(context.Context, *sessionlaunchpb.SessionPromoteRequest) (*emptypb.Empty, error) {
	return nil, errors.New("unexpected Promote call")
}

func (c *recordingSessionLifecycleClient) GetInitialInput(ctx context.Context, req *sessionlaunchpb.SessionInitialInputRequest) (*sessionlaunchpb.SessionInitialInputSuccess, error) {
	if c.getInitialInput == nil {
		return nil, errors.New("unexpected GetInitialInput call")
	}
	return c.getInitialInput(ctx, req)
}

func (c *recordingSessionLifecycleClient) PersistInputDraft(ctx context.Context, req *sessionlaunchpb.SessionPersistInputDraftRequest) (*emptypb.Empty, error) {
	if c.persistInputDraft == nil {
		return nil, errors.New("unexpected PersistInputDraft call")
	}
	return c.persistInputDraft(ctx, req)
}

func (c *recordingSessionLifecycleClient) RetargetSessionWorkspace(ctx context.Context, req *sessionlaunchpb.SessionRetargetWorkspaceRequest) (*sessionlaunchpb.SessionRetargetWorkspaceSuccess, error) {
	if c.retargetSessionWorkspace == nil {
		return &sessionlaunchpb.SessionRetargetWorkspaceSuccess{}, errors.New("unexpected RetargetSessionWorkspace call")
	}
	return c.retargetSessionWorkspace(ctx, req)
}

func (c *recordingSessionLifecycleClient) ResolveTransition(ctx context.Context, req *sessionlaunchpb.SessionResolveTransitionRequest) (*sessionlaunchpb.SessionDirective, error) {
	if c.resolveTransition == nil {
		return &sessionlaunchpb.SessionDirective{}, errors.New("unexpected ResolveTransition call")
	}
	return c.resolveTransition(ctx, req)
}

func (*recordingSessionLifecycleClient) ArchiveSession(context.Context, *sessionlaunchpb.SessionArchiveRequest) (*sessionlaunchpb.SessionArchiveSuccess, error) {
	return nil, errors.New("unexpected ArchiveSession call")
}

func (*recordingSessionLifecycleClient) DeleteSession(context.Context, *sessionlaunchpb.SessionDeleteRequest) (*sessionlaunchpb.SessionDeleteSuccess, error) {
	return nil, errors.New("unexpected DeleteSession call")
}
