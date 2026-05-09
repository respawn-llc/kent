package runprompttarget

import (
	"context"
	"errors"
	"strings"
	"testing"

	"builder/shared/client"
	"builder/shared/config"
)

type testRunPromptClient struct {
	client.RunPromptClient
}

type testAuthClient struct {
	client.AuthBootstrapClient
}

func TestEmbeddedTargetCarriesClientProjectAndClose(t *testing.T) {
	closed := false
	runPrompt := &testRunPromptClient{}
	target := Embedded(runPrompt, func() string { return "project-1" }, func() error {
		closed = true
		return nil
	})

	if target.Value.Client != runPrompt {
		t.Fatalf("client = %T, want embedded run prompt client", target.Value.Client)
	}
	if target.Value.Auth != nil {
		t.Fatalf("auth = %T, want nil for embedded target", target.Value.Auth)
	}
	if target.Value.ProjectID == nil || target.Value.ProjectID() != "project-1" {
		t.Fatalf("project id = %q, want project-1", target.Value.ProjectID())
	}
	if err := target.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !closed {
		t.Fatal("expected close function called")
	}
}

func TestValidateEnsuresRemoteAuthAndProjectRegistration(t *testing.T) {
	auth := &testAuthClient{}
	calls := 0
	err := Validate(context.Background(), ValidateRequest{
		Config: config.App{WorkspaceRoot: "/repo"},
		Target: Target{
			Auth:      auth,
			ProjectID: func() string { return "project-1" },
		},
		EnsureAuthReady: func(_ context.Context, got client.AuthBootstrapClient) error {
			calls++
			if got != auth {
				t.Fatalf("auth client = %T, want test auth client", got)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if calls != 1 {
		t.Fatalf("ensure calls = %d, want 1", calls)
	}
}

func TestValidateReturnsAuthErrorBeforeProjectRegistration(t *testing.T) {
	authErr := errors.New("auth failed")
	err := Validate(context.Background(), ValidateRequest{
		Config: config.App{WorkspaceRoot: "/repo"},
		Target: Target{Auth: &testAuthClient{}},
		EnsureAuthReady: func(context.Context, client.AuthBootstrapClient) error {
			return authErr
		},
	})
	if !errors.Is(err, authErr) {
		t.Fatalf("Validate error = %v, want auth error", err)
	}
}

func TestValidateRequiresProjectRegistration(t *testing.T) {
	err := Validate(context.Background(), ValidateRequest{
		Config: config.App{WorkspaceRoot: "/repo"},
		Target: Target{ProjectID: func() string { return " " }},
	})
	if err == nil || !strings.Contains(err.Error(), "/repo") {
		t.Fatalf("Validate error = %v, want workspace registration error", err)
	}
}
