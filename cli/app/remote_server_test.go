package app

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

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

func TestRemoteAppServerReauthenticatePromptsWhenServerAuthAlreadyReady(t *testing.T) {
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
	}, memoryAuthHandler{state: auth.State{
		Scope: auth.ScopeGlobal,
		Method: auth.Method{
			Type:   auth.MethodAPIKey,
			APIKey: &auth.APIKeyMethod{Key: "old-key"},
		},
	}}, autoOnboarding{})
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

	pickerCalls := 0
	interactor := &interactiveAuthInteractor{
		lookupEnv: func(key string) string {
			if key == "OPENAI_API_KEY" {
				return "reauthed-key"
			}
			return ""
		},
		pickMethod: func(authInteraction) (authMethodPickerResult, error) {
			pickerCalls++
			return authMethodPickerResult{Choice: authMethodChoiceEnvAPIKey}, nil
		},
	}

	server := newRemoteAppServer(remote, cfg)
	if err := server.Reauthenticate(context.Background(), interactor); err != nil {
		t.Fatalf("Reauthenticate: %v", err)
	}
	if pickerCalls != 1 {
		t.Fatalf("expected remote /login to open auth picker once, got %d", pickerCalls)
	}
	state, err := srv.AuthManager().StoredState(context.Background())
	if err != nil {
		t.Fatalf("StoredState: %v", err)
	}
	if state.Method.APIKey == nil || state.Method.APIKey.Key != "reauthed-key" {
		t.Fatalf("expected forced remote reauth to replace auth, got %+v", state.Method)
	}
}

func TestRemoteAppServerEnsureAuthReadySkipsPickerWhenServerAuthAlreadyReady(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	registerAppWorkspace(t, workspace)

	cfg, err := config.Load(workspace, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	srv, err := serve.Start(context.Background(), serverstartup.Request{
		WorkspaceRoot:         workspace,
		WorkspaceRootExplicit: true,
	}, memoryAuthHandler{state: auth.State{
		Scope: auth.ScopeGlobal,
		Method: auth.Method{
			Type:   auth.MethodAPIKey,
			APIKey: &auth.APIKeyMethod{Key: "ready-key"},
		},
	}}, autoOnboarding{})
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

	interactor := &interactiveAuthInteractor{
		pickMethod: func(authInteraction) (authMethodPickerResult, error) {
			t.Fatal("startup auth readiness validation must not open auth picker when server auth is ready")
			return authMethodPickerResult{}, nil
		},
	}

	server := newRemoteAppServer(remote, cfg)
	if err := server.EnsureAuthReady(context.Background(), interactor); err != nil {
		t.Fatalf("EnsureAuthReady: %v", err)
	}

	state, err := srv.AuthManager().StoredState(context.Background())
	if err != nil {
		t.Fatalf("StoredState: %v", err)
	}
	if state.Method.APIKey == nil || state.Method.APIKey.Key != "ready-key" {
		t.Fatalf("expected startup validation to preserve ready auth, got %+v", state.Method)
	}
}

func TestRemoteLoginTransitionWaitsForAuthChoiceWhenServerAuthAlreadyReady(t *testing.T) {
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
	}, memoryAuthHandler{state: auth.State{
		Scope: auth.ScopeGlobal,
		Method: auth.Method{
			Type:   auth.MethodAPIKey,
			APIKey: &auth.APIKeyMethod{Key: "old-key"},
		},
	}}, autoOnboarding{})
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

	pickerEntered := make(chan struct{})
	releasePicker := make(chan struct{})
	interactor := &interactiveAuthInteractor{
		lookupEnv: func(key string) string {
			if key == "OPENAI_API_KEY" {
				return "reauthed-key"
			}
			return ""
		},
		pickMethod: func(authInteraction) (authMethodPickerResult, error) {
			close(pickerEntered)
			<-releasePicker
			return authMethodPickerResult{Choice: authMethodChoiceEnvAPIKey}, nil
		},
	}
	server := newRemoteAppServer(remote, cfg)
	done := make(chan error, 1)
	go func() {
		_, err := resolveSessionAction(context.Background(), server, interactor, "", "", UITransition{Action: UIActionLogout})
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

	state, err := srv.AuthManager().StoredState(context.Background())
	if err != nil {
		t.Fatalf("StoredState: %v", err)
	}
	if state.Method.APIKey == nil || state.Method.APIKey.Key != "reauthed-key" {
		t.Fatalf("expected remote login transition to replace auth after choice, got %+v", state.Method)
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

func TestRemoteAppServerCloseFnDoesNotImplyOwnership(t *testing.T) {
	server := newRemoteAppServerWithAuth(&client.Remote{}, config.App{}, func() error {
		return nil
	}, false)
	if server.OwnsServer() {
		t.Fatal("expected explicit non-owned remote server to stay non-owned")
	}
}

func TestRemoteAppServerDiscoveredRemoteIsNotOwned(t *testing.T) {
	server := newRemoteAppServer(&client.Remote{}, config.App{})
	if server.OwnsServer() {
		t.Fatal("expected configured remote server to not be owned")
	}
}
