package app

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"core/shared/apicontract"
	"core/shared/protoapi"
	chatcontextpb "core/shared/protoapi/gen/kent/api/chat_context"
	settingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/serverapi"

	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/proto"
)

type agentPickerHistoryClient struct {
	runtimeControlFakeClient
	started chan struct{}
	release chan struct{}
}

func (c *agentPickerHistoryClient) RecordPromptHistory(ctx context.Context, _ *promptpb.RecordHistoryRequest) error {
	close(c.started)
	select {
	case <-c.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestAgentAliasesEnterWhileHistoryPending(t *testing.T) {
	for _, alias := range []string{"/agent", "/model"} {
		t.Run(alias, func(t *testing.T) {
			model := busyCommandTestModel()
			model.applyChatSettingsProjection(runtimeControlFakeChatSettings())
			client := &agentPickerHistoryClient{started: make(chan struct{}), release: make(chan struct{})}
			model.sessionControls = client
			testSetMainInput(model, alias)
			_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			entered := make(chan struct{}, 1)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			program := tea.NewProgram(busyWorktreeCommandHarness{command: command},
				tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer(),
				tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
					if msg == tea.EnterAltScreen() {
						entered <- struct{}{}
					}
					return msg
				}))
			finished := make(chan error, 1)
			go func() {
				_, err := program.Run()
				finished <- err
			}()
			defer func() {
				close(client.release)
				program.Quit()
				if err := <-finished; err != nil {
					t.Error(err)
				}
			}()
			select {
			case <-client.started:
			case <-ctx.Done():
				t.Fatal("prompt-history persistence did not start")
			}
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("Alternate Screen entry waited for pending prompt-history persistence")
			}
		})
	}
}

func TestAgentAliasesOpenImmediatelyWhileBusy(t *testing.T) {
	for _, alias := range []string{"/agent", "/model"} {
		t.Run(alias, func(t *testing.T) {
			model := busyCommandTestModel()
			model.setKnownSessionTarget(detailTestSessionID)
			model.applyChatSettingsProjection(runtimeControlFakeChatSettings())
			model.chatSettings = nil
			testSetMainInput(model, alias)
			next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			collectCmdMessages(t, command)
			updated := next.(*uiModel)
			if updated.surface() != uiSurfaceAgent || !updated.isBusy() {
				t.Fatalf("surface = %v, busy = %t", updated.surface(), updated.isBusy())
			}
			requireBusyCommandQueuesEmpty(t, updated)
		})
	}
}

type agentPickerCatalogClient struct {
	apicontract.ChatSettingsService
	err error
}

func (c *agentPickerCatalogClient) ReadChatSettings(ctx context.Context, request *settingspb.ReadRequest) (*settingspb.ReadSuccess, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.ChatSettingsService.ReadChatSettings(ctx, request)
}

func TestAgentPickerNewChatFailureRetry(t *testing.T) {
	t.Setenv("REMOTE_TEST_KEY", "ready-key")
	fixture := startRemoteAuthTestFixture(t)
	server, err := ensureInteractiveProjectBinding(t.Context(), fixture.server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	for _, applying := range []bool{false, true} {
		for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeyRunes, Runes: []rune{'r'}}} {
			t.Run(key.String()+"/"+map[bool]string{false: "load", true: "apply"}[applying], func(t *testing.T) {
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
				client := &agentPickerCatalogClient{ChatSettingsService: server.ChatSettingsClient()}
				model.chatSettings = client
				if !applying {
					client.err = errors.New("catalog failed")
				}
				model.Update(newSessionSettingsReadResult(t, model.loadNewSessionSettingsCmd(nil)))
				testSetMainInput(model, "/agent")
				model.Update(tea.KeyMsg{Type: tea.KeyEnter})
				if applying {
					client.err = errors.New("reload failed")
					model.Update(tea.KeyMsg{Type: tea.KeyDown})
					_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
					for _, msg := range collectCmdMessages(t, command) {
						model.Update(msg)
					}
				}
				if model.surface() != uiSurfaceAgent || model.surfaceHasPendingOperation() {
					t.Fatal("failure did not leave an interactive picker")
				}
				client.err = nil
				_, command := model.Update(key)
				for _, msg := range collectCmdMessages(t, command) {
					model.Update(msg)
				}
				if _, ready := model.newSessionTarget().settingsLoad.(uiNewSettingsReady); !ready {
					t.Fatal("retry did not load the catalog")
				}
				if applying && !model.surface().isTranscript() {
					t.Fatal("successful apply retry did not close")
				}
				if !applying && model.surface() != uiSurfaceAgent {
					t.Fatal("successful load retry did not retain the list")
				}
			})
		}
	}
}

