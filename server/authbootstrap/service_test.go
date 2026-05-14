package authbootstrap

import (
	"context"
	"testing"
	"time"

	"builder/server/auth"
	"builder/shared/config"
	"builder/shared/serverapi"
)

func TestCompleteBootstrapConfiguresAPIKeyWhenAuthNotReady(t *testing.T) {
	service, store := newTestAuthBootstrapService(auth.EmptyState())

	resp, err := service.CompleteBootstrap(context.Background(), serverapi.AuthCompleteBootstrapRequest{
		Mode:   serverapi.AuthBootstrapModeAPIKey,
		APIKey: "server-key",
	})
	if err != nil {
		t.Fatalf("CompleteBootstrap: %v", err)
	}
	if !resp.AuthReady {
		t.Fatal("expected auth ready after bootstrap completion")
	}
	if resp.MethodType != string(auth.MethodAPIKey) {
		t.Fatalf("method type = %q, want %q", resp.MethodType, auth.MethodAPIKey)
	}
	state, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if state.Method.APIKey == nil || state.Method.APIKey.Key != "server-key" {
		t.Fatalf("stored method = %+v, want server-key", state.Method)
	}
}

func TestCompleteBootstrapReturnsSuccessWithoutOverwriteWhenAuthAlreadyReady(t *testing.T) {
	service, store := newTestAuthBootstrapService(auth.State{
		Scope: auth.ScopeGlobal,
		Method: auth.Method{
			Type:   auth.MethodAPIKey,
			APIKey: &auth.APIKeyMethod{Key: "server-key"},
		},
	})

	resp, err := service.CompleteBootstrap(context.Background(), serverapi.AuthCompleteBootstrapRequest{
		Mode:   serverapi.AuthBootstrapModeAPIKey,
		APIKey: "server-key-2",
	})
	if err != nil {
		t.Fatalf("CompleteBootstrap: %v", err)
	}
	if !resp.AuthReady {
		t.Fatal("expected ready auth to return successful no-op")
	}
	if resp.MethodType != string(auth.MethodAPIKey) {
		t.Fatalf("method type = %q, want %q", resp.MethodType, auth.MethodAPIKey)
	}
	state, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if state.Method.APIKey == nil || state.Method.APIKey.Key != "server-key" {
		t.Fatalf("stored method = %+v, want original server-key", state.Method)
	}
}

func TestCompleteBootstrapNoneClearsAuthWhenAuthOptional(t *testing.T) {
	service, store := newTestAuthBootstrapServiceWithSettings(auth.State{
		Scope: auth.ScopeGlobal,
		Method: auth.Method{
			Type:   auth.MethodAPIKey,
			APIKey: &auth.APIKeyMethod{Key: "server-key"},
		},
	}, config.Settings{OpenAIBaseURL: "http://127.0.0.1:8080/v1"})

	resp, err := service.CompleteBootstrap(context.Background(), serverapi.AuthCompleteBootstrapRequest{Mode: serverapi.AuthBootstrapModeNone})
	if err != nil {
		t.Fatalf("CompleteBootstrap none: %v", err)
	}
	if !resp.AuthReady {
		t.Fatal("expected optional auth skip to be ready")
	}
	state, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if state.Method.Type != auth.MethodNone {
		t.Fatalf("stored method = %+v, want none", state.Method)
	}
	if state.EnvAPIKeyPreference != auth.EnvAPIKeyPreferencePreferSaved {
		t.Fatalf("env preference = %q, want no-auth preference", state.EnvAPIKeyPreference)
	}
}

func TestCompleteBootstrapNoneSavesNoAuthPreferenceWhenAuthRequired(t *testing.T) {
	service, store := newTestAuthBootstrapService(auth.State{
		Scope: auth.ScopeGlobal,
		Method: auth.Method{
			Type:   auth.MethodAPIKey,
			APIKey: &auth.APIKeyMethod{Key: "server-key"},
		},
	})

	resp, err := service.CompleteBootstrap(context.Background(), serverapi.AuthCompleteBootstrapRequest{Mode: serverapi.AuthBootstrapModeNone})
	if err != nil {
		t.Fatalf("CompleteBootstrap none: %v", err)
	}
	if resp.AuthReady {
		t.Fatal("did not expect no-auth preference to satisfy required startup readiness")
	}
	state, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !state.IsNoAuthSelected() {
		t.Fatalf("stored state = %+v, want no-auth preference", state)
	}
}

func newTestAuthBootstrapService(initial auth.State) (*Service, *auth.MemoryStore) {
	return newTestAuthBootstrapServiceWithSettings(initial, config.Settings{Model: "gpt-5"})
}

func newTestAuthBootstrapServiceWithSettings(initial auth.State, settings config.Settings) (*Service, *auth.MemoryStore) {
	store := auth.NewMemoryStore(initial)
	manager := auth.NewManager(store, nil, func() time.Time {
		return time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	})
	return NewService(manager, auth.OpenAIOAuthOptions{}, settings, nil), store
}
