package sessionlaunch

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/launch"
	"core/server/registry"
	"core/server/session"
	"core/server/sessionruntime"
	"core/shared/config"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func TestChatAgentMutationAllocatesActualMemberAndPreservesUnchangedBinding(t *testing.T) {
	cfg := loadSessionLaunchTestConfig(t, t.TempDir(), t.TempDir())
	selection := config.ConnectionSelection{"b", "a"}
	cfg.Settings.Connection = &selection
	cfg.Settings.ConnectionOrder = []config.ConnectionID{"a", "b"}
	definition := cfg.Settings.Connections["test"]
	other := definition
	other.Capabilities = config.ProviderCapabilitiesOverride{ProviderID: "openai-compatible", SupportsResponsesAPI: true}
	cfg.Settings.Connections = map[config.ConnectionID]config.ProviderConnection{"a": definition, "b": other}
	cfg.Settings.Subagents["worker"] = config.SubagentRole{
		Settings: config.Settings{Model: "gpt-6-luna"},
		Sources:  map[string]config.Origin{"model": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}}},
	}
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
	service := NewService(launch.Planner{
		Config: cfg, Rotation: rotation, ContainerDir: filepath.Join(cfg.PersistenceRoot, "projects", binding.ProjectID, "sessions"),
		StoreOptions: db.AuthoritativeSessionStoreOptions(), PersistedSessions: db, SessionProjects: db, ManagedWorktreeRoots: db,
	}, ChatSettingsOwner{Authority: authority, Registry: registry.NewRuntimeRegistry()})
	initial, err := service.PlanLaunchSession(t.Context(), PlanRequest{
		Mode: launch.ModeInteractive, Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
	})
	if err != nil {
		t.Fatal(err)
	}
	id := initial.Plan.Descriptor.SessionID()
	for range 2 {
		if _, err := service.NewChatSettings(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := service.SessionChatSettings(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	for _, operation := range []*chatsettingspb.MutationOperation{
		{Operation: &chatsettingspb.MutationOperation_AgentRole{AgentRole: config.DefaultSubagentRole}},
		{Operation: &chatsettingspb.MutationOperation_QuestionsEnabled{QuestionsEnabled: false}},
	} {
		if _, err := service.MutateChatSettings(t.Context(), id, operation); err != nil {
			t.Fatal(err)
		}
	}
	changed, err := service.MutateChatSettings(t.Context(), id, &chatsettingspb.MutationOperation{
		Operation: &chatsettingspb.MutationOperation_AgentRole{AgentRole: "worker"},
	})
	if err != nil || changed.GetApplied() == nil {
		t.Fatalf("Agent change = %v, %v", changed, err)
	}
	record, err := db.ResolvePersistedSession(t.Context(), id.String())
	if err != nil || record.Meta.ConnectionID == nil || *record.Meta.ConnectionID != "b" {
		t.Fatalf("actual selected member = %+v, %v", record.Meta, err)
	}
	if record.Meta.ChatSettings == nil || record.Meta.ChatSettings.Fast == nil || *record.Meta.ChatSettings.Fast {
		t.Fatalf("selected member baseline did not disable unsupported Fast: %+v", record.Meta.ChatSettings)
	}
	next, err := rotation.Prepare(cfg.Settings, true)
	if err != nil || next != "a" {
		t.Fatalf("reads/unchanged edits consumed a slot: %s, %v", next, err)
	}
	before := record.Meta
	if _, err := service.MutateChatSettings(t.Context(), id, &chatsettingspb.MutationOperation{
		Operation: &chatsettingspb.MutationOperation_AgentRole{AgentRole: "worker"},
	}); err != nil {
		t.Fatal(err)
	}
	record, err = db.ResolvePersistedSession(t.Context(), id.String())
	if err != nil || !reflect.DeepEqual(before, record.Meta) {
		t.Fatalf("unchanged Agent changed Session: %v", err)
	}
	// Return to the default on a, then fail preparation of the next member b.
	if _, err := rotation.Prepare(cfg.Settings, true); err != nil {
		t.Fatal(err)
	}
	if _, err := service.MutateChatSettings(t.Context(), id, &chatsettingspb.MutationOperation{
		Operation: &chatsettingspb.MutationOperation_AgentRole{AgentRole: config.DefaultSubagentRole},
	}); err != nil {
		t.Fatal(err)
	}
	record, err = db.ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	before = record.Meta
	broken := other
	broken.Endpoint = textutil.Value("%")
	cfg.Settings.Connections["b"] = broken
	if _, err := service.MutateChatSettings(t.Context(), id, &chatsettingspb.MutationOperation{
		Operation: &chatsettingspb.MutationOperation_AgentRole{AgentRole: "worker"},
	}); err == nil {
		t.Fatal("selected-member preparation unexpectedly succeeded")
	}
	record, err = db.ResolvePersistedSession(t.Context(), id.String())
	if err != nil || !reflect.DeepEqual(before, record.Meta) {
		t.Fatalf("failed preparation changed Session: %v", err)
	}
	next, err = rotation.Prepare(cfg.Settings, true)
	if err != nil || next != "a" {
		t.Fatalf("failed selection switched or replayed a slot: %s, %v", next, err)
	}
}

func TestDistinctConnectionSetInitialSelectionAndUnavailableAgentRepair(t *testing.T) {
	cfg := loadSessionLaunchTestConfig(t, t.TempDir(), t.TempDir())
	defaultSelection := config.ConnectionSelection{"a", "b"}
	roleSelection := config.ConnectionSelection{"a", "c"}
	cfg.Settings.Connection = &defaultSelection
	cfg.Settings.ConnectionOrder = []config.ConnectionID{"a", "b", "c"}
	definition := cfg.Settings.Connections["test"]
	cfg.Settings.Connections = map[config.ConnectionID]config.ProviderConnection{
		"a": definition, "b": definition, "c": definition,
	}
	cfg.Settings.Subagents["worker"] = config.SubagentRole{
		Settings: config.Settings{Connection: &roleSelection},
		Sources:  map[string]config.Origin{"connection": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "connection"}}},
	}
	service := newSessionLaunchTestService(cfg, t.TempDir())
	service.planner.Config = cfg
	service.planner.Rotation = new(launch.ConnectionRotation)
	for _, want := range []config.ConnectionID{"a", "c"} {
		result, err := service.PlanLaunchSession(t.Context(), PlanRequest{
			Mode:   launch.ModeInteractive,
			Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
			InitialChat: &InitialChatCreation{Settings: serverapi.InitialChatSettings{
				AgentRole: "worker", Supervisor: "off", QuestionsEnabled: true, AutoCompactionEnabled: true,
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		store, err := session.Open(filepath.Join(service.planner.ContainerDir, result.Plan.Descriptor.SessionID().String()), service.planner.StoreOptions...)
		if err != nil {
			t.Fatal(err)
		}
		if store.Meta().ConnectionID == nil || *store.Meta().ConnectionID != want {
			t.Fatalf("initial role binding = %v, want %v", store.Meta().ConnectionID, want)
		}
		if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: textutil.Value("removed")}); err != nil {
			t.Fatal(err)
		}
		input, err := service.PrepareSessionChatSettingsOperation(t.Context(), store)
		if err != nil {
			t.Fatal(err)
		}
		resolved, rejected, err := resolveChatSettingsSelection(input, &chatsettingspb.MutationOperation{
			Operation: &chatsettingspb.MutationOperation_AgentRole{AgentRole: "worker"},
		})
		if err != nil || rejected != nil || resolved.Selection.State.AgentSelector() != "worker" {
			t.Fatalf("distinct role unavailable-Agent repair = %+v, %+v, %v", resolved, rejected, err)
		}
	}
	next, err := service.planner.Rotation.Prepare(cfg.Settings, true)
	if err != nil || next != "a" {
		t.Fatalf("role launch consumed default rotation: %v, %v", next, err)
	}
}

type workflowChatSettingsTaskIdentityResolver struct {
	session.PersistedSessionResolver
}

func (workflowChatSettingsTaskIdentityResolver) ChatSettingsTaskIdentityForSession(
	context.Context,
	string,
) (*serverapi.ChatSettingsTaskIdentity, error) {
	return &serverapi.ChatSettingsTaskIdentity{TaskID: "task-chat-settings", TaskShortID: "KENT-1"}, nil
}

func TestDefaultHeadlessAgentSettingsAndInteractiveIsolation(t *testing.T) {
	cfg := loadSessionLaunchTestConfig(t, t.TempDir(), t.TempDir())
	cfg.Settings.Subagents[config.DefaultSubagentRole] = config.SubagentRole{
		Settings: config.Settings{
			Model: "gpt-6-luna", ThinkingLevel: "low",
			EnabledTools: map[toolspec.ID]bool{toolspec.ToolExecCommand: false},
		},
		Sources: map[string]config.Origin{"model": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}}, "thinking_level": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "thinking_level"}}, "tools.shell": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "tools.shell"}}},
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
				wantModel = "gpt-6-luna"
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
		Settings: config.Settings{Model: "gpt-6-luna", ThinkingLevel: "low"},
		Sources:  map[string]config.Origin{"model": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}}, "thinking_level": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "thinking_level"}}, "agent_callable": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "agent_callable"}}},

		AgentCallable: false,
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
	if !ok || entry.Choice.GetModel() != "gpt-6-luna" || entry.Choice.AgentCallable {
		t.Fatalf("headless default choice=%+v, exists=%t", entry.Choice, ok)
	}
	resolved, rejected, err := resolveChatSettingsSelection(prepared, &chatsettingspb.MutationOperation{
		Operation: &chatsettingspb.MutationOperation_Thinking{Thinking: "medium"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rejected != nil {
		t.Fatalf("Thinking selection rejected: %+v", rejected)
	}
	mutation, err := ProjectResolvedChatSettingsOperation(resolved)
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

func TestWorkflowLockedUnavailableAgentAllowsSettingsEditsAndRejectsAgentChange(t *testing.T) {
	cfg := loadSessionLaunchTestConfig(t, t.TempDir(), t.TempDir())
	workerSettings := cfg.Settings
	workerSettings.Model = "gpt-6-luna"
	workerSettings.Subagents = nil
	cfg.Settings.Subagents["worker"] = config.SubagentRole{
		Settings:      workerSettings,
		Sources:       map[string]config.Origin{"model": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}}},
		AgentCallable: true,
	}
	service := newSessionLaunchTestService(cfg, t.TempDir())
	service.planner.PersistedSessions = workflowChatSettingsTaskIdentityResolver{
		PersistedSessionResolver: service.planner.PersistedSessions,
	}
	store := createLaunchTestSession(t, service.planner.ContainerDir, "workflow-chat", cfg.WorkspaceRoot)
	if err := store.SetContinuationContext(session.ContinuationContext{
		AgentRole: textutil.Value("removed"),
	}); err != nil {
		t.Fatal(err)
	}
	prepared, err := service.PrepareSessionChatSettingsOperation(t.Context(), store)
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.WorkflowLocked {
		t.Fatal("Chat settings were not workflow-locked")
	}

	operation := &chatsettingspb.MutationOperation{
		Operation: &chatsettingspb.MutationOperation_QuestionsEnabled{QuestionsEnabled: false},
	}
	resolved, rejected, err := resolveChatSettingsSelection(prepared, operation)
	if err != nil {
		t.Fatal(err)
	}
	if rejected != nil {
		t.Fatalf("Questions edit selection rejected: %+v", rejected)
	}
	mutation, err := ProjectResolvedChatSettingsOperation(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if mutation.Rejection != nil {
		t.Fatalf("Questions edit rejected: %+v", mutation.Rejection)
	}
	if mutation.State.AgentSelector() != config.DefaultSubagentRole ||
		mutation.State.Settings == nil ||
		mutation.State.Settings.Questions == nil ||
		*mutation.State.Settings.Questions {
		t.Fatalf("Questions edit did not repair to default while applying the requested value: %+v", mutation.State)
	}

	defaultAgent, ok := prepared.Catalog.Lookup(config.DefaultSubagentRole)
	if !ok {
		t.Fatal("default Agent baseline is missing")
	}
	runMutation, err := resolveRunSettings(
		PlanRequest{},
		prepared,
		session.Meta{},
		*defaultAgent.Settings,
		operation,
	)
	if err != nil {
		t.Fatal(err)
	}
	if runMutation.Rejection != nil {
		t.Fatalf("Run settings edit rejected: %+v", runMutation.Rejection)
	}

	agentChange, rejected, err := resolveChatSettingsSelection(prepared, &chatsettingspb.MutationOperation{
		Operation: &chatsettingspb.MutationOperation_AgentRole{AgentRole: "worker"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rejected != nil {
		t.Fatalf("Agent change selection rejected before lock policy: %+v", rejected)
	}
	lockedMutation, err := ProjectResolvedChatSettingsOperation(agentChange)
	if err != nil {
		t.Fatal(err)
	}
	if lockedMutation.Rejection == nil ||
		lockedMutation.Rejection.Reason != chatsettingspb.MutationRejectionReason_MUTATION_REJECTION_REASON_AGENT_LOCKED {
		t.Fatalf("Agent change rejection = %+v, want workflow Agent lock", lockedMutation.Rejection)
	}
}

func TestExplicitDefaultSelectionPersistsLaunchMode(t *testing.T) {
	cfg := loadSessionLaunchTestConfig(t, t.TempDir(), t.TempDir())
	cfg.Settings.Subagents[config.DefaultSubagentRole] = config.SubagentRole{
		Settings: config.Settings{Model: "gpt-6-luna"},
		Sources:  map[string]config.Origin{"model": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}}},
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

func TestDefaultAgentCallabilityRestrictsCreationNotContinuation(t *testing.T) {
	cfg := loadSessionLaunchTestConfig(t, t.TempDir(), t.TempDir())
	cfg.Settings.Subagents[config.DefaultSubagentRole] = config.SubagentRole{

		AgentCallable: false, Sources: map[string]config.Origin{"agent_callable": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "agent_callable"}}},
	}
	db := testsetup.OpenStore(t, cfg.PersistenceRoot)
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
		PersistedSessions: db, SessionProjects: db, ManagedWorktreeRoots: db,
	}, ChatSettingsOwner{})
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
			if intent.Kind() == serverapi.SessionLaunchIntentOpenExisting {
				if err != nil {
					t.Fatalf("continue role=%v: %v", role, err)
				}
				continue
			}
			var denial *serverapi.SubagentLaunchDeniedError
			if !errors.As(err, &denial) || denial.Kind != serverapi.SubagentLaunchDenialNotCallable {
				t.Fatalf("intent=%s role=%v: error=%v, want not-callable denial", intent.Kind(), role, err)
			}
		}
	}
}
