package app

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"core/cli/app/commands"
	"core/cli/tui/ongoing"
	"core/internal/testharness/pty"
	"core/internal/testharness/testsetup"
	"core/shared/config"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	tea "github.com/charmbracelet/bubbletea"
)

func TestNewSessionSettingsFailureRepaintsNativeComposer(t *testing.T) {
	var output bytes.Buffer
	model := sizedTestUIModel(newProjectedStaticUIModel(
		WithUIOngoingSurface(ongoing.NewSurface(&output))), 100, 24)
	target := &chatpb.NewChatTarget{}
	model.presentationTarget = &uiNewSessionTarget{creation: target}
	failure := io.ErrUnexpectedEOF
	model.chatSettings = failingHeaderSettingsReader{err: failure}
	read := model.loadNewSessionSettingsCmd(nil)
	if !model.statusLineSpinning() || !model.shouldAnimateSpinner() || model.spinnerTickToken == 0 {
		t.Fatal("settings read did not activate the composer spinner")
	}
	model.renderNativeOngoingSurface()
	model.Update(newSessionSettingsReadResult(t, read))
	if model.statusLineSpinning() {
		t.Fatal("failed settings read left the composer spinner active")
	}
	assertVisibleFailure := func() {
		t.Helper()
		capture, err := pty.NewCapture(pty.MustDimensions(24, 100), []pty.Chunk{pty.NewChunk(0, 0, output.Bytes())})
		if err != nil {
			t.Fatal(err)
		}
		analysis, err := pty.Analyze(capture)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(analysis.Screen.RenderText(), failure.Error()) {
			t.Fatalf("settings failure absent from visible terminal: %s", analysis.Screen.RenderText())
		}
	}
	assertVisibleFailure()
	actions := &concurrentNewChatClient{}
	model.chatMutations = actions
	testSetMainInput(model, "retained input")
	_, retry := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, msg := range collectCmdMessages(t, retry) {
		model.Update(msg)
	}
	assertVisibleFailure()
	if model.newSessionTarget() == nil || len(actions.requests) != 0 || model.mainEditor.Text() != "retained input" {
		t.Fatal("failed settings retry created a Session, submitted or lost the draft")
	}
}

func TestNewSessionSelectedCompactionRespectsServerPolicy(t *testing.T) {
	for _, test := range []struct {
		policy    chatsettingspb.AutoCompactionPolicy
		stored    bool
		effective bool
	}{
		{chatsettingspb.AutoCompactionPolicy_AUTO_COMPACTION_POLICY_DISABLED, true, false},
		{chatsettingspb.AutoCompactionPolicy_AUTO_COMPACTION_POLICY_REQUIRED, false, true},
		{chatsettingspb.AutoCompactionPolicy_AUTO_COMPACTION_POLICY_OPTIONAL, true, true},
		{chatsettingspb.AutoCompactionPolicy_AUTO_COMPACTION_POLICY_OPTIONAL, false, false},
	} {
		model := newUIModelDefaults(nil)
		choice := &chatsettingspb.NewChatAgentChoice{
			Agent:          &chatsettingspb.AgentChoice{Role: "default"},
			Supervisor:     &chatsettingspb.Supervisor{},
			Questions:      &chatsettingspb.Questions{},
			AutoCompaction: &chatsettingspb.AutoCompaction{Policy: test.policy},
		}
		model.presentationTarget = &uiNewSessionTarget{
			creation: &chatpb.NewChatTarget{InitialSettings: &chatsettingspb.InitialChatSettings{
				AgentRole: "default", AutoCompactionEnabled: &test.stored,
			}},
			settingsLoad: uiNewSettingsReady{choices: []*chatsettingspb.NewChatAgentChoice{choice}},
		}
		settings := model.projectedNewSessionSettings()
		if settings.AutoCompaction.Stored != test.stored || settings.AutoCompaction.Effective != test.effective {
			t.Fatalf("policy %v: stored=%t effective=%t", test.policy, settings.AutoCompaction.Stored, settings.AutoCompaction.Effective)
		}
	}
}

