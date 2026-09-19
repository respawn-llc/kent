package launch

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/metadata"
	"core/server/session"
	"core/server/session/sessiontest"
	"core/shared/config"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/toolspec"

	"github.com/BurntSushi/toml"
)

const (
	testWorkspaceContainer = "workspace-a"
	testProjectID          = "project-a"
)

var testSessionStores sync.Map

func newTestPlanner(cfg config.App, containerDir string, storeOptions ...session.StoreOption) Planner {
	cfg.Settings = testsetup.ProviderSettings(cfg.Settings)
	return Planner{
		Config:                   cfg,
		ContainerDir:             containerDir,
		StoreOptions:             storeOptions,
		ProjectWorkspaceBoundary: testProjectBoundaryResolver{root: cfg.WorkspaceRoot},
	}
}

type testProjectBoundaryResolver struct {
	root string
}

func (r testProjectBoundaryResolver) ResolveSessionProjectWorkspaceBoundary(context.Context, string) (metadata.ProjectWorkspaceBoundary, error) {
	return metadata.ProjectWorkspaceBoundary{
		ProjectID:  testProjectID,
		Workspaces: []metadata.ProjectWorkspace{{CanonicalRoot: r.root}},
	}, nil
}

func (r testProjectBoundaryResolver) ListManagedWorktreeRoots(context.Context) ([]string, error) {
	return nil, nil
}

func newPersistenceBackedTestPlanner(cfg config.App, containerDir string, persistence *sessiontest.Persistence) Planner {
	planner := newTestPlanner(cfg, containerDir, persistence.Options()...)
	planner.PersistedSessions = persistence
	return planner
}

func createTestSession(t *testing.T, workspace string) *session.Store {
	t.Helper()
	return createTestSessionInContainer(t, filepath.Join(t.TempDir(), "projects", testProjectID, "sessions"), testWorkspaceContainer, workspace)
}

func createTestSessionInContainer(t *testing.T, containerDir, workspaceContainer, workspaceRoot string, options ...session.StoreOption) *session.Store {
	t.Helper()
	if len(options) == 0 {
		options = sessiontest.NewPersistence().Options()
	}
	store, err := session.Create(containerDir, workspaceContainer, workspaceRoot, sessioncontract.SessionCategoryMain, options...)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	testSessionStores.Store(store.Meta().SessionID, store)
	return store
}

