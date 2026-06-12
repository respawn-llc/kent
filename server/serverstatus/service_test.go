package serverstatus

import (
	"context"
	"errors"
	"testing"

	"builder/server/auth"
	"builder/shared/config"
	"builder/shared/serverapi"
)

func TestGetServerReadinessIncludesWorkflowAssigneeRoles(t *testing.T) {
	service := NewService(nil, config.App{
		Settings: config.Settings{
			Model: "base",
			Subagents: map[string]config.SubagentRole{
				"coder": {
					Settings: config.Settings{Model: "coder-model"},
					Sources:  map[string]string{"model": "test"},
				},
				"blocked": {
					AgentCallable:    false,
					AgentCallableSet: true,
					Settings:         config.Settings{Model: "blocked-model"},
					Sources:          map[string]string{"model": "test"},
				},
			},
		},
	})

	readiness, err := service.GetServerReadiness(context.Background(), serverapi.ServerReadinessRequest{})
	if err != nil {
		t.Fatalf("GetServerReadiness: %v", err)
	}

	got := make([]string, 0, len(readiness.SubagentRoles))
	for _, role := range readiness.SubagentRoles {
		got = append(got, role.Name)
	}
	want := []string{"default", "fast", "blocked", "coder"}
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
	service := NewService(nil, config.App{
		Settings: config.Settings{ProviderOverride: "anthropic"},
	})

	readiness, err := service.GetServerReadiness(context.Background(), serverapi.ServerReadinessRequest{})
	if err != nil {
		t.Fatalf("GetServerReadiness: %v", err)
	}

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
	service := NewService(nil, config.App{
		Settings: config.Settings{ProviderOverride: "openai"},
	})

	readiness, err := service.GetServerReadiness(context.Background(), serverapi.ServerReadinessRequest{})
	if err != nil {
		t.Fatalf("GetServerReadiness: %v", err)
	}

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

func TestGetServerReadinessIgnoresAuthStoreWhenStartupAuthNotRequired(t *testing.T) {
	manager := auth.NewManager(failingAuthStore{}, nil, nil)
	service := NewService(manager, config.App{Settings: config.Settings{ProviderOverride: "anthropic"}})

	readiness, err := service.GetServerReadiness(context.Background(), serverapi.ServerReadinessRequest{})
	if err != nil {
		t.Fatalf("GetServerReadiness should not fail on auth store when auth not required: %v", err)
	}
	if readiness.AuthRequired {
		t.Fatalf("AuthRequired = true, want false for non-OpenAI provider")
	}
	if !readiness.Ready {
		t.Fatalf("Ready = false, want true when startup auth is not required")
	}
}

func TestGetServerReadinessSurfacesAuthStoreErrorWhenStartupAuthRequired(t *testing.T) {
	manager := auth.NewManager(failingAuthStore{}, nil, nil)
	service := NewService(manager, config.App{Settings: config.Settings{ProviderOverride: "openai"}})

	if _, err := service.GetServerReadiness(context.Background(), serverapi.ServerReadinessRequest{}); err == nil {
		t.Fatal("expected auth store error to surface when startup auth is required")
	}
}
