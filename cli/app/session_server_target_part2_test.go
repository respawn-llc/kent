package app

import (
	"context"
	"io"
	"testing"

	"core/cli/app/internal/projectbinding"
	"core/internal/testharness/requestfixture"
	"core/internal/testharness/testsetup"
	serverstartup "core/server/startup"
	"core/shared/clientui"
	"core/shared/config"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/toolspec"
)

func TestTUIOpeningDormantSessionReadsStateWithoutHydration(t *testing.T) {
	_, workspace := newConfiguredAppWorkspace(t)
	provider, hits := newFakeResponsesServer(t, nil)
	defer provider.Close()
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, testsetup.WithResponsesProvider(cfg.Settings, provider.URL))
	fixture := startConfiguredDaemonFixture(t, workspace, serverstartup.Request{
		WorkspaceRoot: workspace, WorkspaceRootExplicit: true, Model: "gpt-6-sol",
	}, nil)
	server := fixture.attachRemoteSessionServer(t, Options{WorkspaceRoot: workspace}, newHeadlessAuthInteractor())
	created, opening := prepareAppRuntimePlan(t, server, sessionLaunchRequest{
		Mode:   launchModeInteractive,
		Intent: requestfixture.LaunchIntent(t, serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin())),
	}, io.Discard, "create dormant fixture")
	closeRuntimeLaunchPlan(t, opening)
	id, err := runtimeids.ParseSessionID(created.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	_, reopened := prepareAppRuntimePlan(t, server, sessionLaunchRequest{
		Mode: launchModeInteractive, Intent: requestfixture.LaunchIntent(t, serverapi.OpenExistingSessionLaunchIntent(id)),
	}, io.Discard, "open dormant fixture")
	defer closeRuntimeLaunchPlan(t, reopened)
	view, err := reopened.Wiring.runtimeClient.RefreshMainView()
	if err != nil {
		t.Fatal(err)
	}
	if view.Session.SessionId != id.String() || view.Activity.State != runtimepb.ActivityState_RUNTIME_ACTIVITY_UNAVAILABLE {
		t.Fatalf("dormant Main View = %+v", view)
	}
	select {
	case event := <-reopened.Wiring.eventDispatcher.transcriptEvents:
		t.Fatalf("dormant Ongoing emitted transcript event: %+v", event)
	default:
	}
	if hits.Load() != 0 {
		t.Fatal("opening invoked the model")
	}
}

func TestRemoteNoAuthUnregisteredWorkspaceBindingCanPrepareRuntime(t *testing.T) {
	useStartupTestTerminal(t)
	newAppTestHome(t)
	workspace := t.TempDir()
	configureAppTestServerPort(t)
	fakeResponses, hits := newFakeResponsesServer(t, []string{"rebound no-auth reply"})
	defer fakeResponses.Close()
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	cfg.Settings = testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, testsetup.WithResponsesProvider(cfg.Settings, fakeResponses.URL))
	loadOptions := config.LoadOptions{Model: "gpt-6-sol"}
	cfg.Settings.Model = loadOptions.Model
	srv, _ := newMemoryAppTestServer(t, cfg, testsetup.OpenStore(t, cfg.PersistenceRoot), loadOptions)
	stopServing := serveAppServer(t, srv)
	defer stopServing()
	waitForConfiguredRunPromptDaemon(t, workspace)

	originalPicker := runProjectBindingPickerFlow
	originalPrompt := runProjectNamePromptFlow
	t.Cleanup(func() {
		runProjectBindingPickerFlow = originalPicker
		runProjectNamePromptFlow = originalPrompt
	})
	runProjectBindingPickerFlow = func(context.Context, []clientui.ProjectSummary, string, projectbinding.ProjectPickerSnapshot) (projectBindingPickerResult, error) {
		return projectbinding.ProjectPickerCreateNew{}, nil
	}
	runProjectNamePromptFlow = func(defaultName string, theme string) (string, error) {
		return "Remote No Auth Project", nil
	}

	authPickerCalls := 0
	interactor := &interactiveAuthInteractor{
		pickMethod: func(authInteraction) (authMethodPickerResult, error) {
			authPickerCalls++
			if authPickerCalls > 1 {
				t.Fatal("remote no-auth binding flow must not re-enter auth picker")
			}
			t.Fatal("configured auth-less connections must not open a sign-in picker")
			return authMethodPickerResult{}, nil
		},
	}
	server, err := startSessionServer(context.Background(), Options{WorkspaceRoot: workspace, WorkspaceRootExplicit: true, Model: "gpt-6-sol"}, interactor, true)
	if err != nil {
		t.Fatalf("startSessionServer: %v", err)
	}
	defer func() { _ = server.Close() }()
	bound, err := ensureInteractiveProjectBinding(context.Background(), server)
	if err != nil {
		t.Fatalf("ensureInteractiveProjectBinding: %v", err)
	}
	_, runtimePlan := prepareAppRuntimePlan(t, bound, sessionLaunchRequest{Mode: launchModeInteractive, Intent: requestfixture.LaunchIntent(t, serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()))}, io.Discard, "test remote no-auth rebound runtime")
	submission, err := submitRuntimeClientForTest(t, runtimePlan.Wiring.runtimeClient, "hello after rebound no auth")
	requireQueuedAppTestUserTurn(t, submission, err)
	waitForRemoteTranscriptAssistantFinal(
		t,
		runtimePlan.Wiring.eventDispatcher.transcriptEvents,
		"rebound no-auth reply",
	)
	runtimePlan.Close()
	if hits.Load() != 1 {
		t.Fatalf("expected fake LLM call once, got %d", hits.Load())
	}
}

