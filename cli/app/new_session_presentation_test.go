package app

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"core/cli/app/commands"
	"core/shared/config"
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
