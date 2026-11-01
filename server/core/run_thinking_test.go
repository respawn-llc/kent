package core

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	modelstub "core/internal/testharness/pty/blackbox"
	"core/internal/testharness/requestfixture"
	"core/internal/testharness/testsetup"
	"core/server/authmanager"
	"core/server/launch"
	"core/server/runprompt"
	"core/server/sessionlaunch"
	"core/server/sessionruntime"
	"core/server/sessionstore"
	"core/shared/apicontract"
	"core/shared/config"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func newRunThinkingSession(t *testing.T, handler http.HandlerFunc) (*Core, *session.Store, apicontract.RunPromptService) {
	t.Helper()
	root, workspace := t.TempDir(), t.TempDir()
	if handler == nil {
		handler = func(w http.ResponseWriter, r *http.Request) {
			t.Error("unexpected provider request")
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
	provider := httptest.NewServer(handler)
	t.Cleanup(provider.Close)
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(`
model = "gpt-5"
thinking_level = "medium"
[reviewer]
frequency = "off"
[subagents.worker]
model = "gpt-5-mini"
thinking_level = "low"
[subagents.default]
model = "gpt-5.6-sol"
thinking_level = "medium"
[subagents.equivalent]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(workspace, workspace, config.LoadOptions{ConfigRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Settings = testsetup.WithResponsesProvider(cfg.Settings, provider.URL)
	app := newCoreTestApp(t, cfg, auth.EmptyState())
	binding, err := app.MetadataStore().RegisterWorkspaceBinding(t.Context(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	store := createCoreSettingsSession(t, app, cfg, binding.ProjectID)
	client, err := app.RunPromptClientForProjectWorkspace(t.Context(), binding.ProjectID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	return app, store, client
}

func TestChatShellPreparesDormantSessionWithoutModel(t *testing.T) {
	app, store, _ := newRunThinkingSession(t, nil)
	id, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(store.Meta().WorkspaceRoot, "shell-first-action")
	result, err := app.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{
		QueueItemId: runtimeids.NewQueueItemID().String(),
		Target: &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{
			Session: &chatpb.ExistingSessionRequestTarget{SessionId: protoapi.SessionSelectorToProto(id)},
		}},
		Activation: &chatpb.Activation{Input: &chatpb.Activation_Shell{Shell: &chatpb.ShellCommand{Command: "touch " + output}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.GetAccepted() == nil {
		t.Fatalf("Shell not accepted: %+v", result)
	}
	testsetup.RequireUntil(t, time.Now().Add(5*time.Second), time.Millisecond, func() bool {
		_, err := os.Stat(output)
		return err == nil
	}, "accepted Shell did not complete")
}

func TestNewChatShellCreatesSessionAndExecutesOnceWithoutModel(t *testing.T) {
	app, source, _ := newRunThinkingSession(t, nil)
	binding, err := app.MetadataStore().ResolveSessionNavigationBinding(t.Context(), source.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(source.Meta().WorkspaceRoot, "new-shell-count")
	result, err := app.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{
		QueueItemId: runtimeids.NewQueueItemID().String(),
		Target:      newChatCommandTarget(t, app, binding.ProjectId, binding.WorkspaceId),
		Activation:  &chatpb.Activation{Input: &chatpb.Activation_Shell{Shell: &chatpb.ShellCommand{Command: "echo invoked >> " + output}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.GetAccepted() == nil || result.Session.SessionId == source.Meta().SessionID {
		t.Fatalf("shell did not settle on its created Session: %+v", result)
	}
	record, err := app.MetadataStore().ResolvePersistedSession(t.Context(), result.Session.SessionId)
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.ModelRequestCount != 0 {
		t.Fatalf("shell made %d model requests", record.Meta.ModelRequestCount)
	}
	testsetup.RequireUntil(t, time.Now().Add(5*time.Second), time.Millisecond, func() bool {
		content, err := os.ReadFile(output)
		return err == nil && len(strings.Fields(string(content))) == 1
	}, "accepted Shell did not execute")
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Fields(string(content))) != 1 {
		t.Fatalf("shell executed more than once: %q", content)
	}
	requireChatSessionCount(t, app, binding.ProjectId, 1)
}

func TestNewChatShellFailureRetainsCreatedSessionAndDraft(t *testing.T) {
	app, _, binding, _ := newChatCommandCore(t, errors.New("runtime preparation failed"))
	target := newChatCommandTarget(t, app, binding.ProjectID, binding.WorkspaceID)
	result, err := app.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{
		QueueItemId:       runtimeids.NewQueueItemID().String(),
		Target:            target,
		Activation:        &chatpb.Activation{Input: &chatpb.Activation_Shell{Shell: &chatpb.ShellCommand{Command: "pwd"}}},
		InitialInputDraft: textutil.Value("unsent shell draft"),
	})
	if err != nil || result.GetNotAccepted() == nil {
		t.Fatalf("failed shell result = %v, %v", result, err)
	}
	record, err := app.MetadataStore().ResolvePersistedSession(t.Context(), result.Session.SessionId)
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.InputDraft != "unsent shell draft" || record.Meta.ModelRequestCount != 0 {
		t.Fatalf("failed shell lost draft or called model: %+v", record.Meta)
	}
	requireChatSessionCount(t, app, binding.ProjectID, 1)
}

func TestRunThinkingExplicitDefaultSurvivesFailedContinuation(t *testing.T) {
	app, store, client, requests := newRunThinkingRecordingSession(t)
	if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: textutil.Value("worker")}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SetGoal("finish interactively", session.GoalActorUser); err != nil {
		t.Fatal(err)
	}
	id, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.RunPrompt(t.Context(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "select headless default",
		Overrides: serverapi.RunPromptOverrides{AgentRole: textutil.Value(config.DefaultSubagentRole), ThinkingLevel: "high"},
	}), nil)
	if !errors.Is(err, runprompt.ErrHeadlessGoalSession) {
		t.Fatal(err)
	}
	reopened, err := session.Open(store.Dir(), app.MetadataStore().AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	if role := session.ContinuationAgentRole(reopened.Meta()); role == nil || *role != config.DefaultSubagentRole {
		t.Fatalf("saved headless default identity = %v", role)
	}
	if _, _, err := reopened.ClearGoal(session.GoalActorUser); err != nil {
		t.Fatal(err)
	}
	_, err = client.RunPrompt(t.Context(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "use saved headless default",
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if request := <-requests; request.Model != "gpt-5.6-sol" || request.Reasoning.Effort != "high" {
		t.Fatalf("omitted-flag continuation lost headless selection: %+v", request)
	}
}

func TestRunThinkingCombinedSelectionUsesExplicitModel(t *testing.T) {
	testRunThinkingEffectiveSelection(t, false, serverapi.RunPromptOverrides{
		AgentRole: textutil.Value("worker"), Model: "gpt-5.6-sol", ThinkingLevel: "xhigh",
	}, "worker", "gpt-5.6-sol")
}

func TestRunThinkingAcceptsConfiguredAgentOmittedFromPicker(t *testing.T) {
	testRunThinkingEffectiveSelection(t, false, serverapi.RunPromptOverrides{
		AgentRole: textutil.Value("equivalent"), ThinkingLevel: "high",
	}, "equivalent", "gpt-5")
}

func TestRunThinkingKeepsConfiguredAgentOmittedFromPicker(t *testing.T) {
	app, store, client := newRunThinkingSession(t, nil)
	if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: textutil.Value("equivalent")}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SetGoal("continue interactively", session.GoalActorUser); err != nil {
		t.Fatal(err)
	}
	id, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.RunPrompt(t.Context(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "keep the configured selection",
		Overrides: serverapi.RunPromptOverrides{ThinkingLevel: "high"},
	}), nil)
	if !errors.Is(err, runprompt.ErrHeadlessGoalSession) {
		t.Fatal(err)
	}
	reopened, err := session.Open(store.Dir(), app.MetadataStore().AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	if role := session.ContinuationAgentRole(reopened.Meta()); role == nil || *role != "equivalent" {
		t.Fatalf("saved configured Agent identity = %v", role)
	}
}

func TestRunThinkingOnlySelectionReachesProvider(t *testing.T) {
	testRunThinkingEffectiveSelection(t, false, serverapi.RunPromptOverrides{
		ThinkingLevel: "high",
	}, config.DefaultSubagentRole, "gpt-5")
}

func TestRunThinkingPreparedActivationUsesPersistedSelection(t *testing.T) {
	app, store, _, requests := newRunThinkingRecordingSession(t)
	id, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	service, err := (chatSettingsService{core: app}).sessionSettingsService(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	request, err := service.SaveRunSelection(t.Context(), sessionlaunch.PlanRequest{
		Mode: launch.ModeHeadless, Intent: serverapi.OpenExistingSessionLaunchIntent(id),
		Overrides: serverapi.RunPromptOverrides{
			AgentRole: textutil.Value("worker"), Model: "gpt-5.6-sol", ThinkingLevel: "xhigh",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// A later accepted settings change owns the next activation.
	changed, err := app.ChatSettingsClient().MutateChatSettings(t.Context(), &chatsettingspb.MutationRequest{
		Session:   &chatsettingspb.SessionTarget{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: id.String()}}},
		Operation: &chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_Thinking{Thinking: "high"}},
	})
	if err != nil || changed.GetResult().GetApplied() == nil {
		t.Fatalf("change saved Thinking: %v, %v", changed, err)
	}
	planned, err := service.PlanLaunchSession(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	activation, err := sessionruntime.ActivationRequestFromSessionPlan(planned.Plan)
	if err != nil {
		t.Fatal(err)
	}
	if activation.ThinkingOverrideExplicit || activation.AgentSelection != nil ||
		activation.ActiveSettings.ThinkingLevel != "high" || activation.ActiveSettings.Model != "gpt-5.6-sol" {
		t.Fatalf("activation did not consume saved selection: %+v", activation)
	}
	result, err := app.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{
		QueueItemId: runtimeids.NewQueueItemID().String(),
		Target:      chatCommandTarget(id.String()),
		Activation:  &chatpb.Activation{Input: &chatpb.Activation_Text{Text: "use saved choices"}},
	})
	if err != nil || result.GetAccepted() == nil {
		t.Fatalf("Chat admission = %v, %v", result, err)
	}
	if request := <-requests; request.Model != "gpt-5-mini" || request.Reasoning.Effort != "high" {
		t.Fatalf("Chat ignored saved selection: %+v", request)
	}
}

func TestChatActionAppliesInvocationSelection(t *testing.T) {
	testChatActionInvocationSelection(t, false)
}

func TestChatActionAppliesInvocationSelectionToLoadedRuntime(t *testing.T) {
	testChatActionInvocationSelection(t, true)
}

func testChatActionInvocationSelection(t *testing.T, alreadyLoaded bool) {
	t.Helper()
	app, store, _, requests := newRunThinkingRecordingSession(t)
	id := store.Identity().ID
	service, err := (chatSettingsService{core: app}).sessionSettingsService(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if alreadyLoaded {
		planned, err := service.PlanLaunchSession(t.Context(), sessionlaunch.PlanRequest{
			Mode: launch.ModeInteractive, Intent: serverapi.OpenExistingSessionLaunchIntent(id),
			Overrides: serverapi.RunPromptOverrides{AgentRole: textutil.Value("worker"), ThinkingLevel: "low", Tools: "patch"},
		})
		if err != nil {
			t.Fatal(err)
		}
		activation, err := sessionruntime.ActivationRequestFromSessionPlan(planned.Plan)
		if err != nil {
			t.Fatal(err)
		}
		if err := app.SessionRuntimeClient().ActivateSessionRuntime(t.Context(), activation); err != nil {
			t.Fatal(err)
		}
	}
	planned, err := service.PlanLaunchSession(t.Context(), sessionlaunch.PlanRequest{
		Mode: launch.ModeInteractive, Intent: serverapi.OpenExistingSessionLaunchIntent(id),
		Overrides: serverapi.RunPromptOverrides{
			AgentRole: textutil.Value("worker"), ThinkingLevel: "high", Tools: "patch",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	activation, err := sessionruntime.ActivationRequestFromSessionPlan(planned.Plan)
	if err != nil {
		t.Fatal(err)
	}
	preparation, err := protoapi.SessionRuntimePreparationToProto(activation.SessionRuntimePreparationOptions)
	if err != nil {
		t.Fatal(err)
	}
	target := chatCommandTarget(id.String())
	target.GetSession().Preparation = preparation
	result, err := app.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{
		QueueItemId: runtimeids.NewQueueItemID().String(),
		Target:      target,
		Activation:  &chatpb.Activation{Input: &chatpb.Activation_Text{Text: "use invocation choices"}},
	})
	if err != nil || result.GetAccepted() == nil {
		t.Fatalf("Chat admission = %v, %v", result, err)
	}
	if request := <-requests; request.Model != "gpt-5-mini" || request.Reasoning.Effort != "high" {
		t.Fatalf("Chat ignored invocation selection: %+v", request)
	}
	record, err := app.MetadataStore().ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.RetainedToolSelection == nil || !reflect.DeepEqual(record.Meta.RetainedToolSelection.Tools, []toolspec.ID{toolspec.ToolPatch}) {
		t.Fatalf("Chat discarded explicit tools: %+v", record.Meta.RetainedToolSelection)
	}
}

type runThinkingModelRequest struct {
	Model     string `json:"model"`
	Reasoning struct {
		Effort string `json:"effort"`
	} `json:"reasoning"`
}

func newRunThinkingRecordingSession(t *testing.T) (*Core, *session.Store, apicontract.RunPromptService, <-chan runThinkingModelRequest) {
	t.Helper()
	requests := make(chan runThinkingModelRequest, 1)
	app, store, client := newRunThinkingSession(t, func(w http.ResponseWriter, r *http.Request) {
		var request runThinkingModelRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		requests <- request
		modelstub.WriteCompletedResponseStream(w, "done", 1, 1)
	})
	return app, store, client, requests
}

func testRunThinkingEffectiveSelection(t *testing.T, locked bool, overrides serverapi.RunPromptOverrides, wantRole, wantModel string) []runpromptpb.RunSelectionWarning {
	t.Helper()
	app, store, client, requests := newRunThinkingRecordingSession(t)
	if locked {
		lockRunThinkingSession(t, store)
	}
	id, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.RunPrompt(t.Context(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "use the combined selection",
		Overrides: overrides,
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	request := <-requests
	if request.Model != wantModel || request.Reasoning.Effort != overrides.ThinkingLevel {
		t.Fatalf("effective model/Thinking = %+v", request)
	}
	read, err := app.ChatSettingsClient().ReadChatSettings(t.Context(), &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: id.String()}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := read.GetSession().Settings.SelectedAgent; got.Role != wantRole || got.Thinking != overrides.ThinkingLevel {
		t.Fatalf("saved selection = %+v", got)
	}
	if response == nil {
		t.Fatal("Run returned no success response")
	}
	return response.SelectionWarnings
}

func TestRunThinkingOmittedFlagUsesSavedSelection(t *testing.T) {
	app, store, client, requests := newRunThinkingRecordingSession(t)
	id, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := app.ChatSettingsClient().MutateChatSettings(t.Context(), &chatsettingspb.MutationRequest{
		Session:   &chatsettingspb.SessionTarget{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: id.String()}}},
		Operation: &chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_Thinking{Thinking: "high"}},
	})
	if err != nil || changed.GetResult().GetApplied() == nil {
		t.Fatalf("save Thinking: %v, %v", changed, err)
	}
	_, err = client.RunPrompt(t.Context(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "use saved Thinking",
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if request := <-requests; request.Reasoning.Effort != "high" {
		t.Fatalf("omitted flag Thinking = %q", request.Reasoning.Effort)
	}
}

func TestRunThinkingNewSessionUsesExplicitSelection(t *testing.T) {
	_, _, client, requests := newRunThinkingRecordingSession(t)
	_, err := client.RunPrompt(t.Context(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent:    serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
		Prompt:    "create with explicit Thinking",
		Overrides: serverapi.RunPromptOverrides{ThinkingLevel: "high"},
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if request := <-requests; request.Reasoning.Effort != "high" {
		t.Fatalf("new Session Thinking = %q", request.Reasoning.Effort)
	}
}

func TestRunThinkingPersistsBeforeContinuationFailure(t *testing.T) {
	testRunThinkingContinuationFailure(t, serverapi.RunPromptOverrides{ThinkingLevel: "high"}, config.DefaultSubagentRole)
}

func TestRunThinkingCombinedSelectionSurvivesContinuationFailure(t *testing.T) {
	testRunThinkingContinuationFailure(t, serverapi.RunPromptOverrides{
		AgentRole: textutil.Value("worker"), Model: "gpt-5.6-sol", ThinkingLevel: "xhigh",
	}, "worker")
}

func testRunThinkingContinuationFailure(t *testing.T, overrides serverapi.RunPromptOverrides, wantRole string) {
	t.Helper()
	app, store, client := newRunThinkingSession(t, nil)
	if _, _, err := store.SetGoal("keep this Session interactive", session.GoalActorUser); err != nil {
		t.Fatal(err)
	}
	id, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	projectID, err := app.MetadataStore().ResolveSessionProjectID(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	other := createCoreSettingsSession(t, app, app.safeBundles().Projects.cfg, projectID)
	otherBefore, err := app.MetadataStore().ResolvePersistedSession(t.Context(), other.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.RunPrompt(t.Context(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent:    serverapi.OpenExistingSessionLaunchIntent(id),
		Prompt:    "must not be submitted",
		Overrides: overrides,
	}), nil)
	if !errors.Is(err, runprompt.ErrHeadlessGoalSession) {
		t.Fatalf("continuation error = %v", err)
	}
	read, err := app.ChatSettingsClient().ReadChatSettings(t.Context(), &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: id.String()}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := read.GetSession().Settings.SelectedAgent; got.Thinking != overrides.ThinkingLevel || got.Role != wantRole {
		t.Fatalf("saved selection = %+v", got)
	}
	if app.safeBundles().Runtime.runtimeRegistry.RuntimeActivityRegistrySnapshot(id.String()).Registered {
		t.Fatal("failed dormant continuation created a Runtime")
	}
	reopened, err := session.Open(store.Dir(), app.MetadataStore().AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Meta().ChatSettings; got == nil || got.Thinking == nil || *got.Thinking != overrides.ThinkingLevel {
		t.Fatalf("reopened Thinking = %+v", got)
	}
	otherAfter, err := app.MetadataStore().ResolvePersistedSession(t.Context(), other.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(otherBefore.Meta, otherAfter.Meta) {
		t.Fatal("Run changed another Session")
	}
}

func TestRunThinkingRejectsUnsupportedSelection(t *testing.T) {
	testRunThinkingRejected(t, serverapi.RunPromptOverrides{ThinkingLevel: "unsupported"}, false)
}

func TestRunThinkingRejectsCombinedSelectionWithoutSavingAgent(t *testing.T) {
	testRunThinkingRejected(t, serverapi.RunPromptOverrides{
		AgentRole: textutil.Value("worker"), Model: "gpt-5", ThinkingLevel: "xhigh",
	}, false)
}

func TestRunThinkingCombinedRejectionUsesNewModelCapabilities(t *testing.T) {
	app, store, client := newRunThinkingSession(t, nil)
	if _, _, err := store.SetGoal("continue interactively", session.GoalActorUser); err != nil {
		t.Fatal(err)
	}
	id, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.RunPrompt(t.Context(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "save supported selection",
		Overrides: serverapi.RunPromptOverrides{Model: "gpt-5.6-sol", ThinkingLevel: "xhigh"},
	}), nil)
	if !errors.Is(err, runprompt.ErrHeadlessGoalSession) {
		t.Fatal(err)
	}
	before, err := app.MetadataStore().ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.RunPrompt(t.Context(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "reject unsupported selection",
		Overrides: serverapi.RunPromptOverrides{Model: "gpt-5", ThinkingLevel: "ultra"},
	}), nil)
	if err == nil || errors.Is(err, runprompt.ErrHeadlessGoalSession) {
		t.Fatalf("invalid combination reached continuation: %v", err)
	}
	after, err := app.MetadataStore().ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Meta, after.Meta) {
		t.Fatal("invalid model/Thinking combination changed the saved selection")
	}
}

func testRunThinkingRejected(t *testing.T, overrides serverapi.RunPromptOverrides, locked bool) {
	t.Helper()
	app, store, client := newRunThinkingSession(t, nil)
	if locked {
		lockRunThinkingSession(t, store)
	}
	id, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := app.MetadataStore().ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.RunPrompt(t.Context(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent:    serverapi.OpenExistingSessionLaunchIntent(id),
		Prompt:    "must not be submitted",
		Overrides: overrides,
	}), nil)
	var rejected *serverapi.RunSelectionRejectedError
	if !errors.As(err, &rejected) || rejected.Reason != chatsettingspb.MutationRejectionReason_MUTATION_REJECTION_REASON_THINKING_UNAVAILABLE {
		t.Fatalf("unsupported Thinking rejection lost its reason: %v", err)
	}
	after, err := app.MetadataStore().ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Meta, after.Meta) {
		t.Fatal("rejected Thinking changed the Session")
	}
	if app.safeBundles().Runtime.runtimeRegistry.RuntimeActivityRegistrySnapshot(id.String()).Registered {
		t.Fatal("rejected Thinking created a Runtime")
	}
}

func lockRunThinkingSession(t *testing.T, store *session.Store) {
	t.Helper()
	if err := store.MarkModelDispatchLocked(session.LockedContract{
		Model: "gpt-5", HasEnabledTools: true, EnabledTools: []string{"ask_question"}, WebSearchMode: "none",
		ProviderContract: session.LockedProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRunThinkingLockedSelectionRetainsAgentAndModel(t *testing.T) {
	warnings := testRunThinkingEffectiveSelection(t, true, serverapi.RunPromptOverrides{
		AgentRole: textutil.Value("worker"), Model: "gpt-4.1", ThinkingLevel: "high",
	}, config.DefaultSubagentRole, "gpt-5")
	wantWarnings := []runpromptpb.RunSelectionWarning{
		runpromptpb.RunSelectionWarning_RUN_SELECTION_WARNING_AGENT_IGNORED_TO_PRESERVE_CACHE,
		runpromptpb.RunSelectionWarning_RUN_SELECTION_WARNING_MODEL_IGNORED_TO_PRESERVE_CACHE,
	}
	if !reflect.DeepEqual(warnings, wantWarnings) {
		t.Fatalf("locked selection warnings = %q, want %q", warnings, wantWarnings)
	}
}

func TestRunThinkingLockedOverridesWarnWithoutThinking(t *testing.T) {
	app, store, client, requests := newRunThinkingRecordingSession(t)
	lockRunThinkingSession(t, store)
	id, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.RunPrompt(t.Context(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "continue with the locked selection",
		Overrides: serverapi.RunPromptOverrides{AgentRole: textutil.Value("worker"), Model: "gpt-4.1"},
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	wantWarnings := []runpromptpb.RunSelectionWarning{
		runpromptpb.RunSelectionWarning_RUN_SELECTION_WARNING_AGENT_IGNORED_TO_PRESERVE_CACHE,
		runpromptpb.RunSelectionWarning_RUN_SELECTION_WARNING_MODEL_IGNORED_TO_PRESERVE_CACHE,
	}
	if response == nil || !reflect.DeepEqual(response.SelectionWarnings, wantWarnings) {
		t.Fatalf("locked selection warnings = %v, want %v", response.GetSelectionWarnings(), wantWarnings)
	}
	if request := <-requests; request.Model != "gpt-5" {
		t.Fatalf("locked model override was applied: %+v", request)
	}
	read, err := app.ChatSettingsClient().ReadChatSettings(t.Context(), &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: id.String()}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selected := read.GetSession().Settings.SelectedAgent; selected.Role != config.DefaultSubagentRole || selected.Model != "gpt-5" {
		t.Fatalf("locked Chat selection = %+v", selected)
	}
}

func TestRunThinkingLockedSelectionRejectsRequestedModelCapability(t *testing.T) {
	testRunThinkingRejected(t, serverapi.RunPromptOverrides{
		AgentRole: textutil.Value("worker"), Model: "gpt-5.6-sol", ThinkingLevel: "xhigh",
	}, true)
}

func TestRunThinkingRejectsWhenLockedContractDisablesReasoning(t *testing.T) {
	app, store, client := newRunThinkingSession(t, nil)
	if err := store.MarkModelDispatchLocked(session.LockedContract{
		Model: "gpt-5", HasEnabledTools: true, EnabledTools: []string{"ask_question"}, WebSearchMode: "none",
		ProviderContract:  session.LockedProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true},
		ModelCapabilities: session.LockedModelCapabilities{SupportsVisionInputs: true},
	}); err != nil {
		t.Fatal(err)
	}
	id, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	read, err := app.ChatSettingsClient().ReadChatSettings(t.Context(), &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: id.String()}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if read.GetSession().Settings.Thinking != nil {
		t.Fatal("fixture has available Thinking despite its locked capability")
	}
	before, err := app.MetadataStore().ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.RunPrompt(t.Context(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "must not dispatch",
		Overrides: serverapi.RunPromptOverrides{ThinkingLevel: "high"},
	}), nil)
	if err == nil {
		t.Fatal("accepted Thinking despite locked capability")
	}
	after, err := app.MetadataStore().ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Meta, after.Meta) {
		t.Fatal("unsupported Thinking changed locked Session")
	}
}

func TestRunThinkingRetainedSelectionAcceptsCustomValues(t *testing.T) {
	app, store, client := newRunThinkingSession(t, nil)
	// This is valid persisted state from a provider-specific custom selection.
	if err := store.SetThinkingOverride(textutil.Value("provider-depth")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SetGoal("continue interactively", session.GoalActorUser); err != nil {
		t.Fatal(err)
	}
	id, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	read, err := app.ChatSettingsClient().ReadChatSettings(t.Context(), &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: id.String()}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if read.GetSession().Settings.Thinking.Kind != chatsettingspb.ThinkingKind_THINKING_KIND_CUSTOM {
		t.Fatal("fixture is not a custom-valued Thinking selection")
	}
	_, err = client.RunPrompt(t.Context(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "save another custom Thinking value",
		Overrides: serverapi.RunPromptOverrides{ThinkingLevel: "provider-depth-next"},
	}), nil)
	if err == nil {
		t.Fatal("expected continuation to fail after saving the custom selection")
	}
	reopened, err := session.Open(store.Dir(), app.MetadataStore().AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Meta().ChatSettings; got.Thinking == nil || *got.Thinking != "provider-depth-next" {
		t.Fatalf("custom Thinking was not saved: %+v", got)
	}
}

func TestRunThinkingPersistsWhileBusy(t *testing.T) {
	started, release := make(chan struct{}, 1), make(chan struct{})
	var calls atomic.Int32
	app, store, client := newRunThinkingSession(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		started <- struct{}{}
		<-release
		modelstub.WriteCompletedResponseStream(w, "done", 1, 1)
	})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	id, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := client.RunPrompt(t.Context(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
			Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "hold this request",
		}), nil)
		done <- err
	}()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("first Run ended before dispatch: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("first Run did not dispatch")
	}
	readRequest := &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: id.String()}}}},
	}
	before, err := app.ChatSettingsClient().ReadChatSettings(t.Context(), readRequest)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.RunPrompt(t.Context(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "reject without delivery",
		Overrides: serverapi.RunPromptOverrides{ThinkingLevel: "unsupported"},
	}), nil)
	var rejected *serverapi.RunSelectionRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("unsupported active Thinking = %v", err)
	}
	admitted := make(chan *runpromptpb.ActiveSteerAccepted, 1)
	continued := make(chan error, 1)
	go func() {
		_, err := client.RunPrompt(t.Context(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
			Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "redirect",
			Overrides: serverapi.RunPromptOverrides{
				ThinkingLevel: "high", AgentRole: textutil.Value("worker"),
				Model: "unavailable-model", Tools: "unavailable-tool", Theme: "dark", ModelTimeoutSeconds: 1,
			},
		}), serverapi.RunPromptProgressFunc(func(event *runpromptpb.ProgressEvent) {
			if accepted := event.GetActiveSteerAccepted(); accepted != nil {
				admitted <- accepted
			}
		}))
		continued <- err
	}()
	var acceptance *runpromptpb.ActiveSteerAccepted
	select {
	case acceptance = <-admitted:
	case err := <-continued:
		t.Fatalf("continuation finished before acceptance: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("active continuation did not report acceptance")
	}
	ignored := map[runpromptpb.IgnoredRunSetting]struct{}{
		runpromptpb.IgnoredRunSetting_IGNORED_RUN_SETTING_AGENT:         {},
		runpromptpb.IgnoredRunSetting_IGNORED_RUN_SETTING_MODEL:         {},
		runpromptpb.IgnoredRunSetting_IGNORED_RUN_SETTING_TOOLS:         {},
		runpromptpb.IgnoredRunSetting_IGNORED_RUN_SETTING_THEME:         {},
		runpromptpb.IgnoredRunSetting_IGNORED_RUN_SETTING_MODEL_TIMEOUT: {},
	}
	for _, setting := range acceptance.IgnoredSettings {
		if _, exists := ignored[setting]; !exists {
			t.Fatalf("unexpected ignored setting %v", setting)
		}
		delete(ignored, setting)
	}
	if len(ignored) != 0 {
		t.Fatalf("missing ignored settings = %v", ignored)
	}
	read, err := app.ChatSettingsClient().ReadChatSettings(t.Context(), readRequest)
	if err != nil {
		t.Fatal(err)
	}
	if got := read.GetSession().Settings.SelectedAgent.Thinking; got != "high" {
		t.Fatalf("busy Session Thinking = %q, want high", got)
	}
	if got, retained := read.GetSession().Settings.SelectedAgent, before.GetSession().Settings.SelectedAgent; got.Role != retained.Role || got.Model != retained.Model {
		t.Fatal("active launch overrides changed the retained selection")
	}
	unblock()
	if err := <-continued; err != nil {
		t.Fatalf("active continuation: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("first Run did not finish")
	}
	if calls.Load() != 2 {
		t.Fatalf("provider calls = %d, want initial and accepted steer only", calls.Load())
	}
}
