package app

import (
	"context"
	"errors"
	"os"
	"testing"

	"builder/server/auth"
	"builder/server/serve"
	serverstartup "builder/server/startup"
	"builder/shared/client"
	"builder/shared/config"
)

func TestRemoteAppServerReauthenticateConfiguresServerOwnedAuth(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENAI_API_KEY", "reauthed-key")
	registerAppWorkspace(t, workspace)

	cfg, err := config.Load(workspace, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	srv, err := serve.Start(context.Background(), serverstartup.Request{
		WorkspaceRoot:         workspace,
		WorkspaceRootExplicit: true,
		AllowUnauthenticated:  true,
	}, memoryAuthHandler{state: auth.EmptyState()}, autoOnboarding{})
	if err != nil {
		t.Fatalf("serve.Start: %v", err)
	}
	defer func() { _ = srv.Close() }()
	serveCtx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(serveCtx) }()
	defer func() {
		cancel()
		if serveErr := <-errCh; !errors.Is(serveErr, context.Canceled) {
			t.Fatalf("Serve error = %v, want context canceled", serveErr)
		}
	}()
	waitForConfiguredRemoteIdentity(t, workspace)

	remote, err := client.DialRemoteURL(context.Background(), config.ServerRPCURL(cfg))
	if err != nil {
		t.Fatalf("DialRemoteURL: %v", err)
	}
	defer func() { _ = remote.Close() }()

	server := newRemoteAppServer(remote, cfg)
	if err := server.Reauthenticate(context.Background(), newHeadlessAuthInteractor()); err != nil {
		t.Fatalf("Reauthenticate: %v", err)
	}

	state, err := srv.AuthManager().StoredState(context.Background())
	if err != nil {
		t.Fatalf("StoredState: %v", err)
	}
	if state.Method.APIKey == nil || state.Method.APIKey.Key != "reauthed-key" {
		t.Fatalf("unexpected stored auth state: %+v", state.Method)
	}
	if _, err := os.Stat(config.GlobalAuthConfigPath(cfg)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected client auth file to remain absent, got %v", err)
	}
}

func TestRemoteAppServerCloseUsesOwnedCloser(t *testing.T) {
	called := false
	server := newRemoteAppServerWithClose(&client.Remote{}, config.App{}, func() error {
		called = true
		return nil
	})
	if !server.OwnsServer() {
		t.Fatal("expected launched remote server to be owned")
	}
	if err := server.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !called {
		t.Fatal("expected owned remote closer to be invoked")
	}
}

func TestRemoteAppServerDiscoveredRemoteIsNotOwned(t *testing.T) {
	server := newRemoteAppServer(&client.Remote{}, config.App{})
	if server.OwnsServer() {
		t.Fatal("expected configured remote server to not be owned")
	}
}
