package sessiontarget

import (
	"errors"
	"testing"

	"core/cli/app/internal/targetstartup"
	"core/shared/config"
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

type testRemote struct{}
