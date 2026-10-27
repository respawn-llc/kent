package app

import (
	"context"
	"core/cli/app/commands"
	"io"
	"os/exec"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/shared/apicontract"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/serverapi"
	tea "github.com/charmbracelet/bubbletea"
)

func TestNewSessionConfirmedWorktreeEntersBeforeObservation(t *testing.T) {
	newAppTestHome(t)
	workspace := initStatusLineGitRepo(t, "main")
	commit := exec.Command("git", "-C", workspace, "-c", "user.name=Kent", "-c", "user.email=kent@example.com", "commit", "--allow-empty", "-m", "initial")
	if output, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("initial commit: %v: %s", err, output)
	}
	registerAppWorkspace(t, workspace)
	writeAppTestSettings(t)
	t.Setenv("REMOTE_TEST_KEY", "ready-key")
	fixture := startRemoteAuthTestFixture(t, workspace)
	server, err := ensureInteractiveProjectBinding(t.Context(), fixture.server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	request, err := prepareNewSessionUIRun(t.Context(), server, nil, serverapi.RunPromptOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	composition, err := composeUIProgram(request, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer composition.close()
	model := composition.model
	model.Update(newSessionSettingsReadResult(t, model.loadNewSessionSettingsCmd(nil)))
	// The observation command is intentionally not executed.
	model.observeSession = nil
	model.worktrees.open = true
	model.worktrees.phase = uiWorktreeOverlayPhaseCreate
	branch, base := "created-before-observation", "HEAD"
	cmd := model.worktreeCreateCmd(&worktreepb.CreateRequest{Spec: &worktreepb.CreateSpec{
		CreateBranch: true, BranchName: &branch, BaseRef: &base,
	}})
	var created worktreeCreateDoneMsg
	results := make(chan tea.Msg, 16)
	for _, child := range cmd().(tea.BatchMsg) {
		go func() { results <- child() }()
	}
	wait := time.NewTimer(5 * time.Second)
	defer wait.Stop()
creation:
	for {
		select {
		case msg := <-results:
			if result, ok := msg.(worktreeCreateDoneMsg); ok {
				created = result
				break creation
			}
		case <-wait.C:
			t.Fatal("confirmed Worktree creation did not complete")
		}
	}
	if created.err != nil || created.session == nil {
		t.Fatalf("create: %v", created.err)
	}
	_, followup := model.Update(created)
	for _, msg := range collectCmdMessages(t, followup) {
		if result, ok := msg.(worktreeSwitchDoneMsg); ok && result.err != nil {
			t.Fatalf("Enter: %v", result.err)
		}
		model.Update(msg)
	}
	deadline := time.After(3 * time.Second)
	id, err := runtimeids.ParseSessionID(created.session.SessionId)
	if err != nil {
		t.Fatal(err)
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		view, err := server.SessionViewClient().GetSessionMainView(t.Context(), &sessionpb.MainViewRequest{SessionId: protoapi.SessionSelectorToProto(id)})
		if err != nil {
			t.Fatal(err)
		}
		if view.MainView.Session.ExecutionTarget.GetWorktree().GetId() == created.resp.Worktree.Topology.GetRegistered().Kent.WorktreeId {
			list := model.requestWorktreeListCmd()().(worktreeListDoneMsg)
			if list.err != nil || len(list.workspace.Worktrees) < 2 {
				t.Fatalf("workspace management after creation without observation: %v", list.err)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("created Session did not enter its Worktree")
		case <-tick.C:
		}
	}
}

type sessionPromptHistoryClient struct{ apicontract.SessionViewService }

func (sessionPromptHistoryClient) GetPromptHistory(_ context.Context, request *sessionpb.PromptHistoryRequest) (*sessionpb.PromptHistorySuccess, error) {
	return &sessionpb.PromptHistorySuccess{Prompts: []string{request.SessionId.GetSessionId()}}, nil
}

func TestNewSessionAdoptionLoadsMetadataWithoutTranscriptHydration(t *testing.T) {
	model := newProjectedStaticUIModel()
	defer model.Close()
	id := runtimeids.NewSessionID()
	name := "Dormant adopted Session"
	reads := &countingSessionViewClient{view: &runtimepb.MainView{
		Session: &runtimepb.SessionView{SessionId: id.String(), SessionName: &name},
		Status:  &runtimepb.Status{},
	}}
	client := newUIRuntimeClientWithReads(id.String(), reads)
	model.setKnownSessionTarget(id.String())
	model.observationScope, model.cancelObservation = context.WithCancel(t.Context())
	model.startupCmds = append(model.startupCmds, model.installSessionObservation(&runtimeWiring{
		runtimeClient: client, eventDispatcher: newUIEventDispatcher(nil), terminalFocus: newTerminalFocusState(),
	}, sessionLaunchPlan{}))
	results := make(chan tea.Msg, 16)
	var dispatch func(tea.Cmd)
	dispatch = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		go func() {
			message := cmd()
			if batch, ok := message.(tea.BatchMsg); ok {
				for _, nested := range batch {
					dispatch(nested)
				}
			} else if message != nil {
				results <- message
			}
		}()
	}
	for _, cmd := range model.startupCmds {
		dispatch(cmd)
	}
	for {
		select {
		case msg := <-results:
			if _, ok := msg.(runtimeMainViewRefreshedMsg); !ok {
				continue
			}
			model.Update(msg)
			if model.sessionName == nil || *model.sessionName != name {
				t.Fatal("adopted Session metadata was not loaded independently")
			}
			return
		case <-time.After(3 * time.Second):
			t.Fatal("adoption waited for transcript hydration before loading metadata")
		}
	}
}

func TestNewSessionOldObservationHistoryCannotReplaceCurrentRecall(t *testing.T) {
	model := newProjectedStaticUIModel()
	model.statusConfig.SessionViews = sessionPromptHistoryClient{}
	model.observeSession = func(context.Context, runtimeids.SessionID) (sessionLaunchPlan, *runtimeLaunchPlan, error) {
		return sessionLaunchPlan{}, nil, io.ErrUnexpectedEOF
	}
	first, second := runtimeids.NewSessionID(), runtimeids.NewSessionID()
	model.setKnownSessionTarget(first.String())
	model.observeAdoptedSessionCmd()
	oldHistory := model.loadPromptHistoryCmd()()
	model.stopSessionObservation()
	model.setKnownSessionTarget(second.String())
	model.observeAdoptedSessionCmd()
	currentHistory := model.loadPromptHistoryCmd()()
	model.Update(currentHistory)
	model.Update(oldHistory)
	if len(model.promptHistory) != 1 || model.promptHistory[0] != second.String() {
		t.Fatalf("current prompt recall replaced by old observation: %v", model.promptHistory)
	}
	model.stopSessionObservation()
}

func TestNewSessionDisconnectIsVisibleWithoutObservation(t *testing.T) {
	model := newProjectedStaticUIModel()
	model.observeRuntimeRequestResult(io.EOF)
	if !model.runtimeDisconnectStatusVisible() {
		t.Fatal("disconnect was hidden without an observation")
	}
	model.observeRuntimeRequestResult(nil)
	if model.runtimeDisconnectStatusVisible() {
		t.Fatal("successful request did not clear disconnect")
	}
}

func TestNewSessionOldConnectionFailureDoesNotAffectCurrentObservation(t *testing.T) {
	model := newProjectedStaticUIModel()
	model.observeSession = func(context.Context, runtimeids.SessionID) (sessionLaunchPlan, *runtimeLaunchPlan, error) {
		return sessionLaunchPlan{}, nil, io.ErrUnexpectedEOF
	}
	model.setKnownSessionTarget(runtimeids.NewSessionID().String())
	model.observeAdoptedSessionCmd()
	events := make(chan runtimeConnectionStateChangedMsg, 1)
	events <- runtimeConnectionStateChangedMsg{err: io.EOF}
	old := waitRuntimeConnectionStateChange(events, model.observationScope)()
	model.stopSessionObservation()
	model.setKnownSessionTarget(runtimeids.NewSessionID().String())
	model.observeAdoptedSessionCmd()
	model.Update(old)
	if model.runtimeDisconnectedState() {
		t.Fatal("old observation marked current Session disconnected")
	}
	model.stopSessionObservation()
}

func TestNewSessionGoalUsesOrdinaryRemoteAdmission(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)
	t.Setenv("REMOTE_TEST_KEY", "ready-key")
	fixture := startRemoteAuthTestFixture(t, workspace)
	server, err := ensureInteractiveProjectBinding(t.Context(), fixture.server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	request, err := prepareNewSessionUIRun(t.Context(), server, nil, serverapi.RunPromptOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	composition, err := composeUIProgram(request, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer composition.close()
	model := composition.model
	model.Update(newSessionSettingsReadResult(t, model.loadNewSessionSettingsCmd(nil)))
	model.mainEditor.SetText("unsent draft")
	cmd := model.newSessionActionCmd(chatGoalAction{objective: "Report current directory"}, nil)
	result := cmd().(newSessionActionDoneMsg)
	if result.err != nil || result.session == nil {
		t.Fatalf("first Goal: session=%v error=%v", result.session, result.err)
	}
	model.Update(result)
	if model.mainEditor.Text() != "unsent draft" || model.newSessionTarget() != nil {
		t.Fatal("Goal did not retain the editor and adopt its Session")
	}
	known := model.goalRuntimeCommand(goalRuntimeSet, "Report the current directory again")().(goalRuntimeDoneMsg)
	if known.err != nil || known.setResult == nil || known.setResult.GetMutation() == nil {
		t.Fatalf("known unobserved Goal: result=%v error=%v", known.setResult, known.err)
	}
	model.Update(known)
	for _, step := range []struct {
		operation goalRuntimeOperation
		status    runtimepb.GoalStatus
	}{
		{goalRuntimePause, runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_PAUSED},
		{goalRuntimeResume, runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE},
	} {
		result := model.goalRuntimeCommand(step.operation, "")().(goalRuntimeDoneMsg)
		if result.err != nil || result.mutation == nil {
			t.Fatalf("unobserved operation %v: mutation=%v error=%v", step.operation, result.mutation, result.err)
		}
		model.Update(result)
		shown := model.goalRuntimeCommand(goalRuntimeShow, "")().(goalRuntimeDoneMsg)
		if shown.err != nil || shown.goal.GetGoal().GetStatus() != step.status {
			t.Fatalf("unobserved persisted Goal: goal=%v error=%v", shown.goal, shown.err)
		}
	}
	cleared := model.goalRuntimeCommand(goalRuntimeClear, "")().(goalRuntimeDoneMsg)
	if cleared.err != nil || cleared.mutation == nil {
		t.Fatalf("unobserved clear: mutation=%v error=%v", cleared.mutation, cleared.err)
	}
	model.Update(cleared)
	shown := model.goalRuntimeCommand(goalRuntimeShow, "")().(goalRuntimeDoneMsg)
	if shown.err != nil || goalIsPresent(shown.goal) {
		t.Fatalf("cleared Goal still present: goal=%v error=%v", shown.goal, shown.err)
	}
}

func TestNewSessionConfirmedWorktreeCreateKeepsSessionOnSetupFailure(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)
	t.Setenv("REMOTE_TEST_KEY", "ready-key")
	fixture := startRemoteAuthTestFixture(t, workspace)
	server, err := ensureInteractiveProjectBinding(t.Context(), fixture.server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	request, err := prepareNewSessionUIRun(t.Context(), server, nil, serverapi.RunPromptOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	composition, err := composeUIProgram(request, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer composition.close()
	model := composition.model
	model.Update(newSessionSettingsReadResult(t, model.loadNewSessionSettingsCmd(nil)))
	model.mainEditor.SetText("draft across setup failure")
	model.newChatTarget().Name = appStringPointer("future Worktree Session")
	model.observeSession = nil
	worktrees := &worktreeCommandTestClient{createErr: io.ErrUnexpectedEOF}
	model.worktreeClient = worktrees
	model.worktrees.open = true
	model.worktrees.phase = uiWorktreeOverlayPhaseCreate
	cmd := model.worktreeCreateCmd(&worktreepb.CreateRequest{})
	var result *worktreeCreateDoneMsg
	for _, msg := range collectCmdMessages(t, cmd) {
		if done, ok := msg.(worktreeCreateDoneMsg); ok {
			result = &done
		}
	}
	if result == nil || result.session == nil || result.err == nil {
		t.Fatal("setup failure did not deliver its created Session")
	}
	model.Update(*result)
	if model.currentRuntimeSessionID() != result.session.SessionId || model.mainEditor.Text() != "draft across setup failure" {
		t.Fatal("setup failure lost the created Session or visible draft")
	}
	if len(worktrees.createRequests) != 1 || worktrees.createRequests[0].Scope.GetWorkspace().GetTarget().GetWorkspaceId() != request.newChat.WorkspaceId {
		t.Fatal("Worktree create did not run once against its workspace")
	}
	stored, err := sessionLaunchInitialStateFromServer(t.Context(), server, result.session.SessionId, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Input != model.mainEditor.Text() {
		t.Fatal("ordinary creation lost the initial visible draft")
	}
}

func TestNewSessionAdoptionPersistsVisibleDraftWithoutMergingStoredInput(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)
	t.Setenv("REMOTE_TEST_KEY", "ready-key")
	fixture := startRemoteAuthTestFixture(t, workspace)
	server, err := ensureInteractiveProjectBinding(t.Context(), fixture.server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	intent, err := protoapi.SessionLaunchRequestIntentToProto(serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := newSessionLaunchPlanner(server).PlanSession(t.Context(), sessionLaunchRequest{Mode: launchModeInteractive, Intent: intent})
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.SessionLifecycleClient().PersistInputDraft(t.Context(), &sessionlaunchpb.SessionPersistInputDraftRequest{SessionId: plan.SessionID, Input: "initial stored input"})
	if err != nil {
		t.Fatal(err)
	}
	request, err := prepareNewSessionUIRun(t.Context(), server, nil, serverapi.RunPromptOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	composition, err := composeUIProgram(request, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer composition.close()
	model := composition.model
	visible := " \nnewer visible draft\n "
	model.mainEditor.SetText(visible)
	model.pendingNewChatActions = 1
	// Drive observation independently of the indefinite subscription wait.
	model.applyNewSessionActionDone(newSessionActionDoneMsg{
		origin: model, chatActionResult: chatActionResult{session: &chatpb.ExistingSessionTarget{SessionId: plan.SessionID}},
	})
	id, err := runtimeids.ParseSessionID(plan.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	scope := t.Context()
	observedPlan, observation, err := request.observeSession(scope, id)
	model.applySessionObservationLoaded(sessionObservationLoadedMsg{
		scope: scope, sessionID: id, plan: observedPlan, observation: observation, err: err,
	})
	if err != nil {
		t.Fatal(err)
	}
	if model.mainEditor.Text() != visible {
		t.Fatal("observation merged stored input into the visible editor")
	}
	cmd := model.saveEditedDraft()
	if cmd == nil {
		t.Fatal("adoption did not make the visible draft eligible for saving")
	}
	model.Update(cmd())
	stored, err := sessionLaunchInitialStateFromServer(t.Context(), server, plan.SessionID, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Input != visible {
		t.Fatalf("restored draft = %q, want %q", stored.Input, visible)
	}
}

type concurrentNewChatClient struct {
	apicontract.ChatMutationService
	requests []*chatpb.SteerRequest
}

func TestNewSessionNavigationPromptWaitsForSettingsThenDispatchesOnce(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)
	t.Setenv("REMOTE_TEST_KEY", "ready-key")
	fixture := startRemoteAuthTestFixture(t, workspace)
	server, err := ensureInteractiveProjectBinding(t.Context(), fixture.server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	request, err := prepareNewSessionUIRun(t.Context(), server, nil, serverapi.RunPromptOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	request.initialPrompt = "navigation prompt"
	composition, err := composeUIProgram(request, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer composition.close()
	model := composition.model
	client := &concurrentNewChatClient{}
	model.chatMutations = client
	model.mainEditor.SetText("typing after navigation")
	if model.startupSubmitCmd() != nil || len(client.requests) != 0 {
		t.Fatal("navigation prompt dispatched before settings were ready")
	}
	_, cmd := model.Update(newSessionSettingsReadResult(t, model.loadNewSessionSettingsCmd(nil)))
	collectCmdMessages(t, cmd)
	if len(client.requests) != 1 || client.requests[0].Activation.GetText() != request.initialPrompt {
		t.Fatal("settings readiness did not dispatch the explicit navigation prompt once")
	}
	if model.mainEditor.Text() != "typing after navigation" || model.startupSubmitCmd() != nil {
		t.Fatal("navigation prompt replaced newer text or remained eligible for replay")
	}
}

func TestNewSessionKnownFailureRetainsIdentityAndVisibleDraft(t *testing.T) {
	model := newProjectedStaticUIModel()
	model.presentationTarget = &uiNewSessionTarget{creation: &chatpb.NewChatTarget{}}
	model.mainEditor.SetText("newer draft")
	id := runtimeids.NewSessionID()
	model.pendingNewChatActions = 1
	model.Update(newSessionActionDoneMsg{
		origin:           model,
		chatActionResult: chatActionResult{session: &chatpb.ExistingSessionTarget{SessionId: id.String()}, err: io.ErrUnexpectedEOF},
		submittedText:    appStringPointer("rejected input"),
	})
	if model.currentRuntimeSessionID() != id.String() {
		t.Fatal("known failed result lost its Session identity")
	}
	if got := model.mainEditor.Text(); got != "newer draft\n\nrejected input" {
		t.Fatalf("visible draft = %q", got)
	}
}

func TestNewSessionDeliveredResultsAdoptInOrderWithoutReplacingDraft(t *testing.T) {
	model := newProjectedStaticUIModel()
	model.presentationTarget = &uiNewSessionTarget{creation: &chatpb.NewChatTarget{}}
	model.mainEditor.SetText("newer\nvisible draft")
	model.pendingNewChatActions = 2
	for range 2 {
		id := runtimeids.NewSessionID()
		model.Update(newSessionActionDoneMsg{
			origin: model, chatActionResult: chatActionResult{session: &chatpb.ExistingSessionTarget{SessionId: id.String()}},
		})
		if model.currentRuntimeSessionID() != id.String() {
			t.Fatal("delivered result did not adopt its Session")
		}
		if got := model.mainEditor.Text(); got != "newer\nvisible draft" {
			t.Fatalf("adoption replaced visible draft %q", got)
		}
	}
}

func TestNewSessionLateResultDoesNotNavigateReplacement(t *testing.T) {
	origin := newProjectedStaticUIModel()
	origin.presentationTarget = &uiNewSessionTarget{creation: &chatpb.NewChatTarget{}}
	origin.exitAction = UIActionResume
	origin.pendingNewChatActions = 1
	replacement := newProjectedStaticUIModel()
	replacement.presentationTarget = &uiNewSessionTarget{creation: &chatpb.NewChatTarget{}}
	result := newSessionActionDoneMsg{
		origin: origin, chatActionResult: chatActionResult{session: &chatpb.ExistingSessionTarget{SessionId: runtimeids.NewSessionID().String()}},
	}
	origin.Update(result)
	replacement.Update(result)
	if origin.newSessionTarget() == nil || replacement.newSessionTarget() == nil {
		t.Fatal("late result navigated an abandoned or replacement composer")
	}
}

func TestKnownSessionAfterObservationFailureDoesNotCreateOrReplay(t *testing.T) {
	model := newProjectedStaticUIModel()
	model.presentationTarget = &uiNewSessionTarget{creation: &chatpb.NewChatTarget{}}
	client := &concurrentNewChatClient{}
	model.chatMutations = client
	model.observeSession = func(context.Context, runtimeids.SessionID) (sessionLaunchPlan, *runtimeLaunchPlan, error) {
		return sessionLaunchPlan{}, nil, io.ErrUnexpectedEOF
	}
	id := runtimeids.NewSessionID()
	model.pendingNewChatActions = 1
	_, cmd := model.Update(newSessionActionDoneMsg{
		origin: model, chatActionResult: chatActionResult{session: &chatpb.ExistingSessionTarget{SessionId: id.String()}},
	})
	for _, msg := range collectCmdMessages(t, cmd) {
		model.Update(msg)
	}
	if model.currentRuntimeSessionID() != id.String() || len(client.requests) != 0 {
		t.Fatal("observation failure lost identity or replayed a mutation")
	}
	model.inputController().submitCmd("explicit next input", runtimeinput.Text("explicit next input"), "", activeSubmitOriginDirect, nil)()
	if len(client.requests) != 1 || client.requests[0].Target.GetSession().GetSessionId().GetSessionId() != id.String() {
		t.Fatal("explicit next action did not target the known Session")
	}
}

func (client *concurrentNewChatClient) Steer(_ context.Context, request *chatpb.SteerRequest) (*chatpb.InputMutationSuccess, error) {
	client.requests = append(client.requests, request)
	return nil, io.ErrUnexpectedEOF
}

func TestNewSessionFirstRequestsCaptureIndependentInputs(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)
	t.Setenv("REMOTE_TEST_KEY", "ready-key")
	fixture := startRemoteAuthTestFixture(t, workspace)
	server, err := ensureInteractiveProjectBinding(t.Context(), fixture.server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	request, err := prepareNewSessionUIRun(t.Context(), server, nil, serverapi.RunPromptOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	composition, err := composeUIProgram(request, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer composition.close()
	model := composition.model
	model.Update(newSessionSettingsReadResult(t, model.loadNewSessionSettingsCmd(nil)))
	client := &concurrentNewChatClient{}
	model.chatMutations = client
	first := model.inputController().submitCmd("first", runtimeinput.Text("first"), "", activeSubmitOriginDirect, nil)
	model.newChatTarget().Name = appStringPointer("second name")
	second := model.inputController().submitCmd("second", runtimeinput.Text("second"), "", activeSubmitOriginDirect, nil)
	second()
	firstResult := first()
	if len(client.requests) != 2 {
		t.Fatalf("first requests = %d, want 2", len(client.requests))
	}
	if client.requests[0].Activation.GetText() != "second" || client.requests[1].Activation.GetText() != "first" {
		t.Fatal("pending requests lost their captured input")
	}
	if client.requests[0].Target.GetNewChat().GetName() != "second name" || client.requests[1].Target.GetNewChat().Name != nil {
		t.Fatal("pending requests shared mutable creation inputs")
	}
	_, fresh := model.inputController().applyCommandResultWithPreSubmitQueuePosition(commands.Result{
		SubmitUser: true, FreshConversation: true, User: "fresh prompt",
	}, preSubmitQueueBack)
	for _, msg := range collectCmdMessages(t, fresh) {
		if _, done := msg.(newSessionActionDoneMsg); done {
			break
		}
	}
	if len(client.requests) != 3 || model.exitAction != UIActionNone {
		t.Fatal("fresh prompt navigated away from New instead of dispatching its first action")
	}
	model.Update(firstResult)
	if model.newSessionTarget() == nil || len(client.requests) != 3 || model.mainEditor.Text() != "first" {
		t.Fatal("identityless failure lost New, replayed a request, or failed to restore rejected input")
	}
}

func TestNewSessionKeySubmissionsBeforeFirstResultCreateIndependentSessions(t *testing.T) {
	for _, secondKey := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: keyTypeCtrlEnterCSI}} {
		t.Run(secondKey.String(), func(t *testing.T) {
			_, workspace := newRegisteredAppWorkspace(t)
			fixture := startRemoteAuthTestFixture(t, workspace)
			provider, _ := newFakeResponsesServer(t, []string{"completed first", "completed second"})
			defer provider.Close()
			fixture.config.Settings = testsetup.WithResponsesProvider(fixture.config.Settings, provider.URL)
			testsetup.WriteProviderSettings(t, fixture.config.PersistenceRoot, fixture.config.Settings)
			server, err := ensureInteractiveProjectBinding(t.Context(), fixture.server)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = server.Close() }()
			request, err := prepareNewSessionUIRun(t.Context(), server, nil, serverapi.RunPromptOverrides{})
			if err != nil {
				t.Fatal(err)
			}
			composition, err := composeUIProgram(request, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			defer composition.close()
			model := composition.model
			model.Update(newSessionSettingsReadResult(t, model.loadNewSessionSettingsCmd(nil)))
			dispatch := func(text string, key tea.KeyMsg) newSessionActionDoneMsg {
				t.Helper()
				testSetMainInput(model, text)
				_, command := model.Update(key)
				for _, message := range collectCmdMessages(t, command) {
					if result, ok := message.(newSessionActionDoneMsg); ok {
						if result.err != nil || result.session == nil {
							t.Fatalf("dispatch %q: session=%v error=%v", text, result.session, result.err)
						}
						return result
					}
				}
				t.Fatalf("key did not dispatch New action for %q", text)
				return newSessionActionDoneMsg{}
			}
			first := dispatch("first input", tea.KeyMsg{Type: tea.KeyEnter})
			if model.newSessionTarget() == nil {
				t.Fatal("Session adopted before action result delivery")
			}
			second := dispatch("second input", secondKey)
			if first.session.SessionId == second.session.SessionId {
				t.Fatal("independent first key submissions shared a Session")
			}
			model.mainEditor.SetText("newer unsent text")
			model.Update(second)
			model.Update(first)
			if model.currentRuntimeSessionID() != first.session.SessionId || model.mainEditor.Text() != "newer unsent text" {
				t.Fatal("result delivery order or newer draft was lost")
			}
		})
	}
}