func TestAgentPickerAdoptedSessionLoadFailureRetry(t *testing.T) {
	disableTransientStatusClearForTest(t)
	t.Setenv("REMOTE_TEST_KEY", "ready-key")
	fixture := startRemoteAuthTestFixture(t)
	server, err := ensureInteractiveProjectBinding(t.Context(), fixture.server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeyRunes, Runes: []rune{'r'}}} {
		t.Run(key.String(), func(t *testing.T) {
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
			loadErr := errors.New("Session settings load failed")
			model.observeSession = func(ctx context.Context, id runtimeids.SessionID) (sessionLaunchPlan, *runtimeLaunchPlan, error) {
				if loadErr != nil {
					return sessionLaunchPlan{}, nil, loadErr
				}
				return request.observeSession(ctx, id)
			}
			created := model.newSessionInputCmd("$ true", runtimeinput.Shell("true"), newSessionInputEnter)().(newSessionActionDoneMsg)
			if created.err != nil {
				t.Fatal(created.err)
			}
			_, loading := model.Update(created)
			testSetMainInput(model, "/agent")
			model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			for _, msg := range collectCmdMessages(t, loading) {
				model.Update(msg)
			}
			if model.surface() != uiSurfaceAgent || model.surfaceHasPendingOperation() || model.agentPickerError() == nil {
				t.Fatal("adopted Session load failure did not leave a retryable picker")
			}
			loadErr = nil
			_, retry := model.Update(key)
			for _, msg := range collectCmdMessages(t, retry) {
				model.Update(msg)
			}
			if model.sessionSettings == nil || model.sessionObservationLoading() || model.surface() != uiSurfaceAgent {
				t.Fatal("retry did not finish loading the adopted Session")
			}
			model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if !model.surface().isTranscript() {
				t.Fatal("loaded current Agent was not selectable")
			}
		})
	}
}

func TestAgentPickerExistingSessionOpeningLoadFailureRetry(t *testing.T) {
	disableTransientStatusClearForTest(t)
	t.Setenv("REMOTE_TEST_KEY", "ready-key")
	fixture := startRemoteAuthTestFixture(t)
	server, err := ensureInteractiveProjectBinding(t.Context(), fixture.server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	planner := newSessionLaunchPlanner(server)
	create, err := protoapi.SessionLaunchRequestIntentToProto(serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()))
	if err != nil {
		t.Fatal(err)
	}
	created, err := planner.PlanSession(t.Context(), sessionLaunchRequest{Mode: launchModeInteractive, Intent: create})
	if err != nil {
		t.Fatal(err)
	}
	id, err := runtimeids.ParseSessionID(created.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	open, err := protoapi.SessionLaunchRequestIntentToProto(serverapi.OpenExistingSessionLaunchIntent(id))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeyRunes, Runes: []rune{'r'}}} {
		t.Run(key.String(), func(t *testing.T) {
			failed := newSessionLaunchPlanner(failingHeaderSettingsServer{launchPlannerServer: server, err: errors.New("settings read failed")})
			plan, err := failed.PlanSession(t.Context(), sessionLaunchRequest{Mode: launchModeInteractive, Intent: open})
			if err != nil {
				t.Fatalf("settings failure prevented opening the selected Session: %v", err)
			}
			observation, request, err := prepareSessionUIRun(t.Context(), server, planner, plan, "", false, "", false)
			if err != nil {
				t.Fatal(err)
			}
			defer observation.Close()
			composition, err := composeUIProgram(request, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			defer composition.close()
			model := composition.model
			testSetMainInput(model, "/agent")
			model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if model.surface() != uiSurfaceAgent || model.surfaceHasPendingOperation() || model.agentPickerError() == nil {
				t.Fatal("opening settings failure did not remain retryable in the selected Session")
			}
			_, retry := model.Update(key)
			for _, msg := range collectCmdMessages(t, retry) {
				model.Update(msg)
			}
			if model.sessionSettings == nil || model.currentRuntimeSessionID() != id.String() || model.agentPickerError() != nil {
				t.Fatal("retry did not load settings for the original Session")
			}
			model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if !model.surface().isTranscript() {
				t.Fatal("loaded current Agent was not selectable")
			}
		})
	}
}

