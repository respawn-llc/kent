package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	serverstartup "core/server/startup"
	"core/shared/config"
	"core/shared/serverapi"
)

func TestStartupAttachDoesNotRequireProviderSelection(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	if err := os.WriteFile(filepath.Join(cfg.PersistenceRoot, "config.toml"), []byte("model = \"gpt-6-astra\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	daemon, err := serverstartup.StartServeServer(t.Context(), serverstartup.Request{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	t.Cleanup(serveAppServer(t, daemon))
	waitForConfiguredRunPromptDaemon(t, workspace)
	remote, err := attachConfiguredStartupRemote(t.Context(), cfg)
	if err != nil {
		t.Fatalf("attach before provider setup: %v", err)
	}
	if err := remote.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSelectedSessionMissingConnectionCredentialsOpensLogin(t *testing.T) {
	useStartupTestTerminal(t)
	_, workspace := newRegisteredAppWorkspace(t)
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	if err := os.WriteFile(filepath.Join(cfg.PersistenceRoot, "config.toml"), []byte("connection = \"subscription\"\n[connections.subscription]\nprotocol = \"chatgpt-codex\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	daemon, err := serverstartup.StartServeServer(t.Context(), serverstartup.Request{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
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
	intent := serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin())
	err = runSessionLifecycleWithOptions(t.Context(), server, interactor, sessionLifecycleOptions{Intent: &intent})
	if !opened || !errors.Is(err, ErrAuthCanceledByUser) {
		t.Fatalf("login opened = %t, startup error = %v", opened, err)
	}
}
