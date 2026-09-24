package core

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	modelstub "core/internal/testharness/pty/blackbox"
	"core/internal/testharness/testsetup"
	"core/server/auth"
	"core/server/launch"
	"core/server/metadata"
	"core/server/runprompt"
	"core/server/session"
	"core/server/sessionlaunch"
	"core/server/sessionruntime"
	"core/shared/apicontract"
	"core/shared/config"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
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
	binding, err := metadata.RegisterBinding(t.Context(), root, workspace)
	if err != nil {
		t.Fatal(err)
	}
	app := newCoreTestApp(t, cfg, auth.EmptyState())
	store := createCoreSettingsSession(t, app, cfg, binding.ProjectID)
	client, err := app.RunPromptClientForProjectWorkspace(t.Context(), binding.ProjectID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	return app, store, client
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
	_, err = client.RunPrompt(t.Context(), serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "select headless default",
		Overrides: serverapi.RunPromptOverrides{AgentRole: textutil.Value(config.DefaultSubagentRole), ThinkingLevel: "high"},
	}, nil)
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
	_, err = client.RunPrompt(t.Context(), serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "use saved headless default",
	}, nil)
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
	_, err = client.RunPrompt(t.Context(), serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "keep the configured selection",
		Overrides: serverapi.RunPromptOverrides{ThinkingLevel: "high"},
	}, nil)
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
	app, store, _ := newRunThinkingSession(t, nil)
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
		Session:   &chatsettingspb.SessionTarget{SessionId: id.String()},
		Operation: &chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_Thinking{Thinking: "high"}},
	})
	if err != nil || changed.GetResult().GetApplied() == nil {
		t.Fatalf("change saved Thinking: %v, %v", changed, err)
	}
	planned, err := service.PlanLaunchSession(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	activation, err := sessionruntime.ActivationRequestFromSessionPlan(planned.Plan, "run-thinking-contract")
	if err != nil {
		t.Fatal(err)
	}
	if activation.ThinkingOverrideExplicit || activation.AgentSelection != nil ||
		activation.ActiveSettings.ThinkingLevel != "high" || activation.ActiveSettings.Model != "gpt-5.6-sol" {
		t.Fatalf("activation did not consume saved selection: %+v", activation)
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

func testRunThinkingEffectiveSelection(t *testing.T, locked bool, overrides serverapi.RunPromptOverrides, wantRole, wantModel string) {
	t.Helper()
	app, store, client, requests := newRunThinkingRecordingSession(t)
	if locked {
		lockRunThinkingSession(t, store)
	}
	id, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.RunPrompt(t.Context(), serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "use the combined selection",
		Overrides: overrides,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := <-requests
	if request.Model != wantModel || request.Reasoning.Effort != overrides.ThinkingLevel {
		t.Fatalf("effective model/Thinking = %+v", request)
	}
	read, err := app.ChatSettingsClient().ReadChatSettings(t.Context(), &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: id.String()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := read.GetSession().Settings.SelectedAgent; got.Role != wantRole || got.Thinking != overrides.ThinkingLevel {
		t.Fatalf("saved selection = %+v", got)
	}
}

func TestRunThinkingOmittedFlagUsesSavedSelection(t *testing.T) {
	app, store, client, requests := newRunThinkingRecordingSession(t)
	id, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := app.ChatSettingsClient().MutateChatSettings(t.Context(), &chatsettingspb.MutationRequest{
		Session:   &chatsettingspb.SessionTarget{SessionId: id.String()},
		Operation: &chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_Thinking{Thinking: "high"}},
	})
	if err != nil || changed.GetResult().GetApplied() == nil {
		t.Fatalf("save Thinking: %v, %v", changed, err)
	}
	_, err = client.RunPrompt(t.Context(), serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "use saved Thinking",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if request := <-requests; request.Reasoning.Effort != "high" {
		t.Fatalf("omitted flag Thinking = %q", request.Reasoning.Effort)
	}
}

func TestRunThinkingNewSessionUsesExplicitSelection(t *testing.T) {
	_, _, client, requests := newRunThinkingRecordingSession(t)
	_, err := client.RunPrompt(t.Context(), serverapi.RunPromptRequest{
		Intent:    serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
		Prompt:    "create with explicit Thinking",
		Overrides: serverapi.RunPromptOverrides{ThinkingLevel: "high"},
	}, nil)
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
	_, err = client.RunPrompt(t.Context(), serverapi.RunPromptRequest{
		Intent:    serverapi.OpenExistingSessionLaunchIntent(id),
		Prompt:    "must not be submitted",
		Overrides: overrides,
	}, nil)
	if !errors.Is(err, runprompt.ErrHeadlessGoalSession) {
		t.Fatalf("continuation error = %v", err)
	}
	read, err := app.ChatSettingsClient().ReadChatSettings(t.Context(), &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: id.String()}},
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
	_, err = client.RunPrompt(t.Context(), serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "save supported selection",
		Overrides: serverapi.RunPromptOverrides{Model: "gpt-5.6-sol", ThinkingLevel: "xhigh"},
	}, nil)
	if !errors.Is(err, runprompt.ErrHeadlessGoalSession) {
		t.Fatal(err)
	}
	before, err := app.MetadataStore().ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.RunPrompt(t.Context(), serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "reject unsupported selection",
		Overrides: serverapi.RunPromptOverrides{Model: "gpt-5", ThinkingLevel: "ultra"},
	}, nil)
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
	_, err = client.RunPrompt(t.Context(), serverapi.RunPromptRequest{
		Intent:    serverapi.OpenExistingSessionLaunchIntent(id),
		Prompt:    "must not be submitted",
		Overrides: overrides,
	}, nil)
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
	testRunThinkingEffectiveSelection(t, true, serverapi.RunPromptOverrides{
		AgentRole: textutil.Value("worker"), Model: "gpt-4.1", ThinkingLevel: "high",
	}, config.DefaultSubagentRole, "gpt-5")
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
		Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: id.String()}},
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
	_, err = client.RunPrompt(t.Context(), serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "must not dispatch",
		Overrides: serverapi.RunPromptOverrides{ThinkingLevel: "high"},
	}, nil)
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
		Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: id.String()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if read.GetSession().Settings.Thinking.Kind != chatsettingspb.ThinkingKind_THINKING_KIND_CUSTOM {
		t.Fatal("fixture is not a custom-valued Thinking selection")
	}
	_, err = client.RunPrompt(t.Context(), serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "save another custom Thinking value",
		Overrides: serverapi.RunPromptOverrides{ThinkingLevel: "provider-depth-next"},
	}, nil)
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
	app, store, client := newRunThinkingSession(t, func(w http.ResponseWriter, r *http.Request) {
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
		_, err := client.RunPrompt(t.Context(), serverapi.RunPromptRequest{
			Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "hold this request",
		}, nil)
		done <- err
	}()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("first Run ended before dispatch: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("first Run did not dispatch")
	}
	_, err = client.RunPrompt(t.Context(), serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(id), Prompt: "must not dispatch",
		Overrides: serverapi.RunPromptOverrides{ThinkingLevel: "high"},
	}, nil)
	if !errors.Is(err, runprompt.ErrSessionRunning) {
		t.Fatalf("busy continuation error = %v", err)
	}
	read, err := app.ChatSettingsClient().ReadChatSettings(t.Context(), &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: id.String()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := read.GetSession().Settings.SelectedAgent.Thinking; got != "high" {
		t.Fatalf("busy Session Thinking = %q, want high", got)
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("first Run did not finish")
	}
}
