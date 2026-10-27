package sessionlaunch

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/launch"
	"core/server/metadata"
	"core/server/registry"
	"core/server/sessionruntime"
	"core/server/sessionstore"
	"core/server/sessionstore/sessiontest"
	"core/shared/config"
	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/protocol"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/textutil"
	"core/shared/toolspec"
)

var serviceTestPersistence = sessiontest.NewPersistence()

func createLaunchTestSession(t *testing.T, containerDir, name, workspace string) *session.Store {
	t.Helper()
	store, err := session.Create(containerDir, name, workspace, sessioncontract.SessionCategoryMain, serviceTestPersistence.Options()...)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return store
}

func sessionLaunchStringPtr(value string) *string {
	return &value
}

func TestSessionLaunchSharesRotationAcrossDefaultAndRolesAndRetainsBinding(t *testing.T) {
	cfg := loadSessionLaunchTestConfig(t, t.TempDir(), t.TempDir())
	selection := config.ConnectionSelection{"b", "a"}
	cfg.Settings.Connection = &selection
	cfg.Settings.ConnectionOrder = []config.ConnectionID{"a", "b"}
	cfg.Settings.Connections = map[config.ConnectionID]config.ProviderConnection{
		"a": cfg.Settings.Connections["test"], "b": cfg.Settings.Connections["test"],
	}
	roleSelection := config.ConnectionSelection{"a", "b"}
	cfg.Settings.Subagents["worker"] = config.SubagentRole{
		Settings: config.Settings{Connection: &roleSelection, Model: "gpt-6-luna"},
		Sources: map[string]config.Origin{
			"connection": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "connection"}},
			"model":      {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}},
		},
	}
	service := newSessionLaunchTestService(cfg, t.TempDir())
	service.planner.Rotation = launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), 0)
	for index, want := range []config.ConnectionID{"a", "b", "a", "b"} {
		overrides := serverapi.RunPromptOverrides{}
		if index%2 == 1 {
			overrides.AgentRole = textutil.Value("worker")
		}
		result, err := service.PlanLaunchSession(t.Context(), PlanRequest{
			Mode: launch.ModeHeadless, Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
			Overrides: overrides,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Plan.ActiveSettings.Connection, config.SingleConnection(want)) {
			t.Fatalf("launch %d = %v; want %s", index, result.Plan.ActiveSettings.Connection, want)
		}
		id := result.Plan.Descriptor.SessionID()
		record, err := serviceTestPersistence.ResolvePersistedSession(t.Context(), id.String())
		if err != nil || record.Meta.ConnectionID == nil || *record.Meta.ConnectionID != want {
			t.Fatalf("saved selection = %+v, %v", record.Meta, err)
		}
		unknown := config.ConnectionSelection{"unknown"}
		configured := service.planner.Config.Settings.Connection
		service.planner.Config.Settings.Connection = &unknown
		reopened, err := service.PlanLaunchSession(t.Context(), PlanRequest{
			Mode: launch.ModeHeadless, Intent: serverapi.OpenExistingSessionLaunchIntent(id),
		})
		if err != nil || !reflect.DeepEqual(reopened.Plan.ActiveSettings.Connection, config.SingleConnection(want)) {
			t.Fatalf("retained selection = %v, %v", reopened.Plan.ActiveSettings.Connection, err)
		}
		service.planner.Config.Settings.Connection = configured
	}
}

func TestDeletedSessionBindingAllocatesNextMember(t *testing.T) {
	cfg := loadSessionLaunchTestConfig(t, t.TempDir(), t.TempDir())
	selection := config.ConnectionSelection{"a", "b"}
	cfg.Settings.Connection = &selection
	cfg.Settings.ConnectionOrder = []config.ConnectionID{"a", "b"}
	cfg.Settings.Connections = map[config.ConnectionID]config.ProviderConnection{
		"a": cfg.Settings.Connections["test"], "b": cfg.Settings.Connections["test"],
	}
	service := newSessionLaunchTestService(cfg, t.TempDir())
	service.planner.Rotation = launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), 0)
	fresh, err := service.PlanLaunchSession(t.Context(), PlanRequest{
		Mode: launch.ModeInteractive, Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := session.Open(filepath.Join(service.planner.ContainerDir, fresh.Plan.Descriptor.SessionID().String()), serviceTestPersistence.Options()...)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetConnectionID("removed"); err != nil {
		t.Fatal(err)
	}
	replacement, err := service.PlanLaunchSession(t.Context(), PlanRequest{
		Mode: launch.ModeInteractive, Intent: serverapi.OpenExistingSessionLaunchIntent(fresh.Plan.Descriptor.SessionID()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replacement.Plan.ActiveSettings.Connection, config.SingleConnection("b")) {
		t.Fatalf("replacement = %v; want next member b", replacement.Plan.ActiveSettings.Connection)
	}
	notice, err := launch.BindSessionConnection(store, &replacement.Plan.ActiveSettings, replacement.Plan.Source.Sources, replacement.Plan.ConnectionScope)
	if err != nil || notice == nil || notice.Previous != "removed" || notice.Current != "b" || *store.Meta().ConnectionID != "b" {
		t.Fatalf("persisted replacement = %v, %v", notice, err)
	}
}

func TestPreviousSessionChildrenAllocateFreshConnectionsAndResumeSticky(t *testing.T) {
	for _, mode := range []launch.Mode{launch.ModeInteractive, launch.ModeHeadless} {
		for _, role := range []*string{nil, textutil.Value("worker")} {
			name := string(mode) + "/default"
			if role != nil {
				name = string(mode) + "/worker"
			}
			t.Run(name, func(t *testing.T) {
				cfg := loadSessionLaunchTestConfig(t, t.TempDir(), t.TempDir())
				selection := config.ConnectionSelection{"a", "b", "c", "d"}
				cfg.Settings.Connection = &selection
				cfg.Settings.ConnectionOrder = []config.ConnectionID{"a", "b", "c", "d"}
				definition := cfg.Settings.Connections["test"]
				cfg.Settings.Connections = map[config.ConnectionID]config.ProviderConnection{
					"a": definition, "b": definition, "c": definition, "d": definition,
				}
				roleSelection := config.ConnectionSelection{"a", "b", "c"}
				cfg.Settings.Subagents["worker"] = config.SubagentRole{
					Settings: config.Settings{Connection: &roleSelection, Model: "gpt-6-luna"},
					Sources: map[string]config.Origin{
						"connection": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "connection"}},
						"model":      {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}},
					},
				}
				service := newSessionLaunchTestService(cfg, t.TempDir())
				service.planner.Rotation = launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), 0)
				overrides := serverapi.RunPromptOverrides{AgentRole: role}
				parent, err := service.PlanLaunchSession(t.Context(), PlanRequest{
					Mode: mode, Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
					Overrides: overrides,
				})
				if err != nil {
					t.Fatal(err)
				}
				parentID := parent.Plan.Descriptor.SessionID()
				parentRecord, err := serviceTestPersistence.ResolvePersistedSession(t.Context(), parentID.String())
				if err != nil {
					t.Fatal(err)
				}
				wantConnections := []config.ConnectionID{"b", "c", "d"}
				if role != nil {
					wantConnections = []config.ConnectionID{"b", "c", "a"}
				}
				for _, want := range wantConnections {
					child, err := service.PlanLaunchSession(t.Context(), PlanRequest{
						Mode: mode, Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.PreviousSessionCreateOrigin(parentID)),
						Overrides: overrides,
					})
					if err != nil {
						t.Fatal(err)
					}
					childID := child.Plan.Descriptor.SessionID()
					record, err := serviceTestPersistence.ResolvePersistedSession(t.Context(), childID.String())
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(child.Plan.ActiveSettings.Connection, config.SingleConnection(want)) ||
						record.Meta.ConnectionID == nil || *record.Meta.ConnectionID != want {
						t.Fatalf("child connection = %v, saved = %v; want %s", child.Plan.ActiveSettings.Connection, record.Meta.ConnectionID, want)
					}
					if record.Meta.PreviousSessionID == nil || *record.Meta.PreviousSessionID != parentID ||
						!reflect.DeepEqual(record.Meta.Continuation, parentRecord.Meta.Continuation) {
						t.Fatalf("child lost continuation or lineage: %+v", record.Meta)
					}
					for id, binding := range map[runtimeids.SessionID]config.ConnectionID{parentID: "a", childID: want} {
						reopened, err := service.PlanLaunchSession(t.Context(), PlanRequest{
							Mode: mode, Intent: serverapi.OpenExistingSessionLaunchIntent(id),
						})
						if err != nil || !reflect.DeepEqual(reopened.Plan.ActiveSettings.Connection, config.SingleConnection(binding)) {
							t.Fatalf("resume binding = %v, %v; want %s", reopened.Plan.ActiveSettings.Connection, err, binding)
						}
					}
				}
				after, err := serviceTestPersistence.ResolvePersistedSession(t.Context(), parentID.String())
				if err != nil || !reflect.DeepEqual(after.Meta.ConnectionID, parentRecord.Meta.ConnectionID) {
					t.Fatalf("child launch changed parent binding: %+v, %v", after.Meta, err)
				}
			})
		}
	}
}

