package app

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strconv"
	"testing"

	"builder/server/metadata"
	"builder/server/serve"
	"builder/server/session"
	"builder/shared/config"
)

func registerAppWorkspace(t *testing.T, workspace string) {
	t.Helper()
	configureAppTestServerPort(t)
	cfg, err := config.Load(workspace, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	_ = mustRegisterAppBinding(t, cfg.PersistenceRoot, cfg.WorkspaceRoot)
}

func newAppTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func newRegisteredAppWorkspace(t *testing.T) (home string, workspace string) {
	t.Helper()
	home = newAppTestHome(t)
	workspace = t.TempDir()
	registerAppWorkspace(t, workspace)
	return home, workspace
}

func serveAppServer(t *testing.T, srv *serve.Server) func() {
	t.Helper()
	serveCtx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(serveCtx)
	}()
	return func() {
		cancel()
		if serveErr := <-errCh; !errors.Is(serveErr, context.Canceled) {
			t.Fatalf("Serve error = %v, want context canceled", serveErr)
		}
	}
}

func configureAppTestServerPort(t *testing.T) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve server port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	serve.ReserveTestListenReservation(listener)
	t.Cleanup(func() { serve.ReleaseTestListenReservation(listener.Addr().String()) })
	t.Setenv("BUILDER_SERVER_HOST", "127.0.0.1")
	t.Setenv("BUILDER_SERVER_PORT", strconv.Itoa(port))
}

func mustRegisterAppBinding(t *testing.T, persistenceRoot string, workspaceRoot string) metadata.Binding {
	t.Helper()
	binding, err := metadata.RegisterBinding(context.Background(), persistenceRoot, workspaceRoot)
	if err != nil {
		t.Fatalf("RegisterBinding: %v", err)
	}
	return binding
}

func createAuthoritativeAppSession(t *testing.T, persistenceRoot string, workspaceRoot string) *session.Store {
	t.Helper()
	binding := mustRegisterAppBinding(t, persistenceRoot, workspaceRoot)
	metadataStore, err := metadata.Open(persistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	// Keep the metadata store alive for the lifetime of the session store so
	// persistence observer writes continue to succeed during the test.
	store, err := session.Create(
		config.ProjectSessionsRoot(config.App{PersistenceRoot: persistenceRoot}, binding.ProjectID),
		filepath.Base(filepath.Clean(workspaceRoot)),
		workspaceRoot,
		metadataStore.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		_ = metadataStore.Close()
		t.Fatalf("session.Create: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		_ = metadataStore.Close()
		t.Fatalf("EnsureDurable: %v", err)
	}
	t.Cleanup(func() { _ = metadataStore.Close() })
	return store
}

func openAuthoritativeAppSession(t *testing.T, persistenceRoot string, sessionID string) *session.Store {
	t.Helper()
	metadataStore, err := metadata.Open(persistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	store, err := session.OpenByID(persistenceRoot, sessionID, metadataStore.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		_ = metadataStore.Close()
		t.Fatalf("session.OpenByID: %v", err)
	}
	t.Cleanup(func() { _ = metadataStore.Close() })
	return store
}
