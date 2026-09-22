package serverstatus

import (
	"context"
	"errors"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/auth"
	"core/server/authservice"
	"core/shared/config"
	"core/shared/textutil"

	serverpb "core/shared/protoapi/gen/kent/api/server"

	"google.golang.org/protobuf/types/known/emptypb"
)

func TestGetServerReadinessIncludesWorkflowAssigneeRoles(t *testing.T) {
	readiness := requireServerReadiness(t, nil, config.App{
		Settings: config.Settings{
			Model: "base",
			Workflow: config.WorkflowSettings{
				Subagents: false,
			},
			Subagents: map[string]config.SubagentRole{
				"coder": {
					Settings: config.Settings{Model: "coder-model"},
					Sources:  map[string]config.Origin{"model": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}}},
				},
				"blocked": {
					AgentCallable: false,

					Settings: config.Settings{Model: "blocked-model"},
					Sources:  map[string]config.Origin{"model": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}}, "agent_callable": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "agent_callable"}}},
				},
				"workflow_hidden": {
					Settings:         config.Settings{Model: "workflow-hidden-model"},
					Sources:          map[string]config.Origin{"model": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}}, "workflow_subagent": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "workflow_subagent"}}},
					WorkflowSubagent: false,
				},
			},
		},
	})

	got := make([]string, 0, len(readiness.SubagentRoles))
	for _, role := range readiness.SubagentRoles {
		got = append(got, role.Name)
	}
	want := []string{"default", "fast", "blocked", "coder", "workflow_hidden"}
	if len(got) != len(want) {
		t.Fatalf("roles = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("roles = %+v, want %+v", got, want)
		}
	}
}

func TestGetServerReadinessReadyWhenStartupAuthNotRequired(t *testing.T) {
	readiness := requireServerReadiness(t, nil, config.App{
		Settings: config.Settings{},
	})

	if readiness.AuthRequired {
		t.Fatalf("AuthRequired = true, want false for non-OpenAI provider")
	}
	if !readiness.Ready {
		t.Fatalf("Ready = false, want true when startup auth is not required")
	}
	if len(readiness.Causes) != 0 {
		t.Fatalf("Causes = %+v, want none when ready", readiness.Causes)
	}
}

func TestGetServerReadinessBlockedWhenStartupAuthRequiredButMissing(t *testing.T) {
	readiness := requireServerReadiness(t, nil, config.App{
		Settings: config.Settings{Connection: textutil.Value(config.ConnectionID("work")), Connections: map[config.ConnectionID]config.ProviderConnection{
			"work": {Protocol: config.ConnectionChatGPT},
		}},
	})

	if !readiness.AuthRequired {
		t.Fatalf("AuthRequired = false, want true for OpenAI provider")
	}
	if readiness.Ready {
		t.Fatalf("Ready = true, want false when required auth is missing")
	}
	if len(readiness.Causes) == 0 {
		t.Fatalf("Causes empty, want a startup blocker cause")
	}
}

type failingAuthStore struct{}

func (failingAuthStore) Load(context.Context) (auth.State, error) {
	return auth.State{}, errors.New("auth store unavailable")
}

func (failingAuthStore) Save(context.Context, auth.State) error { return nil }

func TestGetServerReadinessSkipsAuthStateWhenStartupAuthNotRequired(t *testing.T) {
	manager := auth.NewManager(failingAuthStore{}, nil)
	app := testsetup.ProgrammaticConfig(t, config.Settings{})
	service := NewServerStatusService(authservice.NewBootstrapService(authservice.NewConnectionResolver(app.PersistenceRoot, manager, nil), auth.OpenAIOAuthOptions{}), app, nil)

	response, err := service.GetReadiness(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetReadiness: %v", err)
	}
	if !response.Readiness.Ready || response.Readiness.AuthRequired {
		t.Fatalf("readiness = %+v, want ready without required auth", response.Readiness)
	}
}

func TestServerStatusSeparatesReadinessFromLazyUpdateStatus(t *testing.T) {
	source := &countingReleaseSource{metadata: releaseMetadata{Version: updateVersion{components: [3]uint64{1, 2, 0}}}}
	updates := newUpdateStatusService("1.1.0", false, source, time.Now)
	t.Cleanup(func() {
		if err := updates.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	service := NewServerStatusService(nil, config.App{Settings: config.Settings{}}, updates)

	if _, err := service.GetReadiness(context.Background(), &emptypb.Empty{}); err != nil {
		t.Fatalf("GetReadiness: %v", err)
	}
	if calls := source.calls.Load(); calls != 0 {
		t.Fatalf("release checks after readiness = %d, want 0", calls)
	}

	response, err := service.GetUpdateStatus(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetUpdateStatus: %v", err)
	}
	if response.Status.GetAvailable() == nil {
		t.Fatalf("update status = %T, want available", response.Status.GetStatus())
	}
	if calls := source.calls.Load(); calls != 1 {
		t.Fatalf("release checks after update request = %d, want 1", calls)
	}
}

func requireServerReadiness(t *testing.T, manager *auth.Manager, app config.App) *serverpb.Readiness {
	t.Helper()
	bootstrap := authservice.NewBootstrapService(authservice.NewConnectionResolver(t.TempDir(), manager, nil), auth.OpenAIOAuthOptions{})
	if app.Settings.Connection != nil {
		root := t.TempDir()
		testsetup.WriteProviderSettings(t, root, app.Settings)
		bootstrap = authservice.NewBootstrapService(authservice.NewConnectionResolver(root, manager, nil), auth.OpenAIOAuthOptions{})
	}
	response, err := NewServerStatusService(bootstrap, app, nil).GetReadiness(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetReadiness: %v", err)
	}
	return response.Readiness
}