func TestExplicitUnchangedAgentReplacesDeletedBindingOnce(t *testing.T) {
	cfg := loadSessionLaunchTestConfig(t, t.TempDir(), t.TempDir())
	selection := config.ConnectionSelection{"a", "b"}
	cfg.Settings.Connection = &selection
	cfg.Settings.ConnectionOrder = []config.ConnectionID{"a", "b"}
	cfg.Settings.Connections = map[config.ConnectionID]config.ProviderConnection{
		"a": cfg.Settings.Connections["test"], "b": cfg.Settings.Connections["test"],
	}
	cfg.Settings.Subagents["worker"] = config.SubagentRole{
		Settings: config.Settings{Model: "gpt-6-luna"},
		Sources:  map[string]config.Origin{"model": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}}},
	}
	service := newSessionLaunchTestService(cfg, t.TempDir())
	service.planner.Rotation = launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), 0)
	store := createLaunchTestSession(t, service.planner.ContainerDir, "retained", cfg.WorkspaceRoot)
	if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: textutil.Value("worker")}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetConnectionID("removed"); err != nil {
		t.Fatal(err)
	}
	fresh := func(want config.ConnectionID) {
		t.Helper()
		result, err := service.PlanLaunchSession(t.Context(), PlanRequest{
			Mode: launch.ModeHeadless, Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Plan.ActiveSettings.Connection, config.SingleConnection(want)) {
			t.Fatalf("fresh connection = %v; want %s", result.Plan.ActiveSettings.Connection, want)
		}
	}
	fresh("a")
	req := PlanRequest{
		Mode: launch.ModeHeadless, Intent: serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, store.Meta().SessionID)),
		Overrides: serverapi.RunPromptOverrides{AgentRole: textutil.Value("worker")},
	}
	result, err := service.PlanLaunchSession(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Plan.ActiveSettings.Connection, config.SingleConnection("b")) || result.Plan.ActiveSettings.Model != "gpt-6-luna" {
		t.Fatalf("replacement settings = %+v", result.Plan.ActiveSettings)
	}
	if _, err := launch.BindSessionConnection(store, &result.Plan.ActiveSettings, result.Plan.Source.Sources, result.Plan.ConnectionScope); err != nil {
		t.Fatal(err)
	}
	if *store.Meta().ConnectionID != "b" || *store.Meta().Continuation.AgentRole != "worker" {
		t.Fatalf("replacement metadata = %+v", store.Meta())
	}
	if _, err := service.PlanLaunchSession(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	fresh("a")
}

func newSessionLaunchTestService(cfg config.App, containerDir string) *Service {
	cfg.Settings = testsetup.ProviderSettings(cfg.Settings)
	return NewService(launch.Planner{
		Config:            cfg,
		ContainerDir:      containerDir,
		StoreOptions:      serviceTestPersistence.Options(),
		PersistedSessions: serviceTestPersistence,
		SessionProjects:   sessionLaunchProjectResolver{}, ManagedWorktreeRoots: sessionLaunchProjectResolver{},
	}, ChatSettingsOwner{}, nil)
}

type sessionLaunchProjectResolver struct{}

func (sessionLaunchProjectResolver) ResolveSessionProjectID(context.Context, string) (string, error) {
	return "test-project", nil
}

func (sessionLaunchProjectResolver) ListManagedWorktreeRoots(context.Context) ([]string, error) {
	return nil, nil
}

func TestServicePlanSessionProjectsTypedOptionalSessionName(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	containerDir := filepath.Join(root, "sessions")
	store := createLaunchTestSession(t, containerDir, "initial", workspace)
	service := newSessionLaunchTestService(loadSessionLaunchTestConfig(t, workspace, root), containerDir)
	intent, err := protoapi.SessionLaunchRequestIntentToProto(
		serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, store.Meta().SessionID)),
	)
	if err != nil {
		t.Fatalf("SessionLaunchIntentToProto: %v", err)
	}
	request := &sessionlaunchpb.SessionPlanRequest{
		Mode:   sessionlaunchpb.SessionLaunchMode_SESSION_LAUNCH_MODE_INTERACTIVE,
		Intent: intent,
	}

	if err := store.SetName(nil); err != nil {
		t.Fatalf("clear session name: %v", err)
	}
	absent, err := service.PlanSession(t.Context(), request)
	if err != nil {
		t.Fatalf("PlanSession absent name: %v", err)
	}
	if absent.Plan.SessionName != nil {
		t.Fatalf("absent session name = %v, want nil", absent.Plan.SessionName)
	}

	title := "Incident triage"
	if err := store.SetName(&title); err != nil {
		t.Fatalf("set session name: %v", err)
	}
	present, err := service.PlanSession(t.Context(), request)
	if err != nil {
		t.Fatalf("PlanSession present name: %v", err)
	}
	if present.Plan.SessionName == nil || *present.Plan.SessionName != title {
		t.Fatalf("present session name = %v, want %q", present.Plan.SessionName, title)
	}
}

func TestPlanLaunchSessionReturnsPlanWithoutRegisteringStore(t *testing.T) {
	persistenceRoot := t.TempDir()
	containerDir := t.TempDir()
	service := newSessionLaunchTestService(config.App{
		WorkspaceRoot:   "/tmp/workspace-a",
		PersistenceRoot: persistenceRoot,
		Settings:        testsetup.WithResponsesProvider(config.Settings{Model: "gpt-6-sol"}, "http://config.local/v1"),
	}, containerDir)

	resp, err := service.PlanLaunchSession(context.Background(), PlanRequest{
		Mode:   launch.ModeInteractive,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
	})
	if err != nil {
		t.Fatalf("PlanLaunchSession: %v", err)
	}
	if resp.Plan.Descriptor.SessionID().String() == "" {
		t.Fatal("expected session id")
	}
	definition, err := resp.Plan.ActiveSettings.SelectedConnection()
	if err != nil || definition.Endpoint == nil || *definition.Endpoint != "http://config.local/v1" {
		t.Fatalf("active connection = %+v, %v", definition, err)
	}
}

func TestPlanLaunchSessionCreatesIndependentMainSessionWithInitialChatState(t *testing.T) {
	workspace := t.TempDir()
	persistenceRoot := t.TempDir()
	containerDir := t.TempDir()
	cfg := loadSessionLaunchTestConfig(t, workspace, persistenceRoot)
	cfg.Settings.Model = "gpt-6-sol"
	cfg.Settings.Reviewer.Frequency = "edits"
	cfg.Settings.ThinkingLevel = "medium"
	cfg.Settings.EnabledTools = map[toolspec.ID]bool{toolspec.ToolAskQuestion: true}
	service := newSessionLaunchTestService(cfg, containerDir)
	draft := " exact unsent\ncomposer text "

	result, err := service.PlanLaunchSession(t.Context(), PlanRequest{
		Mode:   launch.ModeInteractive,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
		InitialChat: &InitialChatCreation{
			Settings: serverapi.InitialChatSettings{
				AgentRole:             config.DefaultSubagentRole,
				Supervisor:            "all",
				Thinking:              textutil.Value("high"),
				Fast:                  textutil.Value(false),
				QuestionsEnabled:      false,
				AutoCompactionEnabled: false,
			},
			InputDraft: &draft,
		},
	})
	if err != nil {
		t.Fatalf("PlanLaunchSession: %v", err)
	}

	reopened, err := session.Open(
		filepath.Join(containerDir, result.Plan.Descriptor.SessionID().String()),
		serviceTestPersistence.Options()...,
	)
	if err != nil {
		t.Fatalf("open created Session: %v", err)
	}
	state, err := session.ChatDraftStateFromMeta(reopened.Meta())
	if err != nil {
		t.Fatalf("ChatDraftStateFromMeta: %v", err)
	}
	want := sessiontest.CompleteChatSettingsState(
		t,
		config.DefaultSubagentRole,
		"all",
		"high",
		false,
		false,
		false,
	)
	if state.Message != draft ||
		state.Agent != want.AgentSelector() ||
		!reflect.DeepEqual(state.Settings, want.Settings) {
		t.Fatalf("created Chat state = %+v, want draft %q and settings %+v", state, draft, want)
	}
	if result.Plan.ActiveSettings.Reviewer.Frequency != "all" ||
		result.Plan.ActiveSettings.ThinkingLevel != "high" ||
		result.Plan.ActiveSettings.PriorityRequestMode ||
		result.Plan.QuestionsEnabled ||
		result.Plan.AutoCompactionEnabled {
		t.Fatalf(
			"planned Chat settings = supervisor=%q thinking=%q fast=%t questions=%t auto_compaction=%t",
			result.Plan.ActiveSettings.Reviewer.Frequency,
			result.Plan.ActiveSettings.ThinkingLevel,
			result.Plan.ActiveSettings.PriorityRequestMode,
			result.Plan.QuestionsEnabled,
			result.Plan.AutoCompactionEnabled,
		)
	}
}

