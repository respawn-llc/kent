package sessionlaunch

import (
	"errors"
	"path/filepath"
	"testing"

	"core/server/launch"
	"core/server/metadata"
	"core/server/session"
	"core/shared/config"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func TestDefaultHeadlessAgentSettingsAndInteractiveIsolation(t *testing.T) {
	cfg := loadSessionLaunchTestConfig(t, t.TempDir(), t.TempDir())
	cfg.Settings.Subagents[config.DefaultSubagentRole] = config.SubagentRole{
		Settings: config.Settings{
			Model: "gpt-5-mini", ThinkingLevel: "low",
			EnabledTools: map[toolspec.ID]bool{toolspec.ToolExecCommand: false},
		},
		Sources: map[string]string{"model": "file", "thinking_level": "file", "tools.shell": "file"},
	}
	service := newSessionLaunchTestService(cfg, t.TempDir())
	for _, explicit := range []bool{false, true} {
		overrides := serverapi.RunPromptOverrides{}
		if explicit {
			overrides.AgentRole = textutil.Value(config.DefaultSubagentRole)
		}
		for _, mode := range []launch.Mode{launch.ModeHeadless, launch.ModeInteractive} {
			result, err := service.PlanLaunchSession(t.Context(), PlanRequest{
				Mode: mode, Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
				Overrides: overrides,
			})
			if err != nil {
				t.Fatalf("mode=%s explicit=%t: %v", mode, explicit, err)
			}
			wantModel := cfg.Settings.Model
			if mode == launch.ModeHeadless {
				wantModel = "gpt-5-mini"
			}
			if result.Plan.ActiveSettings.Model != wantModel {
				t.Fatalf("mode=%s explicit=%t model=%s, want %s", mode, explicit, result.Plan.ActiveSettings.Model, wantModel)
			}
			if result.Plan.ActiveSettings.EnabledTools[toolspec.ToolExecCommand] != (mode == launch.ModeInteractive) {
				t.Fatalf("mode=%s: headless tool override was not isolated", mode)
			}
			if mode == launch.ModeHeadless {
				role := session.ContinuationAgentRole(session.Meta{Continuation: result.Plan.Continuation})
				if role == nil || *role != config.DefaultSubagentRole {
					t.Fatalf("headless role=%v, want persisted default", role)
				}
				resolved, err := launch.ResolveReadOnlySessionContextSettings(cfg, session.Meta{
					Continuation: result.Plan.Continuation,
				}, false)
				if err != nil {
					t.Fatal(err)
				}
				if resolved.Settings.Model != wantModel {
					t.Fatalf("reopened headless model=%s, want %s", resolved.Settings.Model, wantModel)
				}
			}
		}
	}
}

func TestDefaultHeadlessChatSettingsUseRoleBaseline(t *testing.T) {
	cfg := loadSessionLaunchTestConfig(t, t.TempDir(), t.TempDir())
	cfg.Settings.ThinkingLevel = "high"
	cfg.Settings.Subagents[config.DefaultSubagentRole] = config.SubagentRole{
		Settings:         config.Settings{Model: "gpt-5-mini", ThinkingLevel: "low"},
		Sources:          map[string]string{"model": "file", "thinking_level": "file"},
		AgentCallableSet: true,
		AgentCallable:    false,
	}
	service := newSessionLaunchTestService(cfg, t.TempDir())
	store := createLaunchTestSession(t, service.planner.ContainerDir, "default", cfg.WorkspaceRoot)
	if err := store.SetContinuationContext(session.ContinuationContext{
		AgentRole: textutil.Value(config.DefaultSubagentRole),
	}); err != nil {
		t.Fatal(err)
	}
	prepared, err := service.PrepareSessionChatSettingsOperation(t.Context(), store)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Effective.Thinking != "low" {
		t.Fatalf("headless thinking=%s, want low", prepared.Effective.Thinking)
	}
	entry, ok := prepared.Catalog.Lookup(config.DefaultSubagentRole)
	if !ok || entry.Choice.Model != "gpt-5-mini" || entry.Choice.AgentCallable {
		t.Fatalf("headless default choice=%+v, exists=%t", entry.Choice, ok)
	}
	mutation, err := ProjectPreparedChatSettingsOperation(prepared, &chatsettingspb.MutationOperation{
		Operation: &chatsettingspb.MutationOperation_Thinking{Thinking: "medium"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if mutation.Rejection != nil {
		t.Fatalf("Thinking mutation rejected: %+v", mutation.Rejection)
	}
	if _, err := store.CommitChatSettingsState(mutation.State); err != nil {
		t.Fatal(err)
	}
	if role := session.ContinuationAgentRole(store.Meta()); role == nil || *role != config.DefaultSubagentRole {
		t.Fatalf("Thinking mutation lost headless role: %v", role)
	}
}

func TestExplicitDefaultSelectionPersistsLaunchMode(t *testing.T) {
	cfg := loadSessionLaunchTestConfig(t, t.TempDir(), t.TempDir())
	cfg.Settings.Subagents[config.DefaultSubagentRole] = config.SubagentRole{
		Settings: config.Settings{Model: "gpt-5-mini"},
		Sources:  map[string]string{"model": "file"},
	}
	service := newSessionLaunchTestService(cfg, t.TempDir())
	for _, mode := range []launch.Mode{launch.ModeHeadless, launch.ModeInteractive} {
		store := createLaunchTestSession(t, service.planner.ContainerDir, "default", cfg.WorkspaceRoot)
		if mode == launch.ModeInteractive {
			if err := store.SetContinuationContext(session.ContinuationContext{
				AgentRole: textutil.Value(config.DefaultSubagentRole),
			}); err != nil {
				t.Fatal(err)
			}
		}
		result, err := service.PlanLaunchSession(t.Context(), PlanRequest{
			Mode: mode, Intent: serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, store.Meta().SessionID)),
			Overrides: serverapi.RunPromptOverrides{AgentRole: textutil.Value(config.DefaultSubagentRole)},
		})
		if err != nil {
			t.Fatal(err)
		}
		role := session.ContinuationAgentRole(session.Meta{Continuation: result.Plan.Continuation})
		if mode == launch.ModeHeadless && (role == nil || *role != config.DefaultSubagentRole) {
			t.Fatalf("headless default selection lost: %v", role)
		}
		if mode == launch.ModeInteractive && role != nil {
			t.Fatalf("interactive default retained headless role: %v", role)
		}
	}
}

func TestDefaultAgentLaunchAndContinuationEnforceCallability(t *testing.T) {
	cfg := loadSessionLaunchTestConfig(t, t.TempDir(), t.TempDir())
	cfg.Settings.Subagents[config.DefaultSubagentRole] = config.SubagentRole{
		AgentCallableSet: true,
		AgentCallable:    false,
	}
	db, err := metadata.Open(cfg.PersistenceRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	binding, err := db.RegisterWorkspaceBinding(t.Context(), cfg.WorkspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	containerDir := filepath.Join(cfg.PersistenceRoot, "projects", binding.ProjectID, "sessions")
	caller, err := session.Create(containerDir, "caller", cfg.WorkspaceRoot, sessioncontract.SessionCategoryMain, db.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	if err := caller.EnsureDurable(); err != nil {
		t.Fatal(err)
	}
	service := NewService(launch.Planner{
		Config: cfg, ContainerDir: containerDir, StoreOptions: db.AuthoritativeSessionStoreOptions(),
		PersistedSessions: db, ProjectWorkspaceBoundary: db,
	})
	removed, err := session.Create(containerDir, "removed", cfg.WorkspaceRoot, sessioncontract.SessionCategoryMain, db.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	if err := removed.SetContinuationContext(session.ContinuationContext{AgentRole: textutil.Value("removed")}); err != nil {
		t.Fatal(err)
	}
	for _, intent := range []serverapi.SessionLaunchIntent{
		serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
		serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, caller.Meta().SessionID)),
		serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, removed.Meta().SessionID)),
	} {
		for _, role := range []*string{nil, textutil.Value(config.DefaultSubagentRole)} {
			_, err := service.PlanLaunchSession(t.Context(), PlanRequest{
				Mode: launch.ModeHeadless, Intent: intent,
				CallerSessionID: textutil.Value(caller.Meta().SessionID),
				Overrides:       serverapi.RunPromptOverrides{AgentRole: role},
			})
			var denial *serverapi.SubagentLaunchDeniedError
			if !errors.As(err, &denial) || denial.Kind != serverapi.SubagentLaunchDenialNotCallable {
				t.Fatalf("intent=%s role=%v: error=%v, want not-callable denial", intent.Kind(), role, err)
			}
		}
	}
}