func applyRunPromptOverridesNoWarnings(t *testing.T, plan SessionPlan, overrides serverapi.RunPromptOverrides) SessionPlan {
	t.Helper()
	updated, warnings, err := ApplyRunPromptOverrides(plan, overrides)
	if err != nil {
		t.Fatalf("ApplyRunPromptOverrides: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	return updated
}

func newLoadedConfigPlan(t *testing.T, workspace string, loaded config.App) SessionPlan {
	t.Helper()
	return newSettingsPlanWithSource(t, workspace, loaded.Settings, loaded.Source)
}

func newSettingsPlan(t *testing.T, workspace string, settings config.Settings) SessionPlan {
	t.Helper()
	return newSettingsPlanWithSource(t, workspace, settings, config.SourceReport{})
}

func sessionPlanWithSnapshot(plan SessionPlan, store *session.Store, containerDir string) SessionPlan {
	return sessionPlanWithMeta(plan, store.Meta(), containerDir)
}

func newSettingsPlanWithSource(t *testing.T, workspace string, settings config.Settings, source config.SourceReport) SessionPlan {
	t.Helper()
	store := createTestSessionInContainer(t, filepath.Join(t.TempDir(), "projects", "project-a", "sessions"), "workspace-a", workspace)
	return sessionPlanWithSnapshot(SessionPlan{
		ActiveSettings:      settings,
		EnabledTools:        []toolspec.ID{toolspec.ToolExecCommand},
		ConfiguredModelName: settings.Model,
		WorkspaceRoot:       workspace,
		Source:              source,
	}, store, filepath.Dir(store.Dir()))
}

func testStoreForPlan(t *testing.T, plan SessionPlan) *session.Store {
	t.Helper()
	if value, ok := testSessionStores.Load(plan.Descriptor.SessionID().String()); ok {
		return value.(*session.Store)
	}
	t.Fatalf("no test store registered for session %q", plan.Descriptor.SessionID())
	return nil
}

func testStoreForPlannerPlan(t *testing.T, planner Planner, plan SessionPlan) *session.Store {
	t.Helper()
	store, err := session.MaterializeSessionDescriptor(planner.Config.PersistenceRoot, plan.Descriptor, planner.StoreOptions...)
	if err != nil {
		t.Fatalf("materialize session descriptor: %v", err)
	}
	return store
}

func testPlannerForPlan(plan SessionPlan) (Planner, error) {
	value, ok := testSessionStores.Load(plan.Descriptor.SessionID().String())
	if !ok {
		return Planner{}, fmt.Errorf("no test store registered for session %q", plan.Descriptor.SessionID())
	}
	store := value.(*session.Store)
	return Planner{ContainerDir: filepath.Dir(store.Dir())}, nil
}

func ApplyRunPromptOverrides(plan SessionPlan, overrides serverapi.RunPromptOverrides) (SessionPlan, []string, error) {
	planner, err := testPlannerForPlan(plan)
	if err != nil {
		return SessionPlan{}, nil, err
	}
	store := testStoreForPlanForOverride(plan)
	if store == nil {
		return SessionPlan{}, nil, fmt.Errorf("no test store registered for session %q", plan.Descriptor.SessionID())
	}
	return planner.applyRunPromptOverridesWithBudgetApplier(plan, store, overrides, RunPromptOverrideOptions{}, applyDerivedModelContextBudgetOverrides)
}

func ApplyRunPromptOverridesWithOptions(plan SessionPlan, overrides serverapi.RunPromptOverrides, options RunPromptOverrideOptions) (SessionPlan, []string, error) {
	planner, err := testPlannerForPlan(plan)
	if err != nil {
		return SessionPlan{}, nil, err
	}
	store := testStoreForPlanForOverride(plan)
	if store == nil {
		return SessionPlan{}, nil, fmt.Errorf("no test store registered for session %q", plan.Descriptor.SessionID())
	}
	return planner.applyRunPromptOverridesWithBudgetApplier(plan, store, overrides, options, applyDerivedModelContextBudgetOverrides)
}

func ApplyPreparedRunPromptOverrides(plan SessionPlan, overrides serverapi.RunPromptOverrides, prepared PreparedRunPromptOverrides) (SessionPlan, []string, error) {
	planner, err := testPlannerForPlan(plan)
	if err != nil {
		return SessionPlan{}, nil, err
	}
	store := testStoreForPlanForOverride(plan)
	if store == nil {
		return SessionPlan{}, nil, fmt.Errorf("no test store registered for session %q", plan.Descriptor.SessionID())
	}
	return planner.applyPreparedRunPromptOverridesWithBudgetApplier(
		plan,
		store.Meta(),
		store.SetContinuationContext,
		overrides,
		prepared,
		RunPromptOverrideOptions{},
		applyDerivedModelContextBudgetOverrides,
	)
}

func applyRunPromptOverridesWithBudgetApplier(plan SessionPlan, overrides serverapi.RunPromptOverrides, options RunPromptOverrideOptions, applyBudget modelContextBudgetApplier) (SessionPlan, []string, error) {
	planner, err := testPlannerForPlan(plan)
	if err != nil {
		return SessionPlan{}, nil, err
	}
	store := testStoreForPlanForOverride(plan)
	return planner.applyRunPromptOverridesWithBudgetApplier(plan, store, overrides, options, applyBudget)
}

func testStoreForPlanForOverride(plan SessionPlan) *session.Store {
	value, ok := testSessionStores.Load(plan.Descriptor.SessionID().String())
	if !ok {
		return nil
	}
	return value.(*session.Store)
}

func loadLaunchConfig(t *testing.T, workspace string, configLines ...string) config.App {
	t.Helper()
	cfg, _ := loadLaunchConfigWithHome(t, workspace, configLines...)
	return cfg
}

func loadLaunchConfigWithHome(t *testing.T, workspace string, configLines ...string) (config.App, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.PersistenceRootEnvName, filepath.Join(home, config.ConfigDirName))
	writeHomeConfig(t, home, strings.Join(configLines, "\n"))
	cfg, err := config.Load(workspace, workspace, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg, home
}

func writeHomeConfig(t *testing.T, home, contents string) {
	t.Helper()
	document := make(map[string]any)
	if _, err := toml.Decode(contents, &document); err != nil {
		t.Fatal(err)
	}
	if _, present := document["connection"]; !present {
		document["connection"] = "test"
		connections, _ := document["connections"].(map[string]any)
		if connections == nil {
			connections = make(map[string]any)
		}
		connections["test"] = map[string]any{"protocol": "responses", "endpoint": "https://api.openai.com/v1"}
		document["connections"] = connections
	}
	var encoded bytes.Buffer
	if err := toml.NewEncoder(&encoded).Encode(document); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(home, config.ConfigDirName, "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(configPath, encoded.Bytes(), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func createMetadataBackedSession(
	t *testing.T,
	metadataStore *metadata.Store,
	persistenceRoot string,
	workspaceRoot string,
	continuation session.ContinuationContext,
) *session.Store {
	t.Helper()
	binding, err := metadataStore.RegisterWorkspaceBinding(t.Context(), workspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	containerDir := filepath.Join(filepath.Join(config.App{PersistenceRoot: persistenceRoot}.PersistenceRoot, "projects"), binding.ProjectID, "sessions")
	store := createTestSessionInContainer(
		t,
		containerDir,
		filepath.Base(containerDir),
		workspaceRoot,
		metadataStore.AuthoritativeSessionStoreOptions()...,
	)
	if err := store.SetContinuationContext(continuation); err != nil {
		t.Fatalf("set continuation context: %v", err)
	}
	return store
}

func writeSessionEventArtifact(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), nil, 0o644); err != nil {
		t.Fatalf("write session events: %v", err)
	}
}

func requireSameSessionDir(t *testing.T, got, want string) {
	t.Helper()
	openedDir, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("EvalSymlinks opened dir: %v", err)
	}
	selectedDir, err := filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatalf("EvalSymlinks selected dir: %v", err)
	}
	if openedDir != selectedDir {
		t.Fatalf("opened session dir = %q, want %q", openedDir, selectedDir)
	}
}