func TestPlanLaunchSessionReturnsNoSessionWhenOrdinaryCreationPersistenceFails(t *testing.T) {
	workspace := t.TempDir()
	cfg := loadSessionLaunchTestConfig(t, workspace, t.TempDir())
	cfg.Settings.Model = "gpt-6-sol"
	persistence := sessiontest.NewPersistence()
	gate := sessiontest.NewPersistenceGate(persistence)
	persistenceErr := errors.New("ordinary creation persistence failed")
	gate.FailNext(persistenceErr)
	service := NewService(launch.Planner{
		Config:       cfg,
		ContainerDir: t.TempDir(),
		StoreOptions: []session.StoreOption{
			session.WithPersistenceObserver(gate),
			session.WithPersistedSessionResolver(persistence),
			session.WithSessionContextFactWriter(persistence),
		},
		PersistedSessions: persistence,
		SessionProjects:   sessionLaunchProjectResolver{}, ManagedWorktreeRoots: sessionLaunchProjectResolver{},
	}, ChatSettingsOwner{}, nil)

	result, err := service.PlanLaunchSession(t.Context(), PlanRequest{
		Mode:   launch.ModeInteractive,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
		InitialChat: &InitialChatCreation{
			Settings: serverapi.InitialChatSettings{
				AgentRole:             config.DefaultSubagentRole,
				Supervisor:            "edits",
				QuestionsEnabled:      true,
				AutoCompactionEnabled: true,
			},
		},
	})
	if !errors.Is(err, persistenceErr) {
		t.Fatalf("PlanLaunchSession error = %v, want ordinary persistence failure", err)
	}
	if !reflect.DeepEqual(result, PlanResult{}) {
		t.Fatalf("PlanLaunchSession result = %+v, want no Session result", result)
	}
}

func TestPlanLaunchSessionMakesInitialChatVisibleWithoutDraft(t *testing.T) {
	workspace := t.TempDir()
	persistenceRoot := t.TempDir()
	cfg := loadSessionLaunchTestConfig(t, workspace, persistenceRoot)
	cfg.Settings.Model = "gpt-6-sol"
	metadataStore := testsetup.OpenStore(t, persistenceRoot)
	binding, err := metadataStore.RegisterWorkspaceBinding(t.Context(), workspace)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	containerDir := filepath.Join(persistenceRoot, "projects", binding.ProjectID, "sessions")
	if err := os.MkdirAll(containerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll Session container: %v", err)
	}
	service := NewService(launch.Planner{
		Config:            cfg,
		ContainerDir:      containerDir,
		StoreOptions:      metadataStore.AuthoritativeSessionStoreOptions(),
		PersistedSessions: metadataStore,
		SessionProjects:   metadataStore, ManagedWorktreeRoots: metadataStore,
	}, ChatSettingsOwner{}, metadataStore,
	)

	result, err := service.PlanLaunchSession(t.Context(), PlanRequest{
		Mode:   launch.ModeInteractive,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
		InitialChat: &InitialChatCreation{
			Settings: serverapi.InitialChatSettings{
				AgentRole:             config.DefaultSubagentRole,
				Supervisor:            "edits",
				QuestionsEnabled:      true,
				AutoCompactionEnabled: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("PlanLaunchSession: %v", err)
	}
	page, err := metadataStore.ListSessionPage(
		t.Context(),
		binding.ProjectID,
		sessioncontract.SessionCategoryMain,
		0,
		10,
	)
	if err != nil {
		t.Fatalf("ListSessionPage: %v", err)
	}
	if len(page.Sessions) != 1 ||
		page.Sessions[0].SessionID != result.Plan.Descriptor.SessionID() {
		t.Fatalf("visible Sessions = %+v, want created initial Chat", page.Sessions)
	}
	record, err := metadataStore.ResolvePersistedSession(t.Context(), result.Plan.Descriptor.SessionID().String())
	if err != nil || record.ShortID == nil {
		t.Fatalf("created Session projection = %+v, %v", record, err)
	}
	settings, err := service.SessionChatSettings(t.Context(), result.Plan.Descriptor.SessionID())
	if err != nil {
		t.Fatal(err)
	}
	facts := settings.GetSession().Session
	if facts.SessionId != record.Meta.SessionID || facts.ShortId == nil || *facts.ShortId != record.ShortID.Int64() {
		t.Fatalf("Chat facts identity = %v, want %v", facts, record.ShortID)
	}
	for _, raw := range []string{record.ShortID.String(), "000" + record.ShortID.String(), record.Meta.SessionID} {
		selector, err := runtimeids.ParseSessionSelector(raw)
		if err != nil {
			t.Fatal(err)
		}
		resumed, err := service.PlanSession(t.Context(), &sessionlaunchpb.SessionPlanRequest{
			Mode: sessionlaunchpb.SessionLaunchMode_SESSION_LAUNCH_MODE_INTERACTIVE,
			Intent: &sessionlaunchpb.SessionLaunchRequestIntent{Intent: &sessionlaunchpb.SessionLaunchRequestIntent_OpenExistingSessionId{
				OpenExistingSessionId: protoapi.SessionSelectorToProto(selector),
			}},
		})
		if err != nil || resumed.Plan.SessionId != record.Meta.SessionID {
			t.Fatalf("resume %s = %v, %v", raw, resumed, err)
		}
	}
}

func TestPlanLaunchSessionRebasesRemovedInitialAgentToReloadedDefaultBaseline(t *testing.T) {
	workspace := t.TempDir()
	containerDir := t.TempDir()
	stale := loadSessionLaunchTestConfig(t, workspace, t.TempDir())
	workerSettings := stale.Settings
	workerSettings.Model = "gpt-6-sol"
	workerSettings.ThinkingLevel = "high"
	stale.Settings.Subagents = map[string]config.SubagentRole{
		"worker": {
			Settings:      workerSettings,
			AgentCallable: true, Sources: map[string]config.Origin{"agent_callable": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "agent_callable"}}},
		},
	}
	current := stale
	current.Settings.Subagents = nil
	current.Settings.Model = "gpt-6-luna"
	current.Settings.Reviewer.Frequency = "off"
	current.Settings.ThinkingLevel = "low"
	current.Settings.PriorityRequestMode = false
	current.Settings.EnabledTools = map[toolspec.ID]bool{toolspec.ToolAskQuestion: false}
	current.Settings.CompactionMode = config.CompactionModeNone
	persistence := sessiontest.NewPersistence()
	reloads := 0
	service := NewService(launch.Planner{
		Config:            stale,
		ContainerDir:      containerDir,
		StoreOptions:      persistence.Options(),
		PersistedSessions: persistence,
		SessionProjects:   sessionLaunchProjectResolver{}, ManagedWorktreeRoots: sessionLaunchProjectResolver{},
		ReloadConfig: func() (config.App, error) {
			reloads++
			return current, nil
		},
	}, ChatSettingsOwner{}, nil)
	fast := true
	thinking := "high"

	result, err := service.PlanLaunchSession(t.Context(), PlanRequest{
		Mode:   launch.ModeInteractive,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
		InitialChat: &InitialChatCreation{
			Settings: serverapi.InitialChatSettings{
				AgentRole:             "worker",
				Supervisor:            "all",
				Thinking:              &thinking,
				Fast:                  &fast,
				QuestionsEnabled:      true,
				AutoCompactionEnabled: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("PlanLaunchSession: %v", err)
	}
	if reloads != 1 {
		t.Fatalf("ReloadConfig calls = %d, want 1", reloads)
	}
	reopened, err := session.Open(
		filepath.Join(containerDir, result.Plan.Descriptor.SessionID().String()),
		persistence.Options()...,
	)
	if err != nil {
		t.Fatalf("open created Session: %v", err)
	}
	state, err := session.ChatSettingsStateFromMeta(reopened.Meta())
	if err != nil {
		t.Fatalf("ChatSettingsStateFromMeta: %v", err)
	}
	want := sessiontest.CompleteChatSettingsState(
		t,
		config.DefaultSubagentRole,
		"off",
		"low",
		false,
		false,
		true,
	)
	want.ConnectionID = textutil.Value(config.ConnectionID("test"))
	if !reflect.DeepEqual(state, want) {
		gotSettings := session.ChatSettings{
			Supervisor:     *state.Settings.Supervisor,
			Thinking:       *state.Settings.Thinking,
			Fast:           *state.Settings.Fast,
			Questions:      *state.Settings.Questions,
			AutoCompaction: *state.Settings.AutoCompaction,
		}
		wantSettings := session.ChatSettings{
			Supervisor:     *want.Settings.Supervisor,
			Thinking:       *want.Settings.Thinking,
			Fast:           *want.Settings.Fast,
			Questions:      *want.Settings.Questions,
			AutoCompaction: *want.Settings.AutoCompaction,
		}
		t.Fatalf(
			"rebased initial Chat settings = agent:%q settings:%+v, want agent:%q settings:%+v",
			state.AgentSelector(),
			gotSettings,
			want.AgentSelector(),
			wantSettings,
		)
	}
}

func TestPlanLaunchSessionUsesOneConfigSnapshotForNamedRole(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	snapshot := loadSessionLaunchTestConfig(t, workspace, t.TempDir())
	roleSettings := snapshot.Settings
	roleSettings.Model = "gpt-6-luna"
	snapshot.Settings.Subagents = map[string]config.SubagentRole{
		"worker": {
			Settings:      roleSettings,
			Sources:       map[string]config.Origin{"model": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}}, "agent_callable": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "agent_callable"}}},
			AgentCallable: true,
		},
	}
	reloads := 0
	service := NewService(launch.Planner{
		Config:          snapshot,
		ContainerDir:    t.TempDir(),
		StoreOptions:    serviceTestPersistence.Options(),
		SessionProjects: sessionLaunchProjectResolver{}, ManagedWorktreeRoots: sessionLaunchProjectResolver{},
		PersistedSessions: serviceTestPersistence,
		ReloadConfig: func() (config.App, error) {
			reloads++
			if reloads != 1 {
				t.Fatalf("ReloadConfig called %d times, want exactly once", reloads)
			}
			configPath := filepath.Join(home, config.ConfigDirName, "config.toml")
			if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
				t.Fatalf("MkdirAll config dir: %v", err)
			}
			if err := os.WriteFile(configPath, []byte("this is not valid toml = ["), 0o644); err != nil {
				t.Fatalf("WriteFile changed config: %v", err)
			}
			return snapshot, nil
		},
	}, ChatSettingsOwner{}, nil)
	role := "worker"

	response, err := service.PlanLaunchSession(context.Background(), PlanRequest{
		Mode:   launch.ModeHeadless,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
		Overrides: serverapi.RunPromptOverrides{
			AgentRole: &role,
			Model:     "gpt-6-sol",
		},
	})
	if err != nil {
		t.Fatalf("PlanLaunchSession: %v", err)
	}
	if reloads != 1 {
		t.Fatalf("ReloadConfig called %d times, want exactly once", reloads)
	}
	if response.Plan.ActiveSettings.Model != "gpt-6-sol" {
		t.Fatalf("model = %q, want request override from the captured snapshot", response.Plan.ActiveSettings.Model)
	}
}