type agentPickerSettingsClient struct {
	runtimeControlFakeClient
	settings *settingspb.Settings
	applies  int
	applyErr error
	rejected bool
}

func (c *agentPickerSettingsClient) MutateChatSettings(_ context.Context, request *settingspb.MutationRequest) (*settingspb.MutationSuccess, error) {
	c.applies++
	if c.applyErr != nil {
		return nil, c.applyErr
	}
	settings := proto.Clone(c.settings).(*settingspb.Settings)
	settings.SelectedAgent = &settingspb.AgentSummary{Role: request.Operation.GetAgentRole(), Model: "replacement-model", Thinking: "high"}
	settings.CachingLocked = false
	response := &settingspb.MutationSuccess{
		Settings: settings,
		Context:  &chatcontextpb.Context{},
		Result:   &settingspb.MutationResult{Outcome: &settingspb.MutationResult_Applied{Applied: &settingspb.MutationApplied{Changed: true}}},
	}
	if c.rejected {
		response.Settings = proto.Clone(c.settings).(*settingspb.Settings)
		response.Settings.AgentChoices[0], response.Settings.AgentChoices[1] = response.Settings.AgentChoices[1], response.Settings.AgentChoices[0]
		response.Result.Outcome = &settingspb.MutationResult_Rejected{Rejected: &settingspb.MutationRejected{
			Reason: settingspb.MutationRejectionReason_MUTATION_REJECTION_REASON_AGENT_UNAVAILABLE,
		}}
	}
	return response, nil
}

func agentPickerSessionModel() (*uiModel, *agentPickerSettingsClient) {
	settings := runtimeControlFakeChatSettings()
	settings.AgentEditability = settingspb.Editability_EDITABILITY_EDITABLE
	settings.CachingLocked = true
	settings.AgentChoices = []*settingspb.AgentChoice{{Role: "default"}, {Role: "fast"}}
	client := &agentPickerSettingsClient{settings: settings}
	model := newProjectedTestUIModel(client)
	model.setKnownSessionTarget(detailTestSessionID)
	model.chatSettings = client
	model.applyChatSettingsProjection(settings)
	return model, client
}

func TestAgentPickerEnterAppliesOnce(t *testing.T) {
	model, client := agentPickerSessionModel()
	testSetMainInput(model, "/agent")
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, apply := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, repeated := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	messages := append(collectCmdMessages(t, apply), collectCmdMessages(t, repeated)...)
	if client.applies != 1 || model.surface() != uiSurfaceAgent || !model.surfaceHasPendingOperation() {
		t.Fatalf("applies=%d, surface=%v, pending=%t", client.applies, model.surface(), model.surfaceHasPendingOperation())
	}
	for _, message := range messages {
		model.Update(message)
	}
	if !model.surface().isTranscript() || model.modelName != "replacement-model" || model.modelContractLocked {
		t.Fatalf("surface=%v, model=%v, locked=%t", model.surface(), model.modelName, model.modelContractLocked)
	}
}