func TestStartSessionServerUsesInvocationOverridesWhenAttachingToDiscoveredDaemon(t *testing.T) {
	_, workspace := newConfiguredAppWorkspace(t)

	defaultResponses, defaultHits := newFakeResponsesServer(t, []string{"interactive daemon default"})
	defer defaultResponses.Close()
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, testsetup.WithResponsesProvider(cfg.Settings, defaultResponses.URL))

	fixture := startConfiguredDaemonFixture(t, workspace, serverstartup.Request{
		WorkspaceRoot:         workspace,
		WorkspaceRootExplicit: true,
		Model:                 "gpt-6-sol",
	}, nil)

	options := Options{
		WorkspaceRoot:         workspace,
		WorkspaceRootExplicit: true,
		Model:                 "gpt-6-luna",
		Tools:                 "shell",
	}
	server := fixture.attachRemoteSessionServer(t, options, newHeadlessAuthInteractor())

	plan, runtimePlan := prepareAppRuntimePlan(t, server, sessionLaunchRequest{
		Mode: launchModeInteractive, Intent: requestfixture.LaunchIntent(t, serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin())),
		Overrides: runPromptOverridesFromOptions(options),
	}, io.Discard, "test remote interactive runtime override")
	defer closeRuntimeLaunchPlan(t, runtimePlan)
	if plan.ActiveSettings.Model != "gpt-6-luna" {
		t.Fatalf("model = %q, want gpt-6-luna", plan.ActiveSettings.Model)
	}
	if len(plan.EnabledTools) != 1 || plan.EnabledTools[0] != toolspec.ToolExecCommand {
		t.Fatalf("enabled tools = %+v, want only shell", plan.EnabledTools)
	}

	submission, err := submitRuntimeClientForTest(t, runtimePlan.Wiring.runtimeClient, "hello through interactive override")
	requireQueuedAppTestUserTurn(t, submission, err)
	waitForRemoteTranscriptAssistantFinal(
		t,
		runtimePlan.Wiring.eventDispatcher.transcriptEvents,
		"interactive daemon default",
	)
	if defaultHits.Load() != 1 {
		t.Fatalf("expected configured endpoint called once, got %d", defaultHits.Load())
	}
}

func TestStartSessionServerUsesConfiguredDaemonForPromptRoundTrip(t *testing.T) {
	_, workspace := newConfiguredAppWorkspace(t)
	t.Setenv("KENT_REVIEWER_FREQUENCY", "off")
	model := newAppTestModelServer(t,
		appTestModelStep{Calls: []appTestModelToolCall{
			appTestAskCall("ask-1", "Pick one", []string{"one", "two"}, 2),
		}},
		appTestModelStep{Calls: []appTestModelToolCall{
			appTestOutsidePatchCall("patch-1", appTestOutsidePatchPath(t)),
		}},
		appTestModelStep{Final: "prompt round trip complete"},
	)
	defer model.Close()
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, testsetup.WithResponsesProvider(cfg.Settings, model.URL()))

	fixture := startConfiguredDaemonFixture(t, workspace, serverstartup.Request{
		WorkspaceRoot:         workspace,
		WorkspaceRootExplicit: true,
		Model:                 "gpt-6-sol",
	}, nil)

	server := fixture.attachRemoteSessionServer(t, Options{WorkspaceRoot: workspace, WorkspaceRootExplicit: true}, newHeadlessAuthInteractor())
	_, runtimePlan := prepareAppRuntimePlan(t, server, sessionLaunchRequest{Mode: launchModeInteractive, Intent: requestfixture.LaunchIntent(t, serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()))}, io.Discard, "test remote prompt round trip")
	defer closeRuntimeLaunchPlan(t, runtimePlan)

	submissionDone, submissionFailed := startAppTestRuntimeSubmission(t, runtimePlan.Wiring.runtimeClient, "start prompt round trip")
	requireQueuedAppTestRuntimeSubmission(t, submissionDone)
	askPrompt := waitForRemoteTranscriptPrompt(t, runtimePlan.Wiring.eventDispatcher.transcriptEvents, "ask-1", submissionFailed)
	if askPrompt.GetQuestion() == nil || transcriptPromptQuestion(askPrompt) != "Pick one" {
		t.Fatalf("unexpected ask prompt: %+v", askPrompt)
	}
	answerRemoteTranscriptPrompt(t, runtimePlan.Wiring.promptAnswers, askPrompt, clientui.PromptAnswer{
		ToolCallID:           clientui.ToolCallID(transcriptPromptToolCallID(askPrompt)),
		SelectedOptionNumber: func() *int { selected := 2; return &selected }(),
	})
	approvalPrompt := waitForRemoteTranscriptPrompt(t, runtimePlan.Wiring.eventDispatcher.transcriptEvents, "", submissionFailed)
	if approvalPrompt.GetApproval() == nil {
		t.Fatalf("unexpected approval prompt: %+v", approvalPrompt)
	}
	answerRemoteTranscriptPrompt(t, runtimePlan.Wiring.promptAnswers, approvalPrompt, clientui.PromptAnswer{
		ToolCallID: clientui.ToolCallID(transcriptPromptToolCallID(approvalPrompt)),
		Approval: &clientui.ApprovalPromptAnswer{
			Decision:   clientui.ApprovalDecisionAllowOnce,
			Commentary: "trusted",
		},
	})
	waitForRemoteTranscriptAssistantFinal(
		t,
		runtimePlan.Wiring.eventDispatcher.transcriptEvents,
		"prompt round trip complete",
		submissionFailed,
	)
}