func TestPlanLaunchSessionRejectsInvalidPreparedNamedTargetBeforeCreatingSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	snapshot := loadSessionLaunchTestConfig(t, workspace, t.TempDir())
	roleSettings := snapshot.Settings
	roleSettings.Model = "gpt-6-luna"
	roleSettings.ModelContextWindow = 100
	roleSettings.ContextCompactionThresholdTokens = 101
	snapshot.Settings.Subagents = map[string]config.SubagentRole{
		"invalid": {
			Settings: roleSettings,
			Sources: map[string]config.Origin{
				"model": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}},

				"model_context_window": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model_context_window"}},

				"context_compaction_threshold_tokens": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "context_compaction_threshold_tokens"}}, "agent_callable": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "agent_callable"}},
			},
			AgentCallable: true,
		},
	}
	containerDir := t.TempDir()
	service := newSessionLaunchTestService(snapshot, containerDir)
	role := "invalid"
	_, err := service.PlanLaunchSession(context.Background(), PlanRequest{
		Mode:      launch.ModeHeadless,
		Intent:    serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
		Overrides: serverapi.RunPromptOverrides{AgentRole: &role},
	})
	if err == nil {
		t.Fatal("PlanLaunchSession unexpectedly succeeded")
	}
	entries, readErr := os.ReadDir(containerDir)
	if readErr != nil {
		t.Fatalf("ReadDir session container: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid prepared target created session artifacts: %+v", entries)
	}
}

func TestPlanLaunchSessionRejectsUnknownParentBeforeRegisteringStore(t *testing.T) {
	service := newSessionLaunchTestService(config.App{
		WorkspaceRoot:   t.TempDir(),
		PersistenceRoot: t.TempDir(),
		Settings:        config.Settings{Model: "gpt-6-sol"},
	}, t.TempDir())
	unknownParent := runtimeids.NewSessionID()
	_, err := service.PlanLaunchSession(context.Background(), PlanRequest{
		Mode:   launch.ModeHeadless,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(unknownParent)),
	})
	var denied *serverapi.SubagentLaunchDeniedError
	if !errors.As(err, &denied) || denied.Kind != serverapi.SubagentLaunchDenialParentMissing {
		t.Fatalf("error = %T %v, want parent-missing denial", err, err)
	}
}

func TestPlanLaunchSessionUsesResolvedCallerWorkflowOrigin(t *testing.T) {
	ctx := context.Background()
	persistenceRoot := t.TempDir()
	workspace := t.TempDir()
	meta := testsetup.OpenStore(t, persistenceRoot)
	binding, err := meta.RegisterWorkspaceBinding(ctx, workspace)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	containerDir := filepath.Join(persistenceRoot, "projects", binding.ProjectID, "sessions")
	workflowCaller, err := session.Create(containerDir, filepath.Base(containerDir), workspace, sessioncontract.SessionCategoryMain, meta.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("session.Create workflow caller: %v", err)
	}
	if err := workflowCaller.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable workflow caller: %v", err)
	}
	testsetup.BindSessionToWorkflowTask(t, meta, binding.ProjectID, workflowCaller.Meta().SessionID)
	ordinaryCaller, err := session.Create(containerDir, filepath.Base(containerDir), workspace, sessioncontract.SessionCategoryMain, meta.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("session.Create ordinary caller: %v", err)
	}

	cfg := loadSessionLaunchTestConfig(t, workspace, persistenceRoot)
	cfg.Settings.Workflow = config.WorkflowSettings{Subagents: false}
	roleSettings := cfg.Settings
	roleSettings.ThinkingLevel = "high"
	cfg.Settings.Subagents = map[string]config.SubagentRole{
		"worker": {
			Settings:      roleSettings,
			Sources:       map[string]config.Origin{"thinking_level": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "thinking_level"}}, "agent_callable": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "agent_callable"}}},
			AgentCallable: true,
		},
	}
	service := NewService(launch.Planner{
		Config:            cfg,
		ContainerDir:      containerDir,
		StoreOptions:      meta.AuthoritativeSessionStoreOptions(),
		PersistedSessions: meta,
		CallerSessions:    meta,
		ExecutionTargets:  meta,
		SessionProjects:   meta, ManagedWorktreeRoots: meta,
	}, ChatSettingsOwner{}, meta,
	)
	workflowCallerID := workflowCaller.Meta().SessionID
	workflowCallerRuntimeID := mustSessionLaunchIntentID(t, workflowCallerID)
	ordinaryCallerID := ordinaryCaller.Meta().SessionID
	ordinaryCallerRuntimeID := mustSessionLaunchIntentID(t, ordinaryCallerID)
	for _, role := range []*string{textutil.Value("worker"), textutil.Value(config.DefaultSubagentRole), textutil.Value(config.BuiltInSubagentRoleFast), nil} {
		_, err = service.PlanLaunchSession(ctx, PlanRequest{
			Mode:            launch.ModeHeadless,
			Intent:          serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(workflowCallerRuntimeID)),
			CallerSessionID: &workflowCallerID,
			Overrides:       serverapi.RunPromptOverrides{AgentRole: role},
		})
		var denied *serverapi.SubagentLaunchDeniedError
		if !errors.As(err, &denied) || denied.Kind != serverapi.SubagentLaunchDenialNotCallable {
			t.Fatalf("workflow caller error = %T %v, want not-callable denial", err, err)
		}
		if _, err := service.PlanLaunchSession(ctx, PlanRequest{
			Mode:            launch.ModeHeadless,
			Intent:          serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(ordinaryCallerRuntimeID)),
			CallerSessionID: &ordinaryCallerID,
			Overrides:       serverapi.RunPromptOverrides{AgentRole: role},
		}); err != nil {
			t.Fatalf("ordinary caller target: %v", err)
		}
	}
	removed, err := session.Create(containerDir, "removed", workspace, sessioncontract.SessionCategoryMain, meta.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	blocked := cfg.Settings.Subagents["worker"]
	blocked.AgentCallable = false
	cfg.Settings.Subagents["worker"] = blocked
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{
		PersistenceRoot: persistenceRoot, StoreOptions: meta.AuthoritativeSessionStoreOptions(),
	})
	t.Cleanup(func() { _ = authority.Close(context.Background()) })
	settingsOwner := ChatSettingsOwner{Authority: authority, Registry: registry.NewRuntimeRegistry(nil)}
	for _, callerID := range []string{workflowCallerID, ordinaryCallerID} {
		for _, override := range []*string{nil, textutil.Value("worker")} {
			if err := removed.SetContinuationContext(session.ContinuationContext{AgentRole: textutil.Value("worker")}); err != nil {
				t.Fatal(err)
			}
			blockedService := NewService(launch.Planner{
				Config: cfg, ContainerDir: containerDir, StoreOptions: meta.AuthoritativeSessionStoreOptions(),
				PersistedSessions: meta, CallerSessions: meta, ExecutionTargets: meta, SessionProjects: meta, ManagedWorktreeRoots: meta,
			}, settingsOwner, meta)
			plan, err := blockedService.PlanLaunchSession(ctx, PlanRequest{
				Mode:            launch.ModeHeadless,
				Intent:          serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, removed.Meta().SessionID)),
				CallerSessionID: &callerID, Overrides: serverapi.RunPromptOverrides{AgentRole: override},
			})
			if err != nil {
				t.Fatalf("continue blocked role, caller=%s override=%v: %v", callerID, override, err)
			}
			if plan.Plan.Descriptor.SessionID().String() != removed.Meta().SessionID {
				t.Fatal("continuation selected a different Session")
			}
			saved, err := blockedService.SaveRunSelection(ctx, PlanRequest{
				Mode:            launch.ModeHeadless,
				Intent:          serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, removed.Meta().SessionID)),
				CallerSessionID: &callerID,
				Overrides:       serverapi.RunPromptOverrides{AgentRole: override, ThinkingLevel: "high"},
			})
			if err != nil {
				t.Fatalf("save blocked role selection: %v", err)
			}
			if _, err := blockedService.PlanLaunchSession(ctx, saved); err != nil {
				t.Fatalf("continue with saved blocked role selection: %v", err)
			}
		}
	}
	for _, workflowEnabled := range []bool{false, true} {
		if err := removed.SetContinuationContext(session.ContinuationContext{AgentRole: textutil.Value("removed")}); err != nil {
			t.Fatal(err)
		}
		cfg.Settings.Workflow.Subagents = workflowEnabled
		cfg.Settings.Subagents[config.DefaultSubagentRole] = config.SubagentRole{
			WorkflowSubagent: false, Sources: map[string]config.Origin{"workflow_subagent": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "workflow_subagent"}}},
		}
		service := NewService(launch.Planner{
			Config: cfg, ContainerDir: containerDir, StoreOptions: meta.AuthoritativeSessionStoreOptions(),
			PersistedSessions: meta, CallerSessions: meta, ExecutionTargets: meta, SessionProjects: meta, ManagedWorktreeRoots: meta,
		}, ChatSettingsOwner{}, meta)
		intent := serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, removed.Meta().SessionID))
		_, err := service.PlanLaunchSession(ctx, PlanRequest{
			Mode: launch.ModeHeadless, Intent: intent, CallerSessionID: &workflowCallerID,
		})
		if err != nil {
			t.Fatalf("workflow caller resuming removed role, workflow enabled=%t: %v", workflowEnabled, err)
		}
		if _, err := service.PlanLaunchSession(ctx, PlanRequest{
			Mode: launch.ModeHeadless, Intent: intent, CallerSessionID: &ordinaryCallerID,
		}); err != nil {
			t.Fatalf("ordinary caller resuming removed role: %v", err)
		}
	}
}

