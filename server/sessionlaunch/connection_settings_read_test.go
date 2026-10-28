package sessionlaunch

import (
	"errors"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/launch"
	"core/server/sessionstore"
	"core/server/sessionstore/sessiontest"
	"core/shared/config"
	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/textutil"
)

func TestSessionChatSettingsReadsRemovedConnectionWithoutRebinding(t *testing.T) {
	cfg := loadSessionLaunchTestConfig(t, t.TempDir(), t.TempDir())
	persistence := sessiontest.NewPersistence()
	store, err := session.Create(t.TempDir(), "workspace", cfg.WorkspaceRoot, sessioncontract.SessionCategoryMain, persistence.Options()...)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetConnectionID(config.ConnectionID("removed")); err != nil {
		t.Fatal(err)
	}
	svc := NewService(launch.Planner{Config: cfg, PersistedSessions: persistence}, ChatSettingsOwner{}, nil)
	id := mustSessionLaunchIntentID(t, store.Meta().SessionID)
	read, err := svc.SessionChatSettings(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if read.GetSession().Settings.SelectedAgent.Role != config.DefaultSubagentRole {
		t.Fatalf("selected Agent = %+v", read.GetSession().Settings.SelectedAgent)
	}
	notice := read.GetSession().ConnectionReplacement
	if notice == nil || notice.Previous != "removed" || notice.Current != "test" {
		t.Fatalf("Connection replacement notice = %+v", notice)
	}
	if read.GetSession().Session.Connection != nil {
		t.Fatal("single configured Connection must keep its optional row hidden")
	}
	record, err := persistence.ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if !textutil.EqualOptional(record.Meta.ConnectionID, textutil.Value(config.ConnectionID("removed"))) {
		t.Fatalf("read changed Session binding: %+v", record.Meta.ConnectionID)
	}
	if err := store.SetConnectionID("test"); err != nil {
		t.Fatal(err)
	}
	read, err = svc.SessionChatSettings(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if read.GetSession().ConnectionReplacement != nil {
		t.Fatal("configured binding must not produce a replacement notice")
	}
	if err := store.SetConnectionID("removed"); err != nil {
		t.Fatal(err)
	}
	cfg.Settings.Connections = map[config.ConnectionID]config.ProviderConnection{}
	svc = NewService(launch.Planner{Config: cfg, PersistedSessions: persistence}, ChatSettingsOwner{}, nil)
	read, err = svc.SessionChatSettings(t.Context(), id)
	var unavailable *config.ConnectionReferenceError
	if read != nil || !errors.As(err, &unavailable) {
		t.Fatalf("unavailable replacement returned success: %+v, %v", read, err)
	}
}

func TestSessionChatSettingsReplacementMatchesInteractiveResumeAgentConnection(t *testing.T) {
	cfg := loadSessionLaunchTestConfig(t, t.TempDir(), t.TempDir())
	cfg.Settings.Connections["worker"] = cfg.Settings.Connections["test"]
	cfg.Settings.Connections["worker-next"] = cfg.Settings.Connections["test"]
	cfg.Settings.ConnectionOrder = []config.ConnectionID{"test", "worker", "worker-next"}
	workerConnections := config.ConnectionSelection{"worker", "worker-next"}
	cfg.Settings.Subagents["worker"] = config.SubagentRole{
		Settings: config.Settings{Connection: &workerConnections, Model: "gpt-6-luna"},
		Sources: map[string]config.Origin{
			"model":      {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}},
			"connection": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "connection"}},
		},
	}
	persistence := sessiontest.NewPersistence()
	container := t.TempDir()
	store, err := session.Create(container, "workspace", cfg.WorkspaceRoot, sessioncontract.SessionCategoryMain, persistence.Options()...)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetConnectionID("removed"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: textutil.Value("worker")}); err != nil {
		t.Fatal(err)
	}
	rotation := launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), 0)
	workerSettings := cfg.Settings
	workerSettings.Connection = &workerConnections
	if selected, err := rotation.Prepare(t.Context(), workerSettings, true); err != nil || selected != "worker" {
		t.Fatalf("initial assignment = %s, %v", selected, err)
	}
	svc := NewService(launch.Planner{
		Config: cfg, Rotation: rotation, PersistedSessions: persistence, ContainerDir: container, StoreOptions: persistence.Options(),
		SessionProjects: sessionLaunchProjectResolver{}, ManagedWorktreeRoots: sessionLaunchProjectResolver{},
	}, ChatSettingsOwner{}, nil)
	id := mustSessionLaunchIntentID(t, store.Meta().SessionID)
	read, err := svc.SessionChatSettings(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	settings := read.GetSession()
	if settings.Settings.SelectedAgent.Role != "worker" || settings.Settings.SelectedAgent.Model != "gpt-6-luna" ||
		settings.Session.Connection == nil || settings.Session.Connection.ConnectionId != "worker-next" ||
		settings.ConnectionReplacement == nil || settings.ConnectionReplacement.Current != "worker-next" {
		t.Fatalf("replacement ignored selected Agent: %+v", settings)
	}
	intent, err := protoapi.SessionLaunchRequestIntentToProto(serverapi.OpenExistingSessionLaunchIntent(id))
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := svc.PlanSession(t.Context(), &sessionlaunchpb.SessionPlanRequest{
		Mode: sessionlaunchpb.SessionLaunchMode_SESSION_LAUNCH_MODE_INTERACTIVE, Intent: intent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Plan.ActiveSettings.GetConnection() != settings.ConnectionReplacement.Current ||
		resumed.Plan.ActiveSettings.Model != settings.Settings.SelectedAgent.Model {
		t.Fatalf("Desktop settings and interactive resume disagree: %+v, %+v", settings, resumed.Plan.ActiveSettings)
	}
	record, err := persistence.ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.ConnectionID == nil || *record.Meta.ConnectionID != "removed" {
		t.Fatalf("projection or planning changed saved binding: %+v", record.Meta.ConnectionID)
	}
}
