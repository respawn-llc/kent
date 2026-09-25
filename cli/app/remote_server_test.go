package app

import (
	"context"
	"errors"
	"os"
	"testing"

	"core/internal/testharness/testsetup"
	serverstartup "core/server/startup"
	"core/shared/client"
	"core/shared/config"
)

type remoteAuthTestFixture struct {
	daemon *serverstartup.ServeServer
	server *remoteAppServer
	config config.App
}

func startRemoteAuthTestFixture(t *testing.T, workspace string) remoteAuthTestFixture {
	t.Helper()
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	cfg.Settings = testsetup.WithResponsesProvider(cfg.Settings, "http://127.0.0.1:1/v1")
	keyName := "REMOTE_TEST_KEY"
	definition := cfg.Settings.Connections[*cfg.Settings.Connection]
	definition.EnvironmentVariable = &keyName
	cfg.Settings.Connections[*cfg.Settings.Connection] = definition
	cfg.Settings = testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, cfg.Settings)
	daemon, err := serverstartup.StartServeServer(context.Background(), serverstartup.Request{
		WorkspaceRoot:         workspace,
		WorkspaceRootExplicit: true,
	})

	if err != nil {
		t.Fatalf("StartServeServer: %v", err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	t.Cleanup(serveAppServer(t, daemon))
	waitForConfiguredRemoteIdentity(t, workspace)
	remote, err := client.DialRemoteURL(context.Background(), config.ServerRPCURL(cfg))
	if err != nil {
		t.Fatalf("DialRemoteURL: %v", err)
	}
	t.Cleanup(func() { _ = remote.Close() })
	return remoteAuthTestFixture{daemon: daemon, server: newRemoteAppServerWithAuth(remote, cfg.Connection(), config.LocalPreferences{Theme: cfg.Settings.Theme}), config: cfg}
}

func TestRemoteAppServerReauthenticateConfiguresServerOwnedAuth(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)
	t.Setenv("REMOTE_TEST_KEY", "reauthed-key")
	fixture := startRemoteAuthTestFixture(t, workspace)
	if err := fixture.server.EnsureAuthReady(context.Background(), *fixture.config.Settings.Connection, newHeadlessAuthInteractor(), false); err != nil {
		t.Fatalf("Reauthenticate: %v", err)
	}

	state, err := fixture.daemon.AuthManager().Load(context.Background())
	if err != nil {
		t.Fatalf("StoredState: %v", err)
	}
	if len(state.Connections) != 0 {
		t.Fatalf("environment key was persisted: %+v", state)
	}
	if _, err := os.Stat(config.GlobalAuthConfigPath(fixture.config)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected client auth file to remain absent, got %v", err)
	}
}

func TestRemoteAppServerEnsureAuthReadySkipsPickerWhenServerAuthAlreadyReady(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)
	t.Setenv("REMOTE_TEST_KEY", "ready-key")
	fixture := startRemoteAuthTestFixture(t, workspace)

	interactor := &interactiveAuthInteractor{
		pickMethod: func(authInteraction) (authMethodPickerResult, error) {
			t.Fatal("startup auth readiness validation must not open auth picker when server auth is ready")
			return authMethodPickerResult{}, nil
		},
	}

	if err := fixture.server.EnsureAuthReady(context.Background(), *fixture.config.Settings.Connection, interactor, true); err != nil {
		t.Fatalf("EnsureAuthReady: %v", err)
	}

	state, err := fixture.daemon.AuthManager().Load(context.Background())
	if err != nil {
		t.Fatalf("StoredState: %v", err)
	}
	if len(state.Connections) != 0 {
		t.Fatalf("environment key was persisted: %+v", state)
	}
}