func TestPlanLaunchSessionUsesCallerRoleDepthForOrdinaryAndWorkflowCallers(t *testing.T) {
	for _, test := range []struct {
		name     string
		workflow bool
	}{
		{name: "ordinary"},
		{name: "workflow", workflow: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			workspace := t.TempDir()
			persistenceRoot := t.TempDir()
			cfg := loadSessionLaunchTestConfig(t, workspace, persistenceRoot)
			cfg.Settings.MaxSubagentDepth = 1
			cfg.Settings.Workflow.Subagents = true
			cfg.Settings.Subagents = map[string]config.SubagentRole{
				"high": sessionLaunchRoleWithDepthLimit("high", 1),
				"low":  sessionLaunchRoleWithDepthLimit("low", 0),
			}
			fixture := newSessionLaunchDepthFixture(t, cfg)
			caller := fixture.createCaller(t, "high", test.workflow)
			service, db, containerDir := fixture.service, fixture.db, fixture.containerDir
			callerID := mustSessionLaunchIntentID(t, caller.Meta().SessionID)
			callerIDValue := caller.Meta().SessionID
			low := "low"
			childResult, err := service.PlanLaunchSession(ctx, PlanRequest{
				Mode:            launch.ModeHeadless,
				Intent:          serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(callerID)),
				CallerSessionID: &callerIDValue,
				Overrides:       serverapi.RunPromptOverrides{AgentRole: &low},
			})
			if err != nil {
				t.Fatalf("launch callable zero-limit target: %v", err)
			}
			childID := childResult.Plan.Descriptor.SessionID()
			childRecord, err := db.ResolvePersistedSession(ctx, childID.String())
			if err != nil {
				t.Fatalf("resolve launched child: %v", err)
			}
			if childRecord.Meta.ParentAgentSessionID == nil || *childRecord.Meta.ParentAgentSessionID != callerID ||
				childRecord.Meta.Continuation == nil || childRecord.Meta.Continuation.AgentRole == nil ||
				*childRecord.Meta.Continuation.AgentRole != "low" {
				t.Fatalf("launched child metadata = %+v, want caller ancestry and selected low role", childRecord.Meta)
			}
			before, err := os.ReadDir(containerDir)
			if err != nil {
				t.Fatalf("read session container before zero-limit delegation: %v", err)
			}

			childIDValue := childID.String()
			high := "high"
			_, err = service.PlanLaunchSession(ctx, PlanRequest{
				Mode:            launch.ModeHeadless,
				Intent:          serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(childID)),
				CallerSessionID: &childIDValue,
				Overrides:       serverapi.RunPromptOverrides{AgentRole: &high},
			})
			var depthErr *protocol.SubagentLaunchPolicyError
			if !errors.As(err, &depthErr) {
				t.Fatalf("launch from zero-limit caller error = %v, want structured depth rejection", err)
			}
			if depthErr.Kind != protocol.SubagentLaunchPolicyMaxDepthExceeded ||
				depthErr.AttemptedDepth == nil || *depthErr.AttemptedDepth != 1 ||
				depthErr.MaxDepth == nil || *depthErr.MaxDepth != 0 {
				t.Fatalf("depth policy error = %+v, want immediate attempted depth 1 and effective limit 0", depthErr)
			}
			after, err := os.ReadDir(containerDir)
			if err != nil {
				t.Fatalf("read session container after zero-limit delegation: %v", err)
			}
			if len(after) != len(before) {
				t.Fatalf("session directories after rejected launch = %d, want %d", len(after), len(before))
			}
		})
	}
}

func TestPlanLaunchSessionDefaultTargetCannotBypassCallerDepthLimit(t *testing.T) {
	workspace := t.TempDir()
	persistenceRoot := t.TempDir()
	cfg := loadSessionLaunchTestConfig(t, workspace, persistenceRoot)
	cfg.Settings.MaxSubagentDepth = 1
	cfg.Settings.Subagents = map[string]config.SubagentRole{
		config.DefaultSubagentRole: sessionLaunchRoleWithDepthLimit(config.DefaultSubagentRole, 1),
		"low":                      sessionLaunchRoleWithDepthLimit("low", 0),
	}
	fixture := newSessionLaunchDepthFixture(t, cfg)
	caller := fixture.createCaller(t, "low", false)
	callerID := mustSessionLaunchIntentID(t, caller.Meta().SessionID)
	callerIDValue := caller.Meta().SessionID
	before, err := os.ReadDir(fixture.containerDir)
	if err != nil {
		t.Fatalf("read session container before launch: %v", err)
	}

	for _, override := range []*string{nil, textutil.Value(config.DefaultSubagentRole)} {
		_, err := fixture.service.PlanLaunchSession(t.Context(), PlanRequest{
			Mode:            launch.ModeHeadless,
			Intent:          serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(callerID)),
			CallerSessionID: &callerIDValue,
			Overrides:       serverapi.RunPromptOverrides{AgentRole: override},
		})
		var depthErr *protocol.SubagentLaunchPolicyError
		if !errors.As(err, &depthErr) {
			t.Fatalf("default target override %v error = %v, want depth rejection", override, err)
		}
		if depthErr.Kind != protocol.SubagentLaunchPolicyMaxDepthExceeded ||
			depthErr.AttemptedDepth == nil || *depthErr.AttemptedDepth != 1 ||
			depthErr.MaxDepth == nil || *depthErr.MaxDepth != 0 {
			t.Fatalf("depth policy error = %+v, want attempted depth 1 and effective limit 0", depthErr)
		}
		after, err := os.ReadDir(fixture.containerDir)
		if err != nil {
			t.Fatalf("read session container after launch: %v", err)
		}
		if len(after) != len(before) {
			t.Fatalf("session directories after rejected launch = %d, want %d", len(after), len(before))
		}
	}
}

