package app

import (
	"context"
	"core/internal/testharness/corefixture"
	"core/internal/testharness/testsetup"
	"core/internal/testharness/toolfixture"
	"core/server/agentruntime"
	serverbootstrap "core/server/bootstrap"
	"core/server/core"
	"core/server/llm"
	"core/server/metadata"
	"core/server/sessionstore"
	serverstartup "core/server/startup"
	"core/server/tools"
	"core/shared/config"
	"core/shared/sessioncontract"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func newMemoryAppTestServer(t *testing.T, cfg config.App, store *metadata.Store, loadOptions config.LoadOptions) (*serverstartup.ServeServer, serverbootstrap.AuthSupport) {
	t.Helper()
	appCore, authSupport := corefixture.Open(t, cfg, store, core.Options{
		WorkspaceConfigLoadOptions: loadOptions,
	})
	server := &serverstartup.ServeServer{Core: appCore}
	return server, authSupport
}

func newAppTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func newConfiguredAppWorkspace(t *testing.T) (home string, workspace string) {
	t.Helper()
	home = newAppTestHome(t)
	workspace = t.TempDir()
	configureAppTestServerPort(t)
	writeAppTestSettings(t)
	return home, workspace
}

func writeAppTestSettings(t *testing.T) {
	t.Helper()
	settingsPath, err := config.ResolveSettingsFilePathInRoot("")
	if err != nil {
		t.Fatalf("resolve test settings path: %v", err)
	}
	if _, statErr := os.Stat(settingsPath); errors.Is(statErr, os.ErrNotExist) {
		if _, err := config.WriteSettingsFileForOnboardingWithOptionsAt(settingsPath, config.DefaultOnboardingSettings(), config.OnboardingWriteOptions{}); err != nil {
			t.Fatalf("write test settings: %v", err)
		}
	} else if statErr != nil {
		t.Fatalf("stat test settings: %v", statErr)
	}
	cfg, err := config.LoadGlobal(config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, cfg.Settings)
}

func newUnconfiguredAppWorkspace(t *testing.T) (home string, workspace string) {
	t.Helper()
	home = newAppTestHome(t)
	workspace = t.TempDir()
	configureAppTestServerPort(t)
	return home, workspace
}

func loadAppTestConfig(t *testing.T, workspace string, opts config.LoadOptions) config.App {
	t.Helper()
	cfg, err := config.Load(workspace, workspace, opts)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

func startStandingRunPromptServer(t *testing.T, workspace, openAIBaseURL string, metadataStore *metadata.Store) (func(), *metadata.Store) {
	t.Helper()
	writeAppTestSettings(t)
	releasePortProbe := reserveAppTestServerPort(t)
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	cfg.Settings = testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, testsetup.WithResponsesProvider(cfg.Settings, openAIBaseURL))
	if metadataStore == nil {
		metadataStore = testsetup.OpenStore(t, cfg.PersistenceRoot)
	}
	if _, err := metadataStore.RegisterWorkspaceBinding(t.Context(), workspace); err != nil {
		t.Fatal(err)
	}
	loadOptions := config.LoadOptions{Model: "gpt-6-sol"}
	cfg.Settings.Model = loadOptions.Model
	srv, _ := newMemoryAppTestServer(t, cfg, metadataStore, loadOptions)
	stop := serveAppServerAfter(t, srv, releasePortProbe)
	cleanup := func() {
		stop()
		_ = srv.Close()
	}
	needsCleanup := true
	defer func() {
		if needsCleanup {
			cleanup()
		}
	}()
	waitForConfiguredRunPromptDaemon(t, workspace)
	needsCleanup = false
	return cleanup, metadataStore
}

func serveAppServer(t *testing.T, srv *serverstartup.ServeServer) func() {
	return serveAppServerAfter(t, srv, releaseConfiguredAppTestServerPort)
}

func serveAppServerAfter(t *testing.T, srv *serverstartup.ServeServer, beforeServe func()) func() {
	t.Helper()
	serveCtx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		beforeServe()
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
	_ = reserveAppTestServerPort(t)
}

func reserveAppTestServerPort(t *testing.T) func() {
	t.Helper()
	releaseConfiguredAppTestServerPort()
	reservation := testsetup.ReserveLoopbackPort(t)
	t.Setenv("KENT_SERVER_HOST", "127.0.0.1")
	t.Setenv("KENT_SERVER_PORT", strconv.Itoa(reservation.Port))
	return reservation.Release
}

func releaseConfiguredAppTestServerPort() {
	host, hostFound := os.LookupEnv("KENT_SERVER_HOST")
	port, portFound := os.LookupEnv("KENT_SERVER_PORT")
	if hostFound && portFound {
		testsetup.ReleaseLoopbackAddress(net.JoinHostPort(host, port))
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

func newAppRuntimeEngine(t *testing.T, client llm.Client, cfg runtime.Config, handlers ...tools.HandlerRegistration) (*session.Store, *runtime.Engine) {
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
	store, _ := createAuthoritativeTestSession(t, root, workspaceContainerName, workspaceRoot)
	return store
}

func newAppRuntimeEngineWithStore(t *testing.T, store *session.Store, client llm.Client, cfg runtime.Config, handlers ...tools.HandlerRegistration) *runtime.Engine {
	t.Helper()
	if cfg.Model == "" {
		cfg.Model = "gpt-6-sol"
	}
	if cfg.GlobalConfigDir == "" {
		cfg.GlobalConfigDir = t.TempDir()
	}
	if cfg.ProjectWorkspaceContextReader == nil {
		metadataStore := testsetup.OpenStore(t, filepath.Dir(store.Dir()))
		testsetup.ImportSessionProjectWorkspaceContext(t, metadataStore, store)
		cfg.ProjectWorkspaceContextReader = metadataStore
	}
	eventLog, err := store.MaterializeEventLog()
	if err != nil {
		t.Fatalf("materialize event log: %v", err)
	}
	eng, err := runtime.New(store, eventLog, client, toolfixture.NewRegistry(t, handlers...), cfg)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	return eng
}

func mustRegisterAppBinding(t *testing.T, store *metadata.Store, workspaceRoot string) metadata.Binding {
	t.Helper()
	binding, err := store.RegisterWorkspaceBinding(t.Context(), workspaceRoot)
	if err != nil {
		t.Fatalf("RegisterBinding: %v", err)
	}
	return binding
}

func createAuthoritativeAppSession(t *testing.T, persistenceRoot string, workspaceRoot string) (*session.Store, *metadata.Store) {
	t.Helper()
	metadataStore := testsetup.OpenStore(t, persistenceRoot)
	binding := mustRegisterAppBinding(t, metadataStore, workspaceRoot)
	// Keep the metadata store alive for the lifetime of the session store so
	// persistence observer writes continue to succeed during the test.
	store, err := session.Create(
		filepath.Join(filepath.Join(config.App{PersistenceRoot: persistenceRoot}.PersistenceRoot, "projects"), binding.ProjectID, "sessions"),
		filepath.Base(filepath.Clean(workspaceRoot)),
		workspaceRoot, sessioncontract.SessionCategoryMain, metadataStore.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}
	return store, metadataStore
}

func openAuthoritativeAppSession(t *testing.T, metadataStore *metadata.Store, sessionID string) *session.Store {
	t.Helper()
	store, err := session.OpenByID(metadataStore.PersistenceRoot(), sessionID, metadataStore.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("session.OpenByID: %v", err)
	}
	return store
}

// startStandingRunPromptServer starts an in-process standing serve server for
// the workspace and waits until it is reachable for headless run-prompt attach.
// kent run is a pure client and never starts its own server, so integration
// tests that drive RunPrompt end-to-end must provide a server to attach to.
// Callers defer the returned cleanup, which stops serving and closes the server.
// Keep the metadata store alive for the lifetime of the session store so
// persistence observer writes continue to succeed during the test.