func TestAgentPickerPreSessionSelectionReachesCreatedSession(t *testing.T) {
	t.Setenv("REMOTE_TEST_KEY", "ready-key")
	fixture := startRemoteAuthTestFixture(t)
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
	testSetMainInput(model, "/agent")
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model.Update(tea.KeyMsg{Type: tea.KeyHome})
	for index, choice := range model.agentChoices() {
		if choice.Role == "fast" {
			for range index {
				model.Update(tea.KeyMsg{Type: tea.KeyDown})
			}
			break
		}
	}
	_, apply := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, msg := range collectCmdMessages(t, apply) {
		model.Update(msg)
	}
	if model.newSessionTarget() == nil || model.newChatTarget().InitialSettings.AgentRole != "fast" || !model.surface().isTranscript() {
		t.Fatal("selection did not finish locally in New Chat")
	}
	result := model.newSessionInputCmd("$ true", runtimeinput.Shell("true"), newSessionInputEnter)().(newSessionActionDoneMsg)
	if result.err != nil || result.session == nil {
		t.Fatalf("creation failed: %v", result.err)
	}
	id, err := runtimeids.ParseSessionID(result.session.SessionId)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := (sessionChatSettingsClient{service: server.ChatSettingsClient(), sessionID: id}).ReadChatSettings()
	if err != nil || settings.GetSelectedAgent().GetRole() != "fast" {
		t.Fatalf("created Agent = %v, error = %v", settings.GetSelectedAgent(), err)
	}
}

func TestAgentPickerApplyFailureRetry(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeyRunes, Runes: []rune{'r'}}} {
			t.Run(key.String()+"/"+map[bool]string{false: "transport", true: "rejected"}[rejected], func(t *testing.T) {
				model, client := agentPickerSessionModel()
				client.rejected = rejected
				if !rejected {
					client.applyErr = errors.New("apply failed")
				}
				testSetMainInput(model, "/agent")
				model.Update(tea.KeyMsg{Type: tea.KeyEnter})
				model.Update(tea.KeyMsg{Type: tea.KeyDown})
				_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
				for _, msg := range collectCmdMessages(t, command) {
					model.Update(msg)
				}
				if model.surface() != uiSurfaceAgent || model.surfaceHasPendingOperation() {
					t.Fatal("failure did not retain an interactive picker")
				}
				client.applyErr, client.rejected = nil, false
				_, command = model.Update(key)
				for _, msg := range collectCmdMessages(t, command) {
					model.Update(msg)
				}
				if client.applies != 2 || !model.surface().isTranscript() || model.sessionSettings.SelectedAgent.Role != "fast" {
					t.Fatalf("retry applies=%d, surface=%v", client.applies, model.surface())
				}
			})
		}
	}
}

func TestLockedAgentAliasesRefuseLocally(t *testing.T) {
	disableTransientStatusClearForTest(t)
	for _, alias := range []string{"/agent", "/model"} {
		t.Run(alias, func(t *testing.T) {
			model := busyCommandTestModel()
			model.setKnownSessionTarget(detailTestSessionID)
			settings := runtimeControlFakeChatSettings()
			settings.AgentEditability = settingspb.Editability_EDITABILITY_WORKFLOW_LOCK
			settings.AgentLocked = true
			model.applyChatSettingsProjection(settings)
			// A command-time request would panic: this surface must use retained settings.
			model.chatSettings = nil
			testSetMainInput(model, alias)
			next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			collectCmdMessages(t, command)
			updated := next.(*uiModel)
			if !updated.surface().isTranscript() || updated.transientStatus == "" {
				t.Fatalf("surface = %v, notice = %v", updated.surface(), updated.transientStatus)
			}
			requireBusyCommandQueuesEmpty(t, updated)
		})
	}
}