type sessionLaunchDepthFixture struct {
	service      *Service
	db           *metadata.Store
	containerDir string
	projectID    string
	workspace    string
}

func newSessionLaunchDepthFixture(t *testing.T, cfg config.App) sessionLaunchDepthFixture {
	t.Helper()
	db := testsetup.OpenStore(t, cfg.PersistenceRoot)
	binding, err := db.RegisterWorkspaceBinding(t.Context(), cfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	containerDir := filepath.Join(cfg.PersistenceRoot, "projects", binding.ProjectID, "sessions")
	service := NewService(launch.Planner{
		Config: cfg, ContainerDir: containerDir, StoreOptions: db.AuthoritativeSessionStoreOptions(),
		PersistedSessions: db, CallerSessions: db, ExecutionTargets: db, SessionProjects: db, ManagedWorktreeRoots: db,
	}, ChatSettingsOwner{}, db)
	return sessionLaunchDepthFixture{
		service: service, db: db, containerDir: containerDir, projectID: binding.ProjectID, workspace: cfg.WorkspaceRoot,
	}
}

func (f sessionLaunchDepthFixture) createCaller(t *testing.T, role string, workflow bool) *session.Store {
	t.Helper()
	caller, err := session.Create(
		f.containerDir,
		filepath.Base(f.containerDir),
		f.workspace,
		sessioncontract.SessionCategoryMain,
		f.db.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create caller: %v", err)
	}
	if err := caller.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable caller: %v", err)
	}
	if workflow {
		testsetup.BindSessionToWorkflowTask(t, f.db, f.projectID, caller.Meta().SessionID)
	}
	if err := caller.SetContinuationContext(session.ContinuationContext{AgentRole: sessiontest.AgentRole(role)}); err != nil {
		t.Fatalf("set caller role: %v", err)
	}
	return caller
}

func sessionLaunchRoleWithDepthLimit(name string, depth int) config.SubagentRole {
	roleName := name
	origin := func(key string) config.Origin {
		return config.Origin{
			Kind: config.SourceInput,
			Property: config.PropertyAddress{
				Key: key, Role: textutil.Value(roleName),
			},
		}
	}
	return config.SubagentRole{
		Settings:         config.Settings{MaxSubagentDepth: depth},
		Sources:          map[string]config.Origin{"max_subagent_depth": origin("max_subagent_depth"), "agent_callable": origin("agent_callable"), "workflow_subagent": origin("workflow_subagent")},
		AgentCallable:    true,
		WorkflowSubagent: true,
	}
}
func TestPlanLaunchSessionPreservesLockedAgentRoleAndTools(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	persistenceRoot := t.TempDir()
	containerDir := t.TempDir()
	store := createLaunchTestSession(t, containerDir, "workspace-a", workspace)
	persistedRole := "old_role"
	if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: &persistedRole}); err != nil {
		t.Fatalf("SetContinuationContext: %v", err)
	}
	if err := store.MarkModelDispatchLocked(session.LockedContract{Model: "locked-model", EnabledTools: []string{"shell"}}); err != nil {
		t.Fatalf("MarkModelDispatchLocked: %v", err)
	}
	cfg := loadSessionLaunchTestConfig(t, workspace, persistenceRoot)
	persistedSettings := cfg.Settings
	persistedSettings.ThinkingLevel = "low"
	cfg.Settings.Subagents = map[string]config.SubagentRole{
		persistedRole: {
			Settings:      persistedSettings,
			Sources:       map[string]config.Origin{"thinking_level": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "thinking_level"}}, "agent_callable": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "agent_callable"}}},
			AgentCallable: true,
		},
	}
	service := newSessionLaunchTestService(cfg, containerDir)
	requestedRole := "removed_role"

	resp, err := service.PlanLaunchSession(context.Background(), PlanRequest{
		Mode:   launch.ModeInteractive,
		Intent: serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, store.Meta().SessionID)),
		Overrides: serverapi.RunPromptOverrides{
			AgentRole: &requestedRole,
			Tools:     "patch,edit",
		},
	})
	if err != nil {
		t.Fatalf("PlanLaunchSession: %v", err)
	}
	if !reflect.DeepEqual(resp.Plan.EnabledTools, []toolspec.ID{toolspec.ToolExecCommand}) {
		t.Fatalf("enabled tools = %+v, want the persisted locked tool set", resp.Plan.EnabledTools)
	}
	continuation := store.Meta().Continuation
	if continuation == nil ||
		continuation.AgentRole == nil ||
		*continuation.AgentRole != persistedRole {
		t.Fatalf("continuation = %+v, want preserved %q role", continuation, persistedRole)
	}
	if resp.Plan.ActiveSettings.ThinkingLevel != "low" {
		t.Fatalf("thinking level = %q, want persisted role value low", resp.Plan.ActiveSettings.ThinkingLevel)
	}
}

func TestPlanLaunchSessionPreparesOmittedSelectedRoleBeforeMaterialization(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	persistenceRoot := t.TempDir()
	containerDir := t.TempDir()
	store := createLaunchTestSession(t, containerDir, "workspace-a", workspace)
	role := "worker"
	if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: &role}); err != nil {
		t.Fatalf("SetContinuationContext: %v", err)
	}
	cfg := loadSessionLaunchTestConfig(t, workspace, persistenceRoot)
	roleSettings := cfg.Settings
	roleSettings.ThinkingLevel = "high"
	cfg.Settings.Subagents = map[string]config.SubagentRole{
		role: {
			Settings:      roleSettings,
			Sources:       map[string]config.Origin{"thinking_level": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "thinking_level"}}, "agent_callable": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "agent_callable"}}},
			AgentCallable: true,
		},
	}
	service := newSessionLaunchTestService(cfg, containerDir)

	resp, err := service.PlanLaunchSession(context.Background(), PlanRequest{
		Mode:      launch.ModeHeadless,
		Intent:    serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, store.Meta().SessionID)),
		Overrides: serverapi.RunPromptOverrides{Tools: "patch"},
	})
	if err != nil {
		t.Fatalf("PlanLaunchSession: %v", err)
	}
	if resp.Plan.ActiveSettings.ThinkingLevel != "high" {
		t.Fatalf("thinking level = %q, want persisted worker role value", resp.Plan.ActiveSettings.ThinkingLevel)
	}
	if !reflect.DeepEqual(resp.Plan.EnabledTools, []toolspec.ID{toolspec.ToolPatch}) {
		t.Fatalf("enabled tools = %+v, want prepared patch target", resp.Plan.EnabledTools)
	}
}

func TestPlanLaunchSessionRejectsOmittedTargetBeforeMaterializingSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	containerDir := t.TempDir()
	cfg := loadSessionLaunchTestConfig(t, workspace, t.TempDir())
	cfg.Settings.Model = "claude-sonnet-4.5"
	cfg.Settings.EnabledTools = map[toolspec.ID]bool{toolspec.ToolExecCommand: true}
	cfg.Source.Sources["tools.patch"] = config.Origin{Kind: config.SourceDefault, Property: config.PropertyAddress{Key: "tools.patch"}}

	cfg.Source.Sources["tools.edit"] = config.Origin{Kind: config.SourceDefault, Property: config.PropertyAddress{Key: "tools.edit"}}

	service := newSessionLaunchTestService(cfg, containerDir)

	_, err := service.PlanLaunchSession(context.Background(), PlanRequest{
		Mode:      launch.ModeHeadless,
		Intent:    serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
		Overrides: serverapi.RunPromptOverrides{Tools: "patch,edit"},
	})
	if !errors.Is(err, launch.ErrPatchEditToolsConflict) {
		t.Fatalf("PlanLaunchSession error = %v, want patch/edit conflict", err)
	}
	entries, readErr := os.ReadDir(containerDir)
	if readErr != nil {
		t.Fatalf("ReadDir session container: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("omitted target failure created session artifacts: %+v", entries)
	}
}

