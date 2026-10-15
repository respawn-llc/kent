package app

import (
	"context"
	"io"
	"testing"

	"core/shared/apicontract"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/serverapi"
)

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
		origin: model, session: &chatpb.ExistingSessionTarget{SessionId: plan.SessionID},
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

func TestNewSessionKnownFailureRetainsIdentityAndVisibleDraft(t *testing.T) {
	model := newProjectedStaticUIModel()
	model.presentationTarget = &uiNewSessionTarget{creation: &chatpb.NewChatTarget{}}
	model.mainEditor.SetText("newer draft")
	id := runtimeids.NewSessionID()
	model.pendingNewChatActions = 1
	model.Update(newSessionActionDoneMsg{
		origin: model, token: 1, action: newSessionInputAction{},
		session:       &chatpb.ExistingSessionTarget{SessionId: id.String()},
		submittedText: appStringPointer("rejected input"), err: io.ErrUnexpectedEOF,
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
			origin: model, token: 1, action: newSessionInputAction{},
			session: &chatpb.ExistingSessionTarget{SessionId: id.String()},
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
		origin: origin, token: 1, action: newSessionInputAction{},
		session: &chatpb.ExistingSessionTarget{SessionId: runtimeids.NewSessionID().String()},
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
		origin: model, token: 1, action: newSessionGoalAction{objective: "objective"},
		session: &chatpb.ExistingSessionTarget{SessionId: id.String()},
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
	model.Update(model.loadNewSessionSettingsCmd(nil)())
	client := &concurrentNewChatClient{}
	model.chatMutations = client
	first := model.inputController().submitCmd("first", runtimeinput.Text("first"), "", activeSubmitOriginDirect, nil)
	model.newChatTarget().Name = appStringPointer("second name")
	second := model.inputController().submitCmd("second", runtimeinput.Text("second"), "", activeSubmitOriginDirect, nil)
	second()
	first()
	if len(client.requests) != 2 {
		t.Fatalf("first requests = %d, want 2", len(client.requests))
	}
	if client.requests[0].Activation.GetText() != "second" || client.requests[1].Activation.GetText() != "first" {
		t.Fatal("pending requests lost their captured input")
	}
	if client.requests[0].Target.GetNewChat().GetName() != "second name" || client.requests[1].Target.GetNewChat().Name != nil {
		t.Fatal("pending requests shared mutable creation inputs")
	}
	if model.activeSubmit.token != 0 {
		t.Fatal("independent New requests replaced the singleton submission")
	}
}
