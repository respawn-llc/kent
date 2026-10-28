package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"core/internal/testharness/testsetup"
	"core/shared/config"
	"core/shared/serverapi"
)

func TestStartupAttachDoesNotRequireProviderSelection(t *testing.T) {
	_, workspace := newConfiguredAppWorkspace(t)
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	if err := os.WriteFile(filepath.Join(cfg.PersistenceRoot, "config.toml"), []byte("model = \"gpt-6-astra\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg = loadAppTestConfig(t, workspace, config.LoadOptions{})
	store := testsetup.OpenStore(t, cfg.PersistenceRoot)
	if _, err := store.RegisterWorkspaceBinding(t.Context(), workspace); err != nil {
		t.Fatal(err)
	}
	daemon, _ := newMemoryAppTestServer(t, cfg, store, config.LoadOptions{})
	t.Cleanup(serveAppServer(t, daemon))
	waitForConfiguredRunPromptDaemon(t, workspace)
	connection, err := config.LoadConnectionDiscovery(workspace, config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	remote, err := attachConfiguredStartupRemote(t.Context(), connection)
	if err != nil {
		t.Fatalf("attach before provider setup: %v", err)
	}
	if err := remote.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNewSessionMissingConnectionCredentialsDoesNotOpenLogin(t *testing.T) {
	useStartupTestTerminal(t)
	_, workspace := newConfiguredAppWorkspace(t)
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	if err := os.WriteFile(filepath.Join(cfg.PersistenceRoot, "config.toml"), []byte("connection = \"subscription\"\n[connections.subscription]\nprotocol = \"chatgpt-codex\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg = loadAppTestConfig(t, workspace, config.LoadOptions{})
	store := testsetup.OpenStore(t, cfg.PersistenceRoot)
	if _, err := store.RegisterWorkspaceBinding(t.Context(), workspace); err != nil {
		t.Fatal(err)
	}
	daemon, _ := newMemoryAppTestServer(t, cfg, store, config.LoadOptions{})
	t.Cleanup(serveAppServer(t, daemon))
	waitForConfiguredRunPromptDaemon(t, workspace)
	opened := false
	interactor := &interactiveAuthInteractor{pickMethod: func(authInteraction) (authMethodPickerResult, error) {
		opened = true
		return authMethodPickerResult{Canceled: true}, nil
	}}
	server, err := startSessionServer(context.Background(), Options{WorkspaceRoot: workspace, WorkspaceRootExplicit: true}, interactor, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	if opened {
		t.Fatal("startup checked provider credentials before selecting a Session")
	}
	bound, err := ensureInteractiveProjectBinding(t.Context(), server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bound.Close() }()
	request, err := prepareNewSessionUIRun(t.Context(), bound, nil, serverapi.RunPromptOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	composition, err := composeUIProgram(request, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer composition.close()
	composition.model.mainEditor.SetText("editable before login")
	if opened {
		t.Fatal("New Session opened automatic login")
	}
}
