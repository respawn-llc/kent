package app

import (
	"context"
	"io"
	"testing"

	"core/cli/app/internal/projectbinding"
	"core/internal/testharness/testsetup"
	serverstartup "core/server/startup"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/serverapi"
	"core/shared/toolspec"
)

func TestRemoteNoAuthUnregisteredWorkspaceBindingCanPrepareRuntime(t *testing.T) {
	newAppTestHome(t)
	workspace := t.TempDir()
	configureAppTestServerPort(t)
	fakeResponses, hits := newFakeResponsesServer(t, []string{"rebound no-auth reply"})
	defer fakeResponses.Close()
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, testsetup.WithResponsesProvider(cfg.Settings, fakeResponses.URL))

	srv, err := serverstartup.StartServeServer(context.Background(), serverstartup.Request{
		Model: "gpt-5",
	}, autoOnboarding)

	if err != nil {
		t.Fatalf("serve.Start: %v", err)
	}
	defer func() { _ = srv.Close() }()
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
			return authMethodPickerResult{Choice: authMethodChoiceSkip}, nil
		},
	}
	server, err := startSessionServer(context.Background(), Options{WorkspaceRoot: workspace, WorkspaceRootExplicit: true, Model: "gpt-5"}, interactor, true)
	if err != nil {
		t.Fatalf("startSessionServer: %v", err)
	}
	defer func() { _ = server.Close() }()
	bound, err := ensureInteractiveProjectBinding(context.Background(), server)
	if err != nil {
		t.Fatalf("ensureInteractiveProjectBinding: %v", err)
	}
	_, runtimePlan := prepareAppRuntimePlan(t, bound, sessionLaunchRequest{Mode: launchModeInteractive, Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin())}, io.Discard, "test remote no-auth rebound runtime")
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
	_, workspace := newRegisteredAppWorkspace(t)

	defaultResponses, defaultHits := newFakeResponsesServer(t, []string{"interactive daemon default"})
	defer defaultResponses.Close()
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, testsetup.WithResponsesProvider(cfg.Settings, defaultResponses.URL))

	fixture := startConfiguredDaemonFixture(t, workspace, serverstartup.Request{
		WorkspaceRoot:         workspace,
		WorkspaceRootExplicit: true,
		Model:                 "gpt-5.4",
	})

	server := fixture.attachRemoteSessionServer(t, Options{
		WorkspaceRoot:         workspace,
		WorkspaceRootExplicit: true,
		Model:                 "gpt-5.3-codex",
		Tools:                 "shell",
	}, newHeadlessAuthInteractor())

	plan, runtimePlan := prepareAppRuntimePlan(t, server, sessionLaunchRequest{Mode: launchModeInteractive, Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin())}, io.Discard, "test remote interactive runtime override")
	defer closeRuntimeLaunchPlan(t, runtimePlan)
	if plan.ActiveSettings.Model != "gpt-5.3-codex" {
		t.Fatalf("model = %q, want gpt-5.3-codex", plan.ActiveSettings.Model)
	}
	if plan.StatusConfig.AuthSelection == nil ||
		plan.StatusConfig.AuthSelection.ConnectionId != "test" {
		t.Fatalf("status auth selection = %+v, want configured connection", plan.StatusConfig.AuthSelection)
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
	_, workspace := newRegisteredAppWorkspace(t)
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
		Model:                 "gpt-5",
	})

	server := fixture.attachRemoteSessionServer(t, Options{WorkspaceRoot: workspace, WorkspaceRootExplicit: true}, newHeadlessAuthInteractor())
	_, runtimePlan := prepareAppRuntimePlan(t, server, sessionLaunchRequest{Mode: launchModeInteractive, Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin())}, io.Discard, "test remote prompt round trip")
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