func TestNewSessionPresentationLeavesArtifactsAndCatalogUnchanged(t *testing.T) {
	t.Setenv("REMOTE_TEST_KEY", "ready-key")
	fixture := startRemoteAuthTestFixture(t)
	var providerCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer provider.Close()
	original, err := fixture.config.Settings.Connection.ConcreteID()
	if err != nil {
		t.Fatal(err)
	}
	definition := fixture.config.Settings.Connections[*original]
	providerURL := provider.URL + "/v1"
	definition.Endpoint = &providerURL
	fixture.config.Settings.Connections = map[config.ConnectionID]config.ProviderConnection{"account-a": definition, "account-b": definition}
	selection := config.ConnectionSelection{"account-a", "account-b"}
	fixture.config.Settings.Connection = &selection
	fixture.config.Settings = testsetup.WriteProviderSettings(t, fixture.config.PersistenceRoot, fixture.config.Settings)
	server, err := ensureInteractiveProjectBinding(t.Context(), fixture.server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	sessionDir := filepath.Dir(config.ProjectSessionDir(fixture.config, server.ProjectID(), "unused"))
	readArtifacts := func() int {
		t.Helper()
		entries, err := os.ReadDir(sessionDir)
		if os.IsNotExist(err) {
			return 0
		}
		if err != nil {
			t.Fatal(err)
		}
		return len(entries)
	}
	readCatalog := func() int {
		t.Helper()
		page, err := (projectScopedSessionPageLoader{
			projectID: server.ProjectID(), client: server.ProjectViewClient(),
		}).ListSessionPage(t.Context(), sessionPageRequest{
			ProjectID: server.ProjectID(), Category: sessioncontract.SessionCategoryMain,
		})
		if err != nil {
			t.Fatal(err)
		}
		return len(page.Sessions)
	}
	artifacts, catalog := readArtifacts(), readCatalog()
	newComposer := func() *uiProgramComposition {
		t.Helper()
		request, err := prepareNewSessionUIRun(t.Context(), server, nil, serverapi.RunPromptOverrides{})
		if err != nil {
			t.Fatal(err)
		}
		composition, err := composeUIProgram(request, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		composition.model.Update(newSessionSettingsReadResult(t, composition.model.loadNewSessionSettingsCmd(nil)))
		if composition.model.newChatTarget().InitialSettings == nil {
			t.Fatal("initial settings did not resolve")
		}
		return composition
	}
	for range 5 {
		composition := newComposer()
		if got := composition.model.mainEditor.Text(); got != "" {
			t.Fatalf("fresh New composer retained draft %q", got)
		}
		composition.model.mainEditor.SetText("abandoned\nunsent draft")
		_, _ = composition.model.inputController().handleSessionNameCommand("temporary name")
		_, _ = composition.model.inputController().handleCopyCommand()
		_, _ = composition.model.inputController().handleBackCommand()
		_, _ = composition.model.inputController().applyCommandResultWithPreSubmitQueuePosition(commands.Result{
			Action: commands.ActionNew,
		}, preSubmitQueueBack)
		if got := composition.model.Transition().Action; got != UIActionNewSession {
			t.Fatalf("New navigation = %q", got)
		}
		composition.close()
		if got := readArtifacts(); got != artifacts {
			t.Fatalf("abandonment created %d artifacts", got-artifacts)
		}
		if got := readCatalog(); got != catalog {
			t.Fatalf("abandonment created %d catalog entries", got-catalog)
		}
	}
	if got := readArtifacts(); got != artifacts {
		t.Fatalf("Session artifacts = %d, want %d", got, artifacts)
	}
	if got := readCatalog(); got != catalog {
		t.Fatalf("Session catalog entries = %d, want %d", got, catalog)
	}
	failed := newComposer()
	failed.model.newChatTarget().WorkspaceId = runtimeids.NewSessionID().String()
	rejected := failed.model.newSessionActionCmd(chatShellAction{command: "true"}, nil)().(newSessionActionDoneMsg)
	if rejected.err == nil || rejected.session != nil {
		t.Fatalf("missing workspace creation: %v, %v", rejected.session, rejected.err)
	}
	failed.model.Update(rejected)
	failed.close()
	if readCatalog() != catalog || readArtifacts() != artifacts {
		t.Fatal("failed creation was persisted or replayed")
	}
	for index, want := range []string{"account-a", "account-b", "account-a"} {
		composition := newComposer()
		result := composition.model.newSessionInputCmd("$ true", runtimeinput.Text("$ true"), newSessionInputEnter)().(newSessionActionDoneMsg)
		if result.err != nil || result.session == nil {
			t.Fatalf("first action: %v", result.err)
		}
		if providerCalls.Load() != 0 {
			t.Fatalf("shell first action contacted provider %d times", providerCalls.Load())
		}
		id, err := runtimeids.ParseSessionID(result.session.SessionId)
		if err != nil {
			t.Fatal(err)
		}
		intent, err := protoapi.SessionLaunchRequestIntentToProto(serverapi.OpenExistingSessionLaunchIntent(id))
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			plan, err := server.SessionLaunchClient().PlanSession(t.Context(), &sessionlaunchpb.SessionPlanRequest{
				Mode: sessionlaunchpb.SessionLaunchMode_SESSION_LAUNCH_MODE_INTERACTIVE, Intent: intent,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := plan.Plan.ActiveSettings.GetConnection(); got != want {
				t.Fatalf("assignment %d after five abandoned settings reads = %q, want %q", index, got, want)
			}
		}
		composition.close()
		if got := readCatalog(); got != catalog+index+1 {
			t.Fatalf("work created %d Sessions, want %d", got-catalog, index+1)
		}
	}
}

func TestNewSessionBackUsesNavigationOnlyOrigin(t *testing.T) {
	fixture := startRemoteAuthTestFixture(t)
	server, err := ensureInteractiveProjectBinding(t.Context(), fixture.server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	createIntent, err := protoapi.SessionLaunchRequestIntentToProto(serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()))
	if err != nil {
		t.Fatal(err)
	}
	created, err := server.SessionLaunchClient().PlanSession(t.Context(), &sessionlaunchpb.SessionPlanRequest{
		Mode:   sessionlaunchpb.SessionLaunchMode_SESSION_LAUNCH_MODE_INTERACTIVE,
		Intent: createIntent,
	})
	if err != nil {
		t.Fatal(err)
	}
	previous, err := runtimeids.ParseSessionID(created.Plan.SessionId)
	if err != nil {
		t.Fatal(err)
	}
	draft := "saved origin\n draft "
	_, err = server.SessionLifecycleClient().PersistInputDraft(t.Context(), &sessionlaunchpb.SessionPersistInputDraftRequest{
		SessionId: previous.String(), Input: draft,
	})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := protoapi.SessionLaunchRequestIntentToProto(serverapi.CreateNewSessionLaunchIntent(serverapi.PreviousSessionCreateOrigin(previous)))
	if err != nil {
		t.Fatal(err)
	}
	request, err := prepareNewSessionUIRun(t.Context(), server, intent.GetCreateNew(), serverapi.RunPromptOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	composition, err := composeUIProgram(request, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer composition.close()
	model := composition.model
	if request.newChat.Origin.GetNavigationOnlySessionId().GetSessionId() != previous.String() {
		t.Fatal("TUI New retained context-inheriting creation")
	}
	testSetMainInput(model, "/back")
	matches := model.currentSlashCommandMatches("back")
	if len(matches) != 1 || matches[0].Name != "back" {
		t.Fatalf("originating navigation unavailable in command picker: %v", matches)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.nextSessionID != previous.String() || model.exitAction != UIActionOpenSession {
		t.Fatal("New Session back did not open its originating Session")
	}
	resolved, err := resolveSessionAction(t.Context(), server, newHeadlessAuthInteractor(), "", model.Transition())
	if err != nil {
		t.Fatal(err)
	}
	if resolved.GetLaunch().GetIntent().GetOpenExistingSessionId() != previous.String() {
		t.Fatalf("back transition = %v", resolved)
	}
	_, _, input, override, err := sessionLaunchPreparationValues(resolved.GetLaunch().Preparation)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := sessionLaunchInitialStateFromServer(t.Context(), server, previous.String(), input, override)
	if err != nil || initial.Input != draft {
		t.Fatalf("origin draft = %v, error = %v", initial.Input, err)
	}
}

func TestNewSessionSettingsEditsStayLocal(t *testing.T) {
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
	read := model.loadNewSessionSettingsCmd(nil)
	model.Update(newSessionSettingsReadResult(t, read))
	initial := model.newChatTarget().InitialSettings
	if initial == nil {
		t.Fatal("New Session settings did not load")
	}
	if model.statusConfig.Settings.ModelContextWindow != fixture.config.Settings.ModelContextWindow {
		t.Fatalf("New status context window = %d, want %d", model.statusConfig.Settings.ModelContextWindow, fixture.config.Settings.ModelContextWindow)
	}
	model.statusRefreshCmd()
	snapshot := model.status.snapshot
	capacity := fixture.config.Settings.ModelContextWindow
	if snapshot.Context.WindowTokens != capacity || snapshot.Context.AvailableTokens != capacity ||
		snapshot.Context.UsedTokens != 0 || snapshot.CompactionCount != 0 {
		t.Fatalf("New status context = %+v, compactions = %d; want unused capacity %d", snapshot.Context, snapshot.CompactionCount, capacity)
	}
	selected := !*initial.QuestionsEnabled
	model.chatSettingsMutationCommand(&chatsettingspb.MutationOperation{
		Operation: &chatsettingspb.MutationOperation_QuestionsEnabled{QuestionsEnabled: selected},
	})
	if got := *model.newChatTarget().InitialSettings.QuestionsEnabled; got != selected {
		t.Fatalf("selected Questions = %t, want %t", got, selected)
	}
	page, err := (projectScopedSessionPageLoader{
		projectID: server.ProjectID(), client: server.ProjectViewClient(),
	}).ListSessionPage(t.Context(), sessionPageRequest{
		ProjectID: server.ProjectID(), Category: sessioncontract.SessionCategoryMain,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sessions) != 0 {
		t.Fatal("settings edit created a Session")
	}
}