func loadSessionLaunchTestConfig(t *testing.T, workspace string, persistenceRoot string) config.App {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(config.PersistenceRootEnvName, t.TempDir())
	cfg, err := config.Load(workspace, workspace, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.PersistenceRoot = persistenceRoot
	cfg.Settings = testsetup.WriteProviderSettings(t, persistenceRoot, cfg.Settings)
	return cfg
}

func TestPlanLaunchSessionDefaultRoleClearDoesNotRequireAuthState(t *testing.T) {
	workspace := t.TempDir()
	containerDir := t.TempDir()
	service := newSessionLaunchTestService(config.App{
		WorkspaceRoot:   workspace,
		PersistenceRoot: t.TempDir(),
		Settings:        config.Settings{Model: "gpt-6-sol"},
	}, containerDir)

	if _, err := service.PlanLaunchSession(context.Background(), PlanRequest{
		Mode:      launch.ModeInteractive,
		Intent:    serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
		Overrides: serverapi.RunPromptOverrides{AgentRole: sessionLaunchStringPtr(config.DefaultSubagentRole)},
	}); err != nil {
		t.Fatalf("PlanLaunchSession with default role clear should not read auth state: %v", err)
	}
}

func TestPlanLaunchSessionCanProjectDefaultRoleBeforeValidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	persistenceRoot := t.TempDir()
	containerDir := t.TempDir()
	store := createLaunchTestSession(t, containerDir, "workspace-a", workspace)
	if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: sessiontest.AgentRole("worker")}); err != nil {
		t.Fatalf("SetContinuationContext: %v", err)
	}
	cfg := loadSessionLaunchTestConfig(t, workspace, persistenceRoot)
	roleSettings := cfg.Settings
	roleSettings.Model = "gpt-6-luna"
	roleSettings.ContextCompactionThresholdTokens = 300_000
	cfg.Settings.Subagents = map[string]config.SubagentRole{
		"worker": {
			Settings: roleSettings,
			Sources:  map[string]config.Origin{"model": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}}, "context_compaction_threshold_tokens": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "context_compaction_threshold_tokens"}}},
		},
	}
	service := newSessionLaunchTestService(cfg, containerDir)

	resp, err := service.PlanLaunchSession(context.Background(), PlanRequest{
		Mode:      launch.ModeInteractive,
		Intent:    serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, store.Meta().SessionID)),
		Overrides: serverapi.RunPromptOverrides{AgentRole: sessionLaunchStringPtr(config.DefaultSubagentRole)},
	})
	if err != nil {
		t.Fatalf("PlanLaunchSession: %v", err)
	}
	if resp.Plan.ActiveSettings.Model != cfg.Settings.Model {
		t.Fatalf("model = %q, want base model %q", resp.Plan.ActiveSettings.Model, cfg.Settings.Model)
	}
}

func TestPlanLaunchSessionAgentSelectionUsesCompletePreparedBaseline(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	persistenceRoot := t.TempDir()
	containerDir := t.TempDir()
	store := createLaunchTestSession(t, containerDir, "workspace-a", workspace)
	setSessionLaunchChatSettings(t, store, session.ChatSettings{
		Supervisor:     "off",
		Thinking:       "low",
		Fast:           false,
		Questions:      false,
		AutoCompaction: false,
	})
	cfg := loadSessionLaunchTestConfig(t, workspace, persistenceRoot)
	cfg.Settings = testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, testsetup.WithResponsesProvider(cfg.Settings, "https://api.openai.com/v1"))
	workerSettings := cfg.Settings
	workerSettings.Reviewer.Frequency = "all"
	workerSettings.ThinkingLevel = "  high  "
	workerSettings.PriorityRequestMode = true
	workerSettings.EnabledTools = map[toolspec.ID]bool{toolspec.ToolAskQuestion: true}
	cfg.Settings.Subagents = map[string]config.SubagentRole{
		"worker": {
			Settings: workerSettings,
			Sources: map[string]config.Origin{
				"reviewer.frequency": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "reviewer.frequency"}},

				"thinking_level": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "thinking_level"}},

				"priority_request_mode": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "priority_request_mode"}},

				"tools.ask_question": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "tools.ask_question"}},
			},
		},
	}
	service := newSessionLaunchTestService(cfg, containerDir)
	worker := "worker"
	selected, err := service.PlanLaunchSession(t.Context(), PlanRequest{
		Mode:      launch.ModeInteractive,
		Intent:    serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, store.Meta().SessionID)),
		Overrides: serverapi.RunPromptOverrides{AgentRole: &worker},
	})
	if err != nil {
		t.Fatalf("PlanLaunchSession select worker: %v", err)
	}
	if strings.TrimSpace(selected.Plan.ActiveSettings.ThinkingLevel) != "high" ||
		selected.Plan.ActiveSettings.Reviewer.Frequency != "all" ||
		!selected.Plan.ActiveSettings.PriorityRequestMode ||
		!selected.Plan.QuestionsEnabled ||
		!selected.Plan.AutoCompactionEnabled {
		t.Fatalf("selected plan = %+v, want complete worker baseline", selected.Plan)
	}
	wantSelection := sessiontest.CompleteChatSettingsState(t, "worker", "all", "high", true, true, true)
	if selected.Plan.ActivationAgentSelection == nil ||
		!reflect.DeepEqual(*selected.Plan.ActivationAgentSelection, wantSelection) {
		t.Fatalf(
			"activation Agent selection = %+v, want %+v",
			selected.Plan.ActivationAgentSelection,
			wantSelection,
		)
	}
	generated, err := sessionPlanSuccessFromResult(selected)
	if err != nil {
		t.Fatalf("encode Session plan: %v", err)
	}
	if generated.Plan.ActivationAgentSelection == nil ||
		!textutil.EqualOptional(generated.Plan.ActivationAgentSelection.AgentRole, textutil.Value("worker")) ||
		generated.Plan.ActivationAgentSelection.Baseline == nil ||
		generated.Plan.ActivationAgentSelection.Baseline.Thinking != "high" {
		t.Fatalf(
			"generated activation Agent selection = %+v, want complete worker selection",
			generated.Plan.ActivationAgentSelection,
		)
	}
}

func TestPlanLaunchSessionProjectsUnavailableAgentWithCompleteDefaultBaseline(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	persistenceRoot := t.TempDir()
	containerDir := t.TempDir()
	store := createLaunchTestSession(t, containerDir, "workspace-a", workspace)
	removed := "removed"
	target := sessiontest.CompleteChatSettingsState(t, removed, "all", "high", true, false, false)
	if _, err := store.CommitChatSettingsState(target); err != nil {
		t.Fatalf("seed removed Agent: %v", err)
	}
	if err := store.SetContinuationContext(session.ContinuationContext{
		AgentRole: &removed,
	}); err != nil {
		t.Fatalf("seed removed Agent base URL: %v", err)
	}
	cfg := loadSessionLaunchTestConfig(t, workspace, persistenceRoot)
	cfg.Settings = testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, testsetup.WithResponsesProvider(cfg.Settings, "https://api.openai.com/v1"))
	cfg.Settings.Reviewer.Frequency = "edits"
	cfg.Settings.ThinkingLevel = "medium"
	cfg.Settings.PriorityRequestMode = false
	cfg.Settings.EnabledTools = map[toolspec.ID]bool{toolspec.ToolAskQuestion: true}
	service := newSessionLaunchTestService(cfg, containerDir)

	repaired, err := service.PlanLaunchSession(t.Context(), PlanRequest{
		Mode:   launch.ModeInteractive,
		Intent: serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, store.Meta().SessionID)),
	})
	if err != nil {
		t.Fatalf("PlanLaunchSession repair removed Agent: %v", err)
	}
	if repaired.Plan.ActiveSettings.Reviewer.Frequency != "edits" ||
		repaired.Plan.ActiveSettings.ThinkingLevel != "medium" ||
		repaired.Plan.ActiveSettings.PriorityRequestMode ||
		!repaired.Plan.QuestionsEnabled ||
		!repaired.Plan.AutoCompactionEnabled {
		t.Fatalf("projected default plan = %+v, want complete current baseline", repaired.Plan)
	}
}

