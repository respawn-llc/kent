package sessiontarget

import (
	"context"
	"errors"
	"testing"

	"builder/cli/app/internal/targetstartup"
	"builder/shared/config"
)

type testServer struct {
	closed  bool
	closeFn func() error
}

func (s *testServer) Close() error {
	s.closed = true
	if s.closeFn != nil {
		return s.closeFn()
	}
	return nil
}

func TestRemoteTargetUsesServerClose(t *testing.T) {
	server := &testServer{}
	target := Remote(server)
	if target.Value != server {
		t.Fatalf("target value = %T, want test server", target.Value)
	}
	if err := target.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !server.closed {
		t.Fatal("expected server close called")
	}
}

func TestWrapDaemonLoadsConfigAndUsesDaemonClose(t *testing.T) {
	daemonClosed := false
	created := false
	target, err := WrapDaemon(targetstartup.DaemonTarget[*testRemote]{
		Value: &testRemote{},
		Close: func() error {
			daemonClosed = true
			return nil
		},
	}, WrapDaemonRequest[*testServer, *testRemote]{
		LoadConfig: func() (config.App, error) {
			return config.App{WorkspaceRoot: "/repo"}, nil
		},
		NewRemote: func(remote *testRemote, cfg config.App, closeFn func() error) *testServer {
			created = remote != nil && cfg.WorkspaceRoot == "/repo" && closeFn != nil
			return &testServer{closeFn: closeFn}
		},
	})
	if err != nil {
		t.Fatalf("WrapDaemon: %v", err)
	}
	if !created {
		t.Fatal("expected remote server factory called with daemon remote/config/close")
	}
	if err := target.Close(); err != nil {
		t.Fatalf("target close: %v", err)
	}
	if !daemonClosed {
		t.Fatal("expected daemon close called")
	}
}

func TestWrapDaemonReturnsConfigError(t *testing.T) {
	configErr := errors.New("config failed")
	_, err := WrapDaemon(targetstartup.DaemonTarget[*testRemote]{}, WrapDaemonRequest[*testServer, *testRemote]{
		LoadConfig: func() (config.App, error) {
			return config.App{}, configErr
		},
		NewRemote: func(*testRemote, config.App, func() error) *testServer {
			t.Fatal("factory should not be called")
			return nil
		},
	})
	if !errors.Is(err, configErr) {
		t.Fatalf("error = %v, want config error", err)
	}
}

func TestWrapDaemonRequiresConfigLoader(t *testing.T) {
	_, err := WrapDaemon(targetstartup.DaemonTarget[*testRemote]{}, WrapDaemonRequest[*testServer, *testRemote]{
		NewRemote: func(*testRemote, config.App, func() error) *testServer {
			return &testServer{}
		},
	})
	if !errors.Is(err, ErrConfigLoaderRequired) {
		t.Fatalf("error = %v, want ErrConfigLoaderRequired", err)
	}
}

func TestWrapDaemonRequiresRemoteFactory(t *testing.T) {
	_, err := WrapDaemon(targetstartup.DaemonTarget[*testRemote]{}, WrapDaemonRequest[*testServer, *testRemote]{
		LoadConfig: func() (config.App, error) {
			return config.App{}, nil
		},
	})
	if !errors.Is(err, ErrRemoteFactoryRequired) {
		t.Fatalf("error = %v, want ErrRemoteFactoryRequired", err)
	}
}

func TestShouldBypassRemoteForFirstRunRequiresInteractiveMissingSettings(t *testing.T) {
	if !ShouldBypassRemoteForFirstRun(true, false) {
		t.Fatal("interactive first run should bypass remote")
	}
	if ShouldBypassRemoteForFirstRun(false, false) {
		t.Fatal("non-interactive run should not bypass remote")
	}
	if ShouldBypassRemoteForFirstRun(true, true) {
		t.Fatal("existing settings should not bypass remote")
	}
}

func TestValidateSkipsEmbeddedAndReauthenticatesRemote(t *testing.T) {
	calls := 0
	server := &testServer{}
	if err := Validate(context.Background(), targetstartup.SourceEmbedded, server, func(context.Context, *testServer) error {
		calls++
		return nil
	}); err != nil {
		t.Fatalf("embedded Validate: %v", err)
	}
	if calls != 0 {
		t.Fatalf("embedded reauth calls = %d, want 0", calls)
	}
	if err := Validate(context.Background(), targetstartup.SourceRemote, server, func(context.Context, *testServer) error {
		calls++
		return nil
	}); err != nil {
		t.Fatalf("remote Validate: %v", err)
	}
	if calls != 1 {
		t.Fatalf("remote reauth calls = %d, want 1", calls)
	}
}

func TestValidateAllowsMissingReauthCallback(t *testing.T) {
	if err := Validate(context.Background(), targetstartup.SourceRemote, &testServer{}, nil); err != nil {
		t.Fatalf("Validate without reauth callback: %v", err)
	}
}

type testRemote struct{}
