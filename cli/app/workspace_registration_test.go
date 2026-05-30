package app

import (
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"testing"

	"builder/server/llm"
	"builder/server/metadata"
	"builder/server/projectview"
	"builder/server/runtime"
	"builder/server/serve"
	"builder/server/session"
	"builder/server/tools"
	"builder/shared/client"
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

func loadAppTestConfig(t *testing.T, workspace string, opts config.LoadOptions) config.App {
	t.Helper()
	cfg, err := config.Load(workspace, opts)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

func newAppMetadataProjectViewClient(t *testing.T, cfg config.App) client.ProjectViewClient {
	t.Helper()
	store, err := metadata.Open(cfg.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service, err := projectview.NewMetadataService(store, "")
	if err != nil {
		t.Fatalf("NewMetadataService: %v", err)
	}
	return client.NewLoopbackProjectViewClient(service)
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

func prepareAppRuntimePlan(t *testing.T, server launchPlannerServer, req sessionLaunchRequest, diagnosticWriter io.Writer, startLogLine string) (sessionLaunchPlan, *runtimeLaunchPlan) {
	t.Helper()
	planner := newSessionLaunchPlanner(server)
	plan, err := planner.PlanSession(context.Background(), req)
	if err != nil {
		t.Fatalf("PlanSession: %v", err)
	}
	runtimePlan, err := planner.PrepareRuntime(context.Background(), plan, diagnosticWriter, startLogLine)
	if err != nil {
		t.Fatalf("PrepareRuntime: %v", err)
	}
	return plan, runtimePlan
}

func newAppRuntimeEngine(t *testing.T, client llm.Client, cfg runtime.Config, handlers ...tools.Handler) (*session.Store, *runtime.Engine) {
	t.Helper()
	store := createAppRuntimeSession(t)
	return store, newAppRuntimeEngineWithStore(t, store, client, cfg, handlers...)
}

func createAppRuntimeSession(t *testing.T) *session.Store {
	t.Helper()
	dir := t.TempDir()
	return createAppRuntimeSessionAt(t, dir, "ws", dir)
}

func createAppRuntimeSessionAt(t *testing.T, root string, workspaceContainerName string, workspaceRoot string) *session.Store {
	t.Helper()
	store, err := session.Create(root, workspaceContainerName, workspaceRoot)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	return store
}

func newAppRuntimeEngineWithStore(t *testing.T, store *session.Store, client llm.Client, cfg runtime.Config, handlers ...tools.Handler) *runtime.Engine {
	t.Helper()
	if cfg.Model == "" {
		cfg.Model = "gpt-5"
	}
	eng, err := runtime.New(store, client, tools.NewRegistry(handlers...), cfg)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	return eng
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
