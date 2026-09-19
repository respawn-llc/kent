package app

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

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
	}, autoOnboarding)

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
	return remoteAuthTestFixture{daemon: daemon, server: newRemoteAppServerWithAuth(remote, cfg), config: cfg}
}

func TestRemoteAppServerReauthenticateConfiguresServerOwnedAuth(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)
	t.Setenv("REMOTE_TEST_KEY", "reauthed-key")
	fixture := startRemoteAuthTestFixture(t, workspace)
	if err := fixture.server.Reauthenticate(context.Background(), newHeadlessAuthInteractor(), false); err != nil {
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

func TestRemoteAppServerReauthenticatePromptsWhenServerAuthAlreadyReady(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)
	t.Setenv("REMOTE_TEST_KEY", "reauthed-key")
	fixture := startRemoteAuthTestFixture(t, workspace)

	pickerCalls := 0
	interactor := &interactiveAuthInteractor{
		pickMethod: func(authInteraction) (authMethodPickerResult, error) {
			pickerCalls++
			return authMethodPickerResult{Choice: authMethodChoiceEnvAPIKey}, nil
		},
	}

	if err := fixture.server.Reauthenticate(context.Background(), interactor, true); err != nil {
		t.Fatalf("Reauthenticate: %v", err)
	}
	if pickerCalls != 1 {
		t.Fatalf("expected remote /login to open auth picker once, got %d", pickerCalls)
	}
	state, err := fixture.daemon.AuthManager().Load(context.Background())
	if err != nil {
		t.Fatalf("StoredState: %v", err)
	}
	if len(state.Connections) != 0 {
		t.Fatalf("environment key was persisted: %+v", state)
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

	if err := fixture.server.EnsureAuthReady(context.Background(), interactor, true); err != nil {
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

func TestRemoteLoginTransitionWaitsForAuthChoiceWhenServerAuthAlreadyReady(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)
	t.Setenv("REMOTE_TEST_KEY", "reauthed-key")
	fixture := startRemoteAuthTestFixture(t, workspace)

	pickerEntered := make(chan struct{})
	releasePicker := make(chan struct{})
	interactor := &interactiveAuthInteractor{
		pickMethod: func(authInteraction) (authMethodPickerResult, error) {
			close(pickerEntered)
			<-releasePicker
			return authMethodPickerResult{Choice: authMethodChoiceEnvAPIKey}, nil
		},
	}
	done := make(chan error, 1)
	go func() {
		_, err := resolveSessionAction(context.Background(), fixture.server, interactor, "", UITransition{Action: UIActionLogout})
		done <- err
	}()

	select {
	case <-pickerEntered:
	case err := <-done:
		t.Fatalf("login transition returned before auth picker opened: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("auth picker did not open")
	}
	select {
	case err := <-done:
		t.Fatalf("login transition returned while auth picker was waiting: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releasePicker)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("login transition after auth choice: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("login transition did not finish after auth choice")
	}

	state, err := fixture.daemon.AuthManager().Load(context.Background())
	if err != nil {
		t.Fatalf("StoredState: %v", err)
	}
	if len(state.Connections) != 0 {
		t.Fatalf("environment key was persisted: %+v", state)
	}
}
