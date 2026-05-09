package embeddedstartup

import (
	"context"
	"errors"
	"testing"

	"builder/server/auth"
	serverstartup "builder/server/startup"
	"builder/shared/config"
)

type stubOnboardingHandler struct {
	run func(context.Context, OnboardingRequest) (config.App, error)
}

func (h stubOnboardingHandler) EnsureOnboardingReady(ctx context.Context, req OnboardingRequest) (config.App, error) {
	if h.run == nil {
		return req.Config, nil
	}
	return h.run(ctx, req)
}

func TestBuildStartupRequestMapsOptions(t *testing.T) {
	req := buildStartupRequest(Request{
		WorkspaceRoot:         "/tmp/workspace",
		WorkspaceRootExplicit: true,
		SessionID:             "session-123",
		OpenAIBaseURL:         "http://127.0.0.1:8080/v1",
		OpenAIBaseURLExplicit: true,
		LoadOptions: config.LoadOptions{
			Model:               "gpt-5",
			ProviderOverride:    "openai",
			ThinkingLevel:       "high",
			Theme:               "dark",
			ModelTimeoutSeconds: 42,
			Tools:               "shell,patch",
		},
	})

	if req.WorkspaceRoot != "/tmp/workspace" || !req.WorkspaceRootExplicit {
		t.Fatalf("unexpected workspace mapping: %+v", req)
	}
	if req.SessionID != "session-123" {
		t.Fatalf("session id = %q, want session-123", req.SessionID)
	}
	if req.OpenAIBaseURL != "http://127.0.0.1:8080/v1" || !req.OpenAIBaseURLExplicit {
		t.Fatalf("unexpected base url mapping: %+v", req)
	}
	if req.LoadOptions.Model != "gpt-5" || req.LoadOptions.ProviderOverride != "openai" || req.LoadOptions.ThinkingLevel != "high" {
		t.Fatalf("unexpected model/provider/thinking mapping: %+v", req.LoadOptions)
	}
	if req.LoadOptions.Theme != "dark" || req.LoadOptions.ModelTimeoutSeconds != 42 || req.LoadOptions.Tools != "shell,patch" {
		t.Fatalf("unexpected load options: %+v", req.LoadOptions)
	}
}

func TestAdaptOnboardingHandlerMapsRequest(t *testing.T) {
	expected := errors.New("mapped")
	mgr := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil, nil)
	reload := func() (config.App, error) {
		return config.App{WorkspaceRoot: "/reloaded"}, nil
	}
	adapter := adaptOnboardingHandler(stubOnboardingHandler{run: func(ctx context.Context, req OnboardingRequest) (config.App, error) {
		if req.Config.WorkspaceRoot != "/workspace" {
			t.Fatalf("workspace root = %q, want /workspace", req.Config.WorkspaceRoot)
		}
		if req.AuthManager != mgr {
			t.Fatal("auth manager was not mapped")
		}
		reloaded, err := req.ReloadConfig()
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if reloaded.WorkspaceRoot != "/reloaded" {
			t.Fatalf("reloaded workspace = %q, want /reloaded", reloaded.WorkspaceRoot)
		}
		return config.App{}, expected
	}})

	_, err := adapter.EnsureOnboardingReady(context.Background(), serverstartup.OnboardingRequest{
		Config:       config.App{WorkspaceRoot: "/workspace"},
		AuthManager:  mgr,
		ReloadConfig: reload,
	})
	if !errors.Is(err, expected) {
		t.Fatalf("expected mapped error, got %v", err)
	}
}

func TestAdaptOnboardingHandlerAllowsNil(t *testing.T) {
	if adaptOnboardingHandler(nil) != nil {
		t.Fatal("expected nil adapter")
	}
}
