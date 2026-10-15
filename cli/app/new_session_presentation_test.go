package app

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"core/cli/app/commands"
	"core/shared/config"
	"core/shared/protoapi"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
)

func TestNewSessionPresentationLeavesArtifactsAndCatalogUnchanged(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)
	fixture := startRemoteAuthTestFixture(t, workspace)
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
	for range 3 {
		request, err := prepareNewSessionUIRun(t.Context(), server, nil, serverapi.RunPromptOverrides{})
		if err != nil {
			t.Fatal(err)
		}
		composition, err := composeUIProgram(request, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
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
	}
	if got := readArtifacts(); got != artifacts {
		t.Fatalf("Session artifacts = %d, want %d", got, artifacts)
	}
	if got := readCatalog(); got != catalog {
		t.Fatalf("Session catalog entries = %d, want %d", got, catalog)
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
