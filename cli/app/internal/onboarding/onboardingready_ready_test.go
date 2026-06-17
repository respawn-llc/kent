package onboarding

import (
	"context"
	"errors"
	"testing"
	"time"

	"core/server/auth"
	serverstartup "core/server/startup"
	"core/shared/config"
)

func TestEnsureSkipsWhenSettingsFileExists(t *testing.T) {
	cfg := config.App{Source: config.SourceReport{SettingsFileExists: true, SettingsPath: "/tmp/settings.toml"}}
	got, changed, err := Ensure(context.Background(), Request{
		Config: cfg,
		ReloadConfig: func() (config.App, error) {
			t.Fatal("reload should not be called")
			return config.App{}, nil
		},
	})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if changed {
		t.Fatal("expected unchanged config")
	}
	if got.Source.SettingsPath != "/tmp/settings.toml" || !got.Source.SettingsFileExists {
		t.Fatalf("unexpected config passthrough: %+v", got.Source)
	}
}

func TestEnsureInteractivePassesLoadedAuthStateToRunner(t *testing.T) {
	expected := auth.State{Method: auth.Method{Type: auth.MethodAPIKey, APIKey: &auth.APIKeyMethod{Key: "sk-test"}}}
	mgr := auth.NewManager(auth.NewMemoryStore(expected), nil, time.Now)
	called := false
	got, changed, err := Ensure(context.Background(), Request{
		Config:      config.App{},
		AuthManager: mgr,
		Interactive: true,
		ReloadConfig: func() (config.App, error) {
			return config.App{Source: config.SourceReport{SettingsPath: "/tmp/reloaded.toml"}}, nil
		},
		Runner: func(_ context.Context, _ config.App, authState AuthState) (Result, error) {
			called = true
			if authState.Method.APIKey == nil || authState.Method.APIKey.Key != "sk-test" {
				t.Fatalf("unexpected auth state: %+v", authState.Method)
			}
			return Result{Completed: true, CreatedDefaultConfig: true, SettingsPath: "/tmp/settings.toml"}, nil
		},
	})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !called {
		t.Fatal("expected runner call")
	}
	if !changed {
		t.Fatal("expected onboarding changes")
	}
	if !got.Source.CreatedDefaultConfig || !got.Source.SettingsFileExists || got.Source.SettingsPath != "/tmp/settings.toml" {
		t.Fatalf("unexpected source metadata: %+v", got.Source)
	}
}

func TestEnsureInteractiveRequiresAuthManager(t *testing.T) {
	_, _, err := Ensure(context.Background(), Request{
		Interactive:  true,
		ReloadConfig: func() (config.App, error) { return config.App{}, nil },
		Runner: func(context.Context, config.App, AuthState) (Result, error) {
			return Result{}, nil
		},
	})
	if err == nil || !errors.Is(err, serverstartup.ErrOnboardingAuthManagerRequired) {
		t.Fatalf("expected missing auth manager error, got %v", err)
	}
}

func TestEnsureReturnsRunnerError(t *testing.T) {
	expected := errors.New("runner failed")
	mgr := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil, time.Now)
	_, _, err := Ensure(context.Background(), Request{
		AuthManager:  mgr,
		Interactive:  true,
		ReloadConfig: func() (config.App, error) { return config.App{}, nil },
		Runner: func(context.Context, config.App, AuthState) (Result, error) {
			return Result{}, expected
		},
	})
	if !errors.Is(err, expected) {
		t.Fatalf("expected runner error, got %v", err)
	}
}
