package serverstatus

import (
	"context"
	"testing"
	"time"

	"core/shared/config"
	"core/shared/textutil"

	serverpb "core/shared/protoapi/gen/kent/api/server"

	"google.golang.org/protobuf/types/known/emptypb"
)

func TestGetServerReadinessIncludesWorkflowAssigneeRoles(t *testing.T) {
	readiness := requireServerReadiness(t, config.App{
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
	readiness := requireServerReadiness(t, config.App{
		Settings: config.Settings{},
	})

	if !readiness.Ready {
		t.Fatalf("Ready = false, want true when startup auth is not required")
	}
	if len(readiness.Causes) != 0 {
		t.Fatalf("Causes = %+v, want none when ready", readiness.Causes)
	}
}

func TestGetServerReadinessDoesNotRequireDefaultConnectionCredentials(t *testing.T) {
	readiness := requireServerReadiness(t, config.App{
		Settings: config.Settings{Connection: textutil.Value(config.ConnectionID("work")), Connections: map[config.ConnectionID]config.ProviderConnection{
			"work": {Protocol: config.ConnectionChatGPT},
		}},
	})

	if !readiness.Ready || len(readiness.Causes) != 0 {
		t.Fatalf("provider credentials must not block server readiness: %+v", readiness)
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
	service := NewServerStatusService(config.App{Settings: config.Settings{}}, updates)

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

func requireServerReadiness(t *testing.T, app config.App) *serverpb.Readiness {
	t.Helper()
	response, err := NewServerStatusService(app, nil).GetReadiness(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetReadiness: %v", err)
	}
	return response.Readiness
}
