package sessionlaunch

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/launch"
	"core/server/metadata"
	"core/server/registry"
	"core/server/runtimewire"
	"core/server/session"
	"core/server/sessionruntime"
	"core/shared/config"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
)

func agentReplacementFixture(t *testing.T) (config.App, *Service, *metadata.Store, runtimeids.SessionID) {
	t.Helper()
	cfg := loadSessionLaunchTestConfig(t, t.TempDir(), t.TempDir())
	selection := config.ConnectionSelection{"a", "b"}
	cfg.Settings.Connection = &selection
	cfg.Settings.ConnectionOrder = []config.ConnectionID{"a", "b"}
	definition := cfg.Settings.Connections["test"]
	cfg.Settings.Connections = map[config.ConnectionID]config.ProviderConnection{"a": definition, "b": definition}
	cfg.Settings.Subagents["worker"] = config.SubagentRole{
		Settings: config.Settings{Model: "gpt-6-luna", ThinkingLevel: "low"},
		Sources: map[string]config.Origin{
			"model":          {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}},
			"thinking_level": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "thinking_level"}},
		},
	}
	cfg.Settings = testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, cfg.Settings)
	db := testsetup.OpenStore(t, cfg.PersistenceRoot)
	binding, err := db.RegisterWorkspaceBinding(t.Context(), cfg.WorkspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	rotation := new(launch.ConnectionRotation)
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{
		PersistenceRoot: cfg.PersistenceRoot, StoreOptions: db.AuthoritativeSessionStoreOptions(), ConnectionRotation: rotation,
	})
	t.Cleanup(func() { _ = authority.Close(context.Background()) })
	changes := NewSettingsBroadcaster()
	t.Cleanup(func() { _ = changes.Close() })
	service := NewService(launch.Planner{
		Config: cfg, Rotation: rotation, ContainerDir: filepath.Join(cfg.PersistenceRoot, "projects", binding.ProjectID, "sessions"),
		StoreOptions: db.AuthoritativeSessionStoreOptions(), PersistedSessions: db, SessionProjects: db, ManagedWorktreeRoots: db,
	}, ChatSettingsOwner{Authority: authority, Registry: registry.NewRuntimeRegistry(db), Changes: changes}, db)
	initial, err := service.PlanLaunchSession(t.Context(), PlanRequest{
		Mode: launch.ModeInteractive, Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
	})
	if err != nil {
		t.Fatal(err)
	}
	id := initial.Plan.Descriptor.SessionID()
	return cfg, service, db, id
}

