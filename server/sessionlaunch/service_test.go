package sessionlaunch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"core/server/auth"
	"core/server/launch"
	"core/server/registry"
	"core/server/session"
	"core/shared/config"
	"core/shared/serverapi"
	"core/shared/toolspec"
)

type failingAuthStateReader struct{}

func (failingAuthStateReader) CurrentState(context.Context) (auth.State, error) {
	return auth.State{}, errors.New("auth unavailable")
}

func TestServicePlanSessionRegistersStoreAndReturnsPlan(t *testing.T) {
	persistenceRoot := t.TempDir()
	containerDir := t.TempDir()
	stores := registry.NewSessionStoreRegistry()
	service := NewService(launch.Planner{
		Config: config.App{
			WorkspaceRoot:   "/tmp/workspace-a",
			PersistenceRoot: persistenceRoot,
			Settings:        config.Settings{Model: "gpt-5", OpenAIBaseURL: "http://config.local/v1"},
		},
		ContainerDir: containerDir,
	}, stores)

	resp, err := service.PlanSession(context.Background(), serverapi.SessionPlanRequest{
		ClientRequestID: "req-1",
		Mode:            serverapi.SessionLaunchModeInteractive,
		ForceNewSession: true,
		ParentSessionID: "parent-1",
	})
	if err != nil {
		t.Fatalf("PlanSession: %v", err)
	}
	if resp.Plan.SessionID == "" {
		t.Fatal("expected session id")
	}
	if resp.Plan.WorkspaceRoot != "/tmp/workspace-a" {
		t.Fatalf("workspace root = %q, want /tmp/workspace-a", resp.Plan.WorkspaceRoot)
	}
	if resp.Plan.ActiveSettings.OpenAIBaseURL != "http://config.local/v1" {
		t.Fatalf("active OpenAI base URL = %q, want http://config.local/v1", resp.Plan.ActiveSettings.OpenAIBaseURL)
	}
	store, err := stores.ResolveStore(context.Background(), resp.Plan.SessionID)
	if err != nil {
		t.Fatalf("ResolveStore: %v", err)
	}
	if store == nil {
		t.Fatal("expected planned session in registry")
	}
	if store.Meta().ParentSessionID != "parent-1" {
		t.Fatalf("parent session id = %q, want parent-1", store.Meta().ParentSessionID)
	}
}

func TestServicePlanSessionDedupesForceNewSessionRequestID(t *testing.T) {
	persistenceRoot := t.TempDir()
	containerDir := t.TempDir()
	stores := registry.NewSessionStoreRegistry()
	service := NewService(launch.Planner{
		Config: config.App{
			WorkspaceRoot:   "/tmp/workspace-a",
			PersistenceRoot: persistenceRoot,
			Settings:        config.Settings{Model: "gpt-5"},
		},
		ContainerDir: containerDir,
	}, stores)
	req := serverapi.SessionPlanRequest{
		ClientRequestID: "req-1",
		Mode:            serverapi.SessionLaunchModeInteractive,
		ForceNewSession: true,
	}
	first, err := service.PlanSession(context.Background(), req)
	if err != nil {
		t.Fatalf("PlanSession first: %v", err)
	}
	second, err := service.PlanSession(context.Background(), req)
	if err != nil {
		t.Fatalf("PlanSession second: %v", err)
	}
	if first.Plan.SessionID != second.Plan.SessionID {
		t.Fatalf("session ids = %q and %q, want stable replay", first.Plan.SessionID, second.Plan.SessionID)
	}
}

func TestServicePlanSessionAppliesLaunchOverrides(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	persistenceRoot := t.TempDir()
	containerDir := t.TempDir()
	service := NewService(launch.Planner{
		Config: config.App{
			WorkspaceRoot:   workspace,
			PersistenceRoot: persistenceRoot,
			Settings: config.Settings{
				Model:        "gpt-5.4",
				EnabledTools: map[toolspec.ID]bool{toolspec.ToolExecCommand: true},
			},
			Source: config.SourceReport{Sources: map[string]string{
				"model":       "file",
				"tools.shell": "file",
				"tools.patch": "default",
			}},
		},
		ContainerDir: containerDir,
	}, registry.NewSessionStoreRegistry())

	resp, err := service.PlanSession(context.Background(), serverapi.SessionPlanRequest{
		ClientRequestID: "req-1",
		Mode:            serverapi.SessionLaunchModeInteractive,
		ForceNewSession: true,
		Overrides: serverapi.RunPromptOverrides{
			Model:         "gpt-5.3-codex",
			ThinkingLevel: "high",
			Tools:         "patch",
		},
	})
	if err != nil {
		t.Fatalf("PlanSession: %v", err)
	}
	if resp.Plan.ActiveSettings.Model != "gpt-5.3-codex" || resp.Plan.ConfiguredModelName != "gpt-5.3-codex" {
		t.Fatalf("model = %q configured=%q, want override", resp.Plan.ActiveSettings.Model, resp.Plan.ConfiguredModelName)
	}
	if resp.Plan.ActiveSettings.ThinkingLevel != "high" {
		t.Fatalf("thinking level = %q, want high", resp.Plan.ActiveSettings.ThinkingLevel)
	}
	if strings.Join(resp.Plan.EnabledToolIDs, ",") != "patch" {
		t.Fatalf("enabled tools = %+v, want patch", resp.Plan.EnabledToolIDs)
	}
	if resp.Plan.Source.Sources["model"] != "cli" || resp.Plan.Source.Sources["tools.patch"] != "cli" {
		t.Fatalf("source = %+v, want cli override sources", resp.Plan.Source.Sources)
	}
}

