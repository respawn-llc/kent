package app

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"core/cli/app/commands"
	"core/internal/testharness/testsetup"
	"core/shared/config"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
)

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
	_, workspace := newRegisteredAppWorkspace(t)
	t.Setenv("REMOTE_TEST_KEY", "ready-key")
	fixture := startRemoteAuthTestFixture(t, workspace)
	original, err := fixture.config.Settings.Connection.ConcreteID()
	if err != nil {
		t.Fatal(err)
	}
	definition := fixture.config.Settings.Connections[*original]
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
		composition.model.Update(composition.model.loadNewSessionSettingsCmd(nil)())
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
		result := composition.model.newSessionActionCmd(chatShellAction{command: "true"}, nil)().(newSessionActionDoneMsg)
		if result.err != nil || result.session == nil {
			t.Fatalf("first action: %v", result.err)
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
	_, workspace := newRegisteredAppWorkspace(t)
	fixture := startRemoteAuthTestFixture(t, workspace)
	server, err := ensureInteractiveProjectBinding(t.Context(), fixture.server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	previous := runtimeids.NewSessionID()
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
	model.inputController().handleBackCommand()
	if model.nextSessionID != previous.String() || model.exitAction != UIActionOpenSession {
		t.Fatal("New Session back did not open its originating Session")
	}
}

func TestNewSessionSettingsEditsStayLocal(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)
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
	read := model.loadNewSessionSettingsCmd(nil)
	model.Update(read())
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
