package sessionlaunch

import (
	"reflect"
	"testing"

	"core/server/launch"
	"core/shared/config"
	"core/shared/serverapi"
	"core/shared/textutil"
)

func TestParentAgentChildrenAllocateFreshConnectionsAndResumeSticky(t *testing.T) {
	cfg := loadSessionLaunchTestConfig(t, t.TempDir(), t.TempDir())
	selection := config.ConnectionSelection{"a", "b"}
	cfg.Settings.Connection = &selection
	cfg.Settings.ConnectionOrder = []config.ConnectionID{"a", "b"}
	definition := cfg.Settings.Connections["test"]
	cfg.Settings.Connections = map[config.ConnectionID]config.ProviderConnection{
		"a": definition, "b": definition,
	}
	cfg.Settings.Subagents["worker"] = config.SubagentRole{
		Settings:      config.Settings{Model: "gpt-6-luna"},
		AgentCallable: true,
	}
	service := newSessionLaunchTestService(cfg, t.TempDir())
	service.planner.Rotation = new(launch.ConnectionRotation)
	service.planner.CallerSessions = serviceTestPersistence
	parent, err := service.PlanLaunchSession(t.Context(), PlanRequest{
		Mode: launch.ModeInteractive,
		Intent: serverapi.CreateNewSessionLaunchIntent(
			serverapi.IndependentSessionCreateOrigin(),
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	parentID := parent.Plan.Descriptor.SessionID()
	if !reflect.DeepEqual(parent.Plan.ActiveSettings.Connection, config.SingleConnection("a")) {
		t.Fatalf("parent connection = %v, want a", parent.Plan.ActiveSettings.Connection)
	}
	callerID := parentID.String()
	for _, want := range []config.ConnectionID{"b", "a", "b"} {
		child, err := service.PlanLaunchSession(t.Context(), PlanRequest{
			Mode: launch.ModeHeadless,
			Intent: serverapi.CreateNewSessionLaunchIntent(
				serverapi.ParentAgentSessionCreateOrigin(parentID),
			),
			CallerSessionID: &callerID,
			Overrides:       serverapi.RunPromptOverrides{AgentRole: textutil.Value("worker")},
		})
		if err != nil {
			t.Fatal(err)
		}
		childID := child.Plan.Descriptor.SessionID()
		record, err := serviceTestPersistence.ResolvePersistedSession(t.Context(), childID.String())
		if err != nil {
			t.Fatal(err)
		}
		if record.Meta.ParentAgentSessionID == nil || *record.Meta.ParentAgentSessionID != parentID {
			t.Fatalf("child parent = %v, want %v", record.Meta.ParentAgentSessionID, parentID)
		}
		if !reflect.DeepEqual(child.Plan.ActiveSettings.Connection, config.SingleConnection(want)) ||
			record.Meta.ConnectionID == nil || *record.Meta.ConnectionID != want {
			t.Fatalf("child connection = %v, persisted = %v, want %s",
				child.Plan.ActiveSettings.Connection, record.Meta.ConnectionID, want)
		}
		for id, binding := range map[string]config.ConnectionID{parentID.String(): "a", childID.String(): want} {
			reopened, err := service.PlanLaunchSession(t.Context(), PlanRequest{
				Mode:   launch.ModeHeadless,
				Intent: serverapi.OpenExistingSessionLaunchIntent(mustSessionLaunchIntentID(t, id)),
			})
			if err != nil {
				t.Fatal(err)
			}
			if reopened.Plan.Descriptor.SessionID().String() != id ||
				!reflect.DeepEqual(reopened.Plan.ActiveSettings.Connection, config.SingleConnection(binding)) {
				t.Fatalf("reopened Session %s = %v, want unchanged identity and connection %s", id, reopened.Plan, binding)
			}
		}
	}
}