func replaceAgentDuringStop(t *testing.T, cfg config.App, service *Service, id runtimeids.SessionID, change func(*config.App)) (*chatsettingspb.MutationResult, error) {
	t.Helper()
	updated := cfg
	updated.Settings.Connections = maps.Clone(cfg.Settings.Connections)
	updated.Settings.Subagents = maps.Clone(cfg.Settings.Subagents)
	for name, role := range updated.Settings.Subagents {
		role.Sources = maps.Clone(role.Sources)
		updated.Settings.Subagents[name] = role
	}
	change(&updated)
	var current atomic.Pointer[config.App]
	current.Store(&cfg)
	service.planner.ReloadConfig = func() (config.App, error) { return *current.Load(), nil }
	filesystem, err := runtimewire.NewFilesystemContext(cfg.WorkspaceRoot, cfg.WorkspaceRoot, "test")
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.Settings
	settings.Connection = config.SingleConnection("a")
	plan, err := sessionruntime.NewAgentRuntimePlan(sessionruntime.AgentRuntimePlanOptions{
		MainWorkspaceRoot: cfg.WorkspaceRoot, Settings: settings, Sources: cfg.Source.Sources,
		FilesystemContext: filesystem, Client: sessionLaunchRuntimeClient{},
		QuestionsEnabled: textutil.Value(true), AutoCompactionEnabled: textutil.Value(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := session.NewOpenSessionDescriptor(id)
	if err != nil {
		t.Fatal(err)
	}
	started, stopped := make(chan struct{}), make(chan struct{})
	release := make(chan struct{}, 1)
	defer close(release)
	handle, err := service.settingsOwner.Authority.StartAgentExecution(t.Context(), sessionruntime.AgentExecutionRequest{
		Descriptor: descriptor, Runtime: &plan, Resource: sessionruntime.OpenAgentResource{},
		Runner: func(ctx context.Context, _ sessionruntime.ExecutionScope, _ sessionruntime.AgentRuntimeBridge) error {
			close(started)
			<-ctx.Done()
			close(stopped)
			<-release
			return context.Cause(ctx)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("execution did not start")
	}
	type outcome struct {
		result *chatsettingspb.MutationResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := service.MutateChatSettings(t.Context(), id, &chatsettingspb.MutationOperation{
			Operation: &chatsettingspb.MutationOperation_AgentRole{AgentRole: "worker"},
		})
		done <- outcome{result, err}
	}()
	select {
	case <-stopped:
	case outcome := <-done:
		t.Fatalf("replacement finished before Stop: %v, %v", outcome.result, outcome.err)
	case <-time.After(3 * time.Second):
		t.Fatal("replacement did not stop execution")
	}
	current.Store(&updated)
	release <- struct{}{}
	select {
	case outcome := <-done:
		if _, err := handle.Wait(t.Context()); !errors.Is(err, context.Canceled) {
			t.Fatalf("stopped execution: %v", err)
		}
		return outcome.result, outcome.err
	case <-time.After(3 * time.Second):
		t.Fatal("replacement did not finish after shutdown")
		return nil, nil
	}
}

func TestAgentReplacementRevalidatesBaselineAfterStop(t *testing.T) {
	cfg, service, db, id := agentReplacementFixture(t)
	result, err := replaceAgentDuringStop(t, cfg, service, id, func(cfg *config.App) {
		role := cfg.Settings.Subagents["worker"]
		role.Settings.Model = "gpt-6-sol"
		role.Settings.ThinkingLevel = "high"
		cfg.Settings.Subagents["worker"] = role
		connection := cfg.Settings.Connections["b"]
		connection.Capabilities = config.ProviderCapabilitiesOverride{ProviderID: "openai-compatible", SupportsResponsesAPI: true}
		cfg.Settings.Connections["b"] = connection
	})
	if err != nil || result.GetApplied() == nil || !result.GetApplied().Changed {
		t.Fatalf("Agent replacement = %v, %v", result, err)
	}
	record, err := db.ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.ConnectionID == nil || *record.Meta.ConnectionID != "b" {
		t.Fatalf("replacement did not retain allocated Connection: %+v", record.Meta.ConnectionID)
	}
	settings := record.Meta.ChatSettings
	if settings == nil || settings.Thinking == nil || *settings.Thinking != "high" || settings.Fast == nil || *settings.Fast {
		t.Fatalf("replacement persisted obsolete baseline: %+v", settings)
	}
	read, err := service.SessionChatSettings(t.Context(), id)
	if err != nil || read.GetSession().Settings.SelectedAgent.Model != "gpt-6-sol" {
		t.Fatalf("replacement model = %v, %v", read, err)
	}
	next, err := service.planner.Rotation.Prepare(cfg.Settings, true)
	if err != nil || next != "a" {
		t.Fatalf("revalidation consumed a second rotation slot: %v, %v", next, err)
	}
}

func TestAgentReplacementRejectsInvalidRetainedConnectionBeforePersistence(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*config.App)
	}{
		{name: "deleted-definition", change: func(cfg *config.App) {
			delete(cfg.Settings.Connections, "b")
			cfg.Settings.Connection = config.SingleConnection("a")
			cfg.Settings.ConnectionOrder = []config.ConnectionID{"a"}
		}},
		{name: "removed-member", change: func(cfg *config.App) {
			cfg.Settings.Connection = config.SingleConnection("a")
		}},
		{name: "changed-role-membership", change: func(cfg *config.App) {
			role := cfg.Settings.Subagents["worker"]
			role.Settings.Connection = config.SingleConnection("a")
			role.Sources["connection"] = config.Origin{Kind: config.SourceInput, Property: config.PropertyAddress{Key: "connection"}}
			cfg.Settings.Subagents["worker"] = role
		}},
		{name: "invalid-current-definition", change: func(cfg *config.App) {
			definition := cfg.Settings.Connections["b"]
			definition.Endpoint = textutil.Value("%")
			cfg.Settings.Connections["b"] = definition
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, service, db, id := agentReplacementFixture(t)
			before, err := db.ResolvePersistedSession(t.Context(), id.String())
			if err != nil {
				t.Fatal(err)
			}
			beforeSettings, err := session.ChatSettingsStateFromMeta(*before.Meta)
			if err != nil {
				t.Fatal(err)
			}
			_, err = replaceAgentDuringStop(t, cfg, service, id, test.change)
			if err == nil {
				t.Fatal("replacement accepted an invalid retained Connection")
			}
			after, err := db.ResolvePersistedSession(t.Context(), id.String())
			if err != nil {
				t.Fatal(err)
			}
			afterSettings, err := session.ChatSettingsStateFromMeta(*after.Meta)
			if err != nil || !beforeSettings.Equal(afterSettings) || !reflect.DeepEqual(before.Meta.Locked, after.Meta.Locked) {
				t.Fatalf("failed revalidation persisted replacement: %+v, %v", afterSettings, err)
			}
			next, err := service.planner.Rotation.Prepare(service.planner.Config.Settings, true)
			if err != nil || next != "a" {
				t.Fatalf("failed revalidation consumed a second slot: %v, %v", next, err)
			}
		})
	}
}