func TestServicePlanSessionRespectsLockedContractWhenApplyingOverrides(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	persistenceRoot := t.TempDir()
	containerDir := t.TempDir()
	store, err := session.Create(containerDir, "workspace-a", workspace)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.MarkModelDispatchLocked(session.LockedContract{Model: "locked-model", EnabledTools: []string{"shell"}}); err != nil {
		t.Fatalf("MarkModelDispatchLocked: %v", err)
	}
	service := NewService(launch.Planner{
		Config: config.App{
			WorkspaceRoot:   workspace,
			PersistenceRoot: persistenceRoot,
			Settings: config.Settings{
				Model:        "base-model",
				EnabledTools: map[toolspec.ID]bool{toolspec.ToolExecCommand: true},
			},
		},
		ContainerDir: containerDir,
	}, registry.NewSessionStoreRegistry())

	resp, err := service.PlanSession(context.Background(), serverapi.SessionPlanRequest{
		ClientRequestID:   "req-1",
		Mode:              serverapi.SessionLaunchModeInteractive,
		SelectedSessionID: store.Meta().SessionID,
		Overrides: serverapi.RunPromptOverrides{
			Model: "cli-model",
			Tools: "patch",
		},
	})
	if err != nil {
		t.Fatalf("PlanSession: %v", err)
	}
	if resp.Plan.ActiveSettings.Model != "locked-model" || resp.Plan.ConfiguredModelName != "base-model" {
		t.Fatalf("model = %q configured=%q, want locked model and base configured", resp.Plan.ActiveSettings.Model, resp.Plan.ConfiguredModelName)
	}
	if strings.Join(resp.Plan.EnabledToolIDs, ",") != "exec_command" {
		t.Fatalf("enabled tools = %+v, want locked shell runtime id", resp.Plan.EnabledToolIDs)
	}
}

func TestServicePlanSessionPropagatesOverrideToolConflict(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	service := NewService(launch.Planner{
		Config: config.App{
			WorkspaceRoot:   workspace,
			PersistenceRoot: t.TempDir(),
			Settings: config.Settings{
				Model:        "claude-sonnet-4.5",
				EnabledTools: map[toolspec.ID]bool{toolspec.ToolExecCommand: true},
			},
			Source: config.SourceReport{Sources: map[string]string{"tools.patch": "default", "tools.edit": "default"}},
		},
		ContainerDir: t.TempDir(),
	}, registry.NewSessionStoreRegistry())

	_, err := service.PlanSession(context.Background(), serverapi.SessionPlanRequest{
		ClientRequestID: "req-1",
		Mode:            serverapi.SessionLaunchModeInteractive,
		ForceNewSession: true,
		Overrides:       serverapi.RunPromptOverrides{Tools: "patch,edit"},
	})
	if err == nil || !strings.Contains(err.Error(), "tools.patch and tools.edit cannot both be enabled") {
		t.Fatalf("error = %v, want tool conflict", err)
	}
}

func TestServicePlanSessionDefaultRoleClearDoesNotRequireAuthState(t *testing.T) {
	workspace := t.TempDir()
	containerDir := t.TempDir()
	service := NewService(launch.Planner{
		Config: config.App{
			WorkspaceRoot:   workspace,
			PersistenceRoot: t.TempDir(),
			Settings:        config.Settings{Model: "gpt-5.5"},
		},
		ContainerDir: containerDir,
	}, registry.NewSessionStoreRegistry()).WithAuthStateReader(failingAuthStateReader{})

	if _, err := service.PlanSession(context.Background(), serverapi.SessionPlanRequest{
		ClientRequestID: "req-1",
		Mode:            serverapi.SessionLaunchModeInteractive,
		ForceNewSession: true,
		Overrides:       serverapi.RunPromptOverrides{AgentRoleSet: true},
	}); err != nil {
		t.Fatalf("PlanSession with default role clear should not read auth state: %v", err)
	}
}