func TestPlanLaunchSessionAppliesPersistedChatSettingPrecedence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	containerDir := t.TempDir()
	store := createLaunchTestSession(t, containerDir, "workspace-a", workspace)
	setSessionLaunchChatSettings(t, store, session.ChatSettings{
		Supervisor:     "off",
		Thinking:       "medium",
		Fast:           false,
		Questions:      false,
		AutoCompaction: false,
	})
	cfg := loadSessionLaunchTestConfig(t, workspace, t.TempDir())
	cfg.Settings.Reviewer.Frequency = "all"
	cfg.Settings.ThinkingLevel = "high"
	cfg.Settings.PriorityRequestMode = true
	cfg.Settings.EnabledTools = map[toolspec.ID]bool{toolspec.ToolAskQuestion: true}
	service := newSessionLaunchTestService(cfg, containerDir)

	response, err := service.PlanLaunchSession(t.Context(), PlanRequest{
		Mode:   launch.ModeInteractive,
		Intent: serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, store.Meta().SessionID)),
	})
	if err != nil {
		t.Fatalf("PlanLaunchSession: %v", err)
	}
	if response.Plan.ActiveSettings.Reviewer.Frequency != "off" ||
		response.Plan.ActiveSettings.ThinkingLevel != "medium" ||
		response.Plan.ActiveSettings.PriorityRequestMode ||
		response.Plan.QuestionsEnabled ||
		response.Plan.AutoCompactionEnabled {
		t.Fatalf("effective Session Chat settings = %+v questions=%t auto_compaction=%t", response.Plan.ActiveSettings, response.Plan.QuestionsEnabled, response.Plan.AutoCompactionEnabled)
	}

	withoutOverrides := createLaunchTestSession(t, containerDir, "workspace-b", workspace)
	fallback, err := service.PlanLaunchSession(t.Context(), PlanRequest{
		Mode:   launch.ModeInteractive,
		Intent: serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, withoutOverrides.Meta().SessionID)),
	})
	if err != nil {
		t.Fatalf("PlanLaunchSession without overrides: %v", err)
	}
	if fallback.Plan.ActiveSettings.Reviewer.Frequency != "all" ||
		fallback.Plan.ActiveSettings.ThinkingLevel != "high" ||
		!fallback.Plan.ActiveSettings.PriorityRequestMode ||
		!fallback.Plan.QuestionsEnabled ||
		!fallback.Plan.AutoCompactionEnabled {
		t.Fatalf("fallback Session Chat settings = %+v questions=%t auto_compaction=%t", fallback.Plan.ActiveSettings, fallback.Plan.QuestionsEnabled, fallback.Plan.AutoCompactionEnabled)
	}
}

func setSessionLaunchChatSettings(t *testing.T, store *session.Store, settings session.ChatSettings) {
	t.Helper()
	target, err := session.ChatSettingsStateFromCompleteSettings(config.DefaultSubagentRole, settings)
	if err != nil {
		t.Fatalf("build Chat settings: %v", err)
	}
	if _, err := store.CommitChatSettingsState(target); err != nil {
		t.Fatalf("commit Chat settings: %v", err)
	}
}

func assertSessionLaunchChatSettings(t *testing.T, sessionDir string, want session.ChatSettingsState) {
	t.Helper()
	reopened, err := session.Open(sessionDir, serviceTestPersistence.Options()...)
	if err != nil {
		t.Fatalf("reopen Session: %v", err)
	}
	got, err := session.ChatSettingsStateFromMeta(reopened.Meta())
	if err != nil {
		t.Fatalf("ChatSettingsStateFromMeta: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.Marshal(got.Settings)
		wantJSON, _ := json.Marshal(want.Settings)
		t.Fatalf("Chat settings state = agent %q settings %s, want agent %q settings %s", got.AgentSelector(), gotJSON, want.AgentSelector(), wantJSON)
	}
}

func TestPlanLaunchSessionConfigOnlyOverrideDoesNotSkipInvalidPersistedRoleValidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	persistenceRoot := t.TempDir()
	containerDir := t.TempDir()
	store := createLaunchTestSession(t, containerDir, "workspace-a", workspace)
	if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: sessiontest.AgentRole("worker")}); err != nil {
		t.Fatalf("SetContinuationContext: %v", err)
	}
	cfg := loadSessionLaunchTestConfig(t, workspace, persistenceRoot)
	roleSettings := cfg.Settings
	roleSettings.Model = "gpt-6-luna"
	roleSettings.ContextCompactionThresholdTokens = 300_000
	cfg.Settings.Subagents = map[string]config.SubagentRole{
		"worker": {
			Settings: roleSettings,
			Sources:  map[string]config.Origin{"model": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}}, "context_compaction_threshold_tokens": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "context_compaction_threshold_tokens"}}},
		},
	}
	service := newSessionLaunchTestService(cfg, containerDir)

	_, err := service.PlanLaunchSession(context.Background(), PlanRequest{
		Mode:      launch.ModeInteractive,
		Intent:    serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, store.Meta().SessionID)),
		Overrides: serverapi.RunPromptOverrides{Model: "gpt-6-sol"},
	})
	if err == nil {
		t.Fatal("expected invalid persisted role validation to fail")
	}
	if errors.Is(err, serverapi.ErrInvalidRunPromptAgentRole) {
		t.Fatalf("error = %v, want persisted role validation error", err)
	}
}

func TestPlanLaunchSessionHeadlessSelectedSessionAllowsHumanContinuationOfNonCallableRole(t *testing.T) {
	workspace := t.TempDir()
	containerDir := t.TempDir()
	store := createLaunchTestSession(t, containerDir, "workspace-a", workspace)
	if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: sessiontest.AgentRole("worker")}); err != nil {
		t.Fatalf("SetContinuationContext: %v", err)
	}
	cfg := loadSessionLaunchTestConfig(t, workspace, t.TempDir())
	cfg.Settings.Subagents = map[string]config.SubagentRole{
		"worker": {

			AgentCallable: false, Sources: map[string]config.Origin{"agent_callable": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "agent_callable"}}},
		},
	}
	service := newSessionLaunchTestService(cfg, containerDir)

	result, err := service.PlanLaunchSession(context.Background(), PlanRequest{
		Mode:   launch.ModeHeadless,
		Intent: serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, store.Meta().SessionID)),
	})
	if err != nil {
		t.Fatalf("PlanLaunchSession: %v", err)
	}
	if result.Plan.Descriptor.SessionID().String() != store.Meta().SessionID {
		t.Fatalf("session id = %q, want selected %q", result.Plan.Descriptor.SessionID(), store.Meta().SessionID)
	}
}

func TestPlanLaunchSessionHeadlessSelectedSessionAllowsRemovedContinuationRole(t *testing.T) {
	workspace := t.TempDir()
	containerDir := t.TempDir()
	store := createLaunchTestSession(t, containerDir, "workspace-a", workspace)
	if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: sessiontest.AgentRole("removed")}); err != nil {
		t.Fatalf("SetContinuationContext: %v", err)
	}
	cfg := loadSessionLaunchTestConfig(t, workspace, t.TempDir())
	service := newSessionLaunchTestService(cfg, containerDir)

	result, err := service.PlanLaunchSession(context.Background(), PlanRequest{
		Mode:   launch.ModeHeadless,
		Intent: serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, store.Meta().SessionID)),
	})
	if err != nil {
		t.Fatalf("PlanLaunchSession: %v", err)
	}
	if result.Plan.ActiveSettings.Model != cfg.Settings.Model {
		t.Fatalf("model = %q, want base model %q", result.Plan.ActiveSettings.Model, cfg.Settings.Model)
	}
}

func TestPlanLaunchSessionHeadlessSelectedSessionKeepsOmittedContinuationRoleDefault(t *testing.T) {
	workspace := t.TempDir()
	containerDir := t.TempDir()
	store := createLaunchTestSession(t, containerDir, "workspace-a", workspace)
	cfg := loadSessionLaunchTestConfig(t, workspace, t.TempDir())
	service := newSessionLaunchTestService(cfg, containerDir)

	result, err := service.PlanLaunchSession(context.Background(), PlanRequest{
		Mode:   launch.ModeHeadless,
		Intent: serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, store.Meta().SessionID)),
	})
	if err != nil {
		t.Fatalf("PlanLaunchSession: %v", err)
	}
	if continuation := result.Plan.Continuation; continuation != nil && continuation.AgentRole != nil {
		t.Fatalf("continuation = %+v, want omitted default role", continuation)
	}
}

func TestPlanLaunchSessionInvalidRoleOverridePrecedesPersistedRoleValidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	persistenceRoot := t.TempDir()
	containerDir := t.TempDir()
	store := createLaunchTestSession(t, containerDir, "workspace-a", workspace)
	if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: sessiontest.AgentRole("worker")}); err != nil {
		t.Fatalf("SetContinuationContext: %v", err)
	}
	cfg := loadSessionLaunchTestConfig(t, workspace, persistenceRoot)
	roleSettings := cfg.Settings
	roleSettings.Model = "gpt-6-luna"
	roleSettings.ContextCompactionThresholdTokens = 300_000
	cfg.Settings.Subagents = map[string]config.SubagentRole{
		"worker": {
			Settings: roleSettings,
			Sources:  map[string]config.Origin{"model": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}}, "context_compaction_threshold_tokens": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "context_compaction_threshold_tokens"}}},
		},
	}
	service := newSessionLaunchTestService(cfg, containerDir)

	for _, role := range []string{"none", "self"} {
		t.Run(role, func(t *testing.T) {
			_, err := service.PlanLaunchSession(context.Background(), PlanRequest{
				Mode:      launch.ModeInteractive,
				Intent:    serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, store.Meta().SessionID)),
				Overrides: serverapi.RunPromptOverrides{AgentRole: sessionLaunchStringPtr(role)},
			})
			if err == nil {
				t.Fatal("expected invalid role override to fail")
			}
			if !errors.Is(err, serverapi.ErrInvalidRunPromptAgentRole) {
				t.Fatalf("error = %v, want malformed role error before persisted role validation", err)
			}
		})
	}
}
