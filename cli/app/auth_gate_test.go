package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"builder/server/auth"
	"builder/server/authflow"
	"builder/server/session"
	"builder/shared/config"
)

type stubAuthInteractor struct {
	callCount      int
	interactive    bool
	interactiveSet bool
	needs          func(authInteraction) bool
	interact       func(context.Context, authInteraction) (authflow.InteractionOutcome, error)
}

func (s *stubAuthInteractor) WrapStore(base auth.Store) auth.Store {
	return base
}

func (s *stubAuthInteractor) NeedsInteraction(req authInteraction) bool {
	if s.needs != nil {
		return s.needs(req)
	}
	return !req.Gate.Ready
}

func (s *stubAuthInteractor) Interact(ctx context.Context, req authInteraction) (authflow.InteractionOutcome, error) {
	s.callCount++
	if s.interact == nil {
		return authflow.InteractionOutcome{}, nil
	}
	return s.interact(ctx, req)
}

func (s *stubAuthInteractor) LookupEnv(string) string {
	return ""
}

func (s *stubAuthInteractor) Interactive() bool {
	if !s.interactiveSet {
		return true
	}
	return s.interactive
}

func TestBootstrapAppHeadlessUsesEnvAPIKeyWithoutPersistingAuthState(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENAI_API_KEY", "sk-env")
	registerAppWorkspace(t, workspace)

	boot, err := startEmbeddedServer(context.Background(), Options{WorkspaceRoot: workspace}, newHeadlessAuthInteractor())
	if err != nil {
		t.Fatalf("bootstrap app: %v", err)
	}
	defer func() { _ = boot.Close() }()

	state, err := boot.AuthManager().Load(context.Background())
	if err != nil {
		t.Fatalf("load auth state: %v", err)
	}
	if state.Method.Type != auth.MethodAPIKey {
		t.Fatalf("expected env api key auth, got %q", state.Method.Type)
	}
	if state.Method.APIKey == nil || state.Method.APIKey.Key != "sk-env" {
		t.Fatalf("expected env api key to be visible through manager, got %+v", state.Method.APIKey)
	}

	authPath := config.GlobalAuthConfigPath(boot.Config())
	if _, err := os.Stat(authPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no persisted auth state at %q, got err=%v", authPath, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".builder", "config.toml")); err != nil {
		t.Fatalf("expected config bootstrap artifacts to exist: %v", err)
	}
}

func TestResolveSessionActionLogoutUsesBootstrapAuthInteractor(t *testing.T) {
	ctx := context.Background()
	mgr := auth.NewManager(auth.NewMemoryStore(auth.State{
		Scope: auth.ScopeGlobal,
		Method: auth.Method{
			Type:   auth.MethodAPIKey,
			APIKey: &auth.APIKeyMethod{Key: "sk-before"},
		},
	}), nil, time.Now)
	pickerCalls := 0
	interactor := &interactiveAuthInteractor{
		lookupEnv: func(key string) string {
			if key == "OPENAI_API_KEY" {
				return "sk-after"
			}
			return ""
		},
		pickMethod: func(req authInteraction) (authMethodPickerResult, error) {
			pickerCalls++
			if !req.AuthRequired {
				t.Fatal("expected logout reauth to require auth for default OpenAI config")
			}
			if !req.HasEnvAPIKey {
				t.Fatal("expected env api key to be available for bootstrap")
			}
			return authMethodPickerResult{Choice: authMethodChoiceEnvAPIKey}, nil
		},
	}

	root := t.TempDir()
	store, err := session.Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}

	resolved, err := resolveSessionAction(
		ctx,
		&testEmbeddedServer{cfg: config.App{PersistenceRoot: root, Settings: config.Settings{Model: "gpt-5"}}, authManager: mgr},
		interactor,
		store.Meta().SessionID,
		"lease-test-controller",
		UITransition{Action: UIActionLogout},
	)
	if err != nil {
		t.Fatalf("resolve session action: %v", err)
	}
	if pickerCalls != 1 {
		t.Fatalf("expected auth picker to be called once, got %d", pickerCalls)
	}
	if !resolved.ShouldContinue {
		t.Fatal("expected logout flow to continue after reauth")
	}
	if resolved.NextSessionID != store.Meta().SessionID {
		t.Fatalf("expected session to continue in place, got %q", resolved.NextSessionID)
	}
	if resolved.InitialPrompt != "" || resolved.InitialInput != "" || resolved.ParentSessionID != "" || resolved.ForceNewSession {
		t.Fatalf("unexpected logout transition values prompt=%q input=%q parent=%q forceNew=%t", resolved.InitialPrompt, resolved.InitialInput, resolved.ParentSessionID, resolved.ForceNewSession)
	}

	state, err := mgr.Load(ctx)
	if err != nil {
		t.Fatalf("load auth state: %v", err)
	}
	if state.Method.APIKey == nil || state.Method.APIKey.Key != "sk-after" {
		t.Fatalf("expected logout flow to restore auth method, got %+v", state.Method.APIKey)
	}
}

func TestResolveSessionActionLogoutAllowsNilStore(t *testing.T) {
	ctx := context.Background()
	mgr := auth.NewManager(auth.NewMemoryStore(auth.State{
		Scope: auth.ScopeGlobal,
		Method: auth.Method{
			Type:   auth.MethodAPIKey,
			APIKey: &auth.APIKeyMethod{Key: "sk-before"},
		},
	}), nil, time.Now)
	pickerCalls := 0
	interactor := &interactiveAuthInteractor{
		lookupEnv: func(key string) string {
			if key == "OPENAI_API_KEY" {
				return "sk-after"
			}
			return ""
		},
		pickMethod: func(authInteraction) (authMethodPickerResult, error) {
			pickerCalls++
			return authMethodPickerResult{Choice: authMethodChoiceEnvAPIKey}, nil
		},
	}

	resolved, err := resolveSessionAction(
		ctx,
		&testEmbeddedServer{authManager: mgr},
		interactor,
		"",
		"",
		UITransition{Action: UIActionLogout},
	)
	if err != nil {
		t.Fatalf("resolve session action: %v", err)
	}
	if pickerCalls != 1 {
		t.Fatalf("expected auth picker to be called once, got %d", pickerCalls)
	}
	if !resolved.ShouldContinue {
		t.Fatal("expected logout flow to continue after reauth")
	}
	if resolved.NextSessionID != "" {
		t.Fatalf("expected no next session id without a current store, got %q", resolved.NextSessionID)
	}
}

func TestBootstrapAppSkipAuthDoesNotPersistAuthState(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENAI_API_KEY", "")
	registerAppWorkspace(t, workspace)

	interactor := &stubAuthInteractor{
		interactive:    false,
		interactiveSet: true,
		interact: func(context.Context, authInteraction) (authflow.InteractionOutcome, error) {
			return authflow.InteractionOutcome{ProceedWithoutAuth: true}, nil
		},
	}
	boot, err := startEmbeddedServer(context.Background(), Options{WorkspaceRoot: workspace, Model: "gpt-5"}, interactor)
	if err != nil {
		t.Fatalf("bootstrap app: %v", err)
	}
	defer func() { _ = boot.Close() }()
	if interactor.callCount != 1 {
		t.Fatalf("expected skip-auth interactor to be called once, got %d", interactor.callCount)
	}

	authPath := config.GlobalAuthConfigPath(boot.Config())
	if _, err := os.Stat(authPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no persisted auth state at %q, got err=%v", authPath, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".builder", "config.toml")); err != nil {
		t.Fatalf("expected onboarding config bootstrap artifacts to exist: %v", err)
	}
}

func TestInteractiveAuthSkipClearsStoredAuthState(t *testing.T) {
	mgr := auth.NewManager(auth.NewMemoryStore(auth.State{
		Scope: auth.ScopeGlobal,
		Method: auth.Method{
			Type:   auth.MethodAPIKey,
			APIKey: &auth.APIKeyMethod{Key: "sk-before"},
		},
		EnvAPIKeyPreference: auth.EnvAPIKeyPreferencePreferSaved,
	}), nil, time.Now)
	storedState, err := mgr.StoredState(context.Background())
	if err != nil {
		t.Fatalf("load stored state: %v", err)
	}

	interactor := &interactiveAuthInteractor{
		pickMethod: func(authInteraction) (authMethodPickerResult, error) {
			return authMethodPickerResult{Choice: authMethodChoiceSkip}, nil
		},
	}
	outcome, err := interactor.Interact(context.Background(), authInteraction{
		Manager:     mgr,
		StoredState: storedState,
	})
	if err != nil {
		t.Fatalf("interactive skip: %v", err)
	}
	if !outcome.ProceedWithoutAuth {
		t.Fatal("expected skip to proceed without auth")
	}

	state, err := mgr.StoredState(context.Background())
	if err != nil {
		t.Fatalf("load cleared state: %v", err)
	}
	if state.Method.Type != auth.MethodNone {
		t.Fatalf("expected stored auth method to be cleared, got %+v", state.Method)
	}
	if state.EnvAPIKeyPreference != auth.EnvAPIKeyPreferenceUnspecified {
		t.Fatalf("expected env api key preference to be cleared, got %q", state.EnvAPIKeyPreference)
	}
}

func TestInteractiveAuthSkipDisablesEnvAPIKeyFallback(t *testing.T) {
	ctx := context.Background()
	store := authflow.WrapStoreWithEnvAPIKeyOverride(auth.NewMemoryStore(auth.EmptyState()), func(key string) string {
		if key == "OPENAI_API_KEY" {
			return "sk-env"
		}
		return ""
	})
	mgr := auth.NewManager(store, nil, time.Now)
	storedState, err := mgr.StoredState(ctx)
	if err != nil {
		t.Fatalf("load stored state: %v", err)
	}

	interactor := &interactiveAuthInteractor{
		pickMethod: func(authInteraction) (authMethodPickerResult, error) {
			return authMethodPickerResult{Choice: authMethodChoiceSkip}, nil
		},
	}
	outcome, err := interactor.Interact(ctx, authInteraction{
		Manager:      mgr,
		StoredState:  storedState,
		HasEnvAPIKey: true,
	})
	if err != nil {
		t.Fatalf("interactive skip: %v", err)
	}
	if !outcome.ProceedWithoutAuth {
		t.Fatal("expected skip to proceed without auth")
	}

	state, err := mgr.Load(ctx)
	if err != nil {
		t.Fatalf("load auth state: %v", err)
	}
	if state.Method.Type != auth.MethodNone {
		t.Fatalf("expected env auth override to stay disabled after skip, got %+v", state.Method)
	}
	if state.EnvAPIKeyPreference != auth.EnvAPIKeyPreferencePreferSaved {
		t.Fatalf("expected skip to persist saved-auth preference, got %q", state.EnvAPIKeyPreference)
	}
	stored, err := mgr.StoredState(ctx)
	if err != nil {
		t.Fatalf("load stored auth state: %v", err)
	}
	if stored.EnvAPIKeyPreference != auth.EnvAPIKeyPreferencePreferSaved {
		t.Fatalf("expected stored preference to disable env fallback, got %q", stored.EnvAPIKeyPreference)
	}
}

func TestInteractiveAuthSkipRejectsRequiredAuth(t *testing.T) {
	interactor := &interactiveAuthInteractor{
		pickMethod: func(authInteraction) (authMethodPickerResult, error) {
			return authMethodPickerResult{Choice: authMethodChoiceSkip}, nil
		},
	}
	_, err := interactor.Interact(context.Background(), authInteraction{
		Manager:      auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil, time.Now),
		AuthRequired: true,
	})
	if err == nil {
		t.Fatal("expected required-auth skip to be rejected")
	}
	if err.Error() != "builder auth is required for this configuration" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveSessionActionLoginSkipClearsStoredAuthOnOptionalAuthSetup(t *testing.T) {
	ctx := context.Background()
	mgr := auth.NewManager(auth.NewMemoryStore(auth.State{
		Scope: auth.ScopeGlobal,
		Method: auth.Method{
			Type:   auth.MethodAPIKey,
			APIKey: &auth.APIKeyMethod{Key: "sk-before"},
		},
		EnvAPIKeyPreference: auth.EnvAPIKeyPreferencePreferSaved,
	}), nil, time.Now)

	interactor := &interactiveAuthInteractor{
		pickMethod: func(req authInteraction) (authMethodPickerResult, error) {
			if req.AuthRequired {
				t.Fatal("expected explicit openai base url setup to make auth optional")
			}
			if !req.PromptOptional {
				t.Fatal("expected explicit /login flow to prompt even when auth is optional")
			}
			if req.StartupErr != nil {
				t.Fatalf("expected optional auth login flow to avoid startup error, got %v", req.StartupErr)
			}
			return authMethodPickerResult{Choice: authMethodChoiceSkip}, nil
		},
	}

	resolved, err := resolveSessionAction(
		ctx,
		&testEmbeddedServer{cfg: config.App{Settings: config.Settings{Model: "gpt-5", OpenAIBaseURL: "http://127.0.0.1:8080/v1"}}, authManager: mgr},
		interactor,
		"",
		"",
		UITransition{Action: UIActionLogout},
	)
	if err != nil {
		t.Fatalf("resolve session action: %v", err)
	}
	if !resolved.ShouldContinue {
		t.Fatal("expected login skip flow to continue")
	}

	state, err := mgr.StoredState(ctx)
	if err != nil {
		t.Fatalf("load stored state: %v", err)
	}
	if state.Method.Type != auth.MethodNone {
		t.Fatalf("expected stored auth method to be cleared, got %+v", state.Method)
	}
	if state.EnvAPIKeyPreference != auth.EnvAPIKeyPreferenceUnspecified {
		t.Fatalf("expected env api key preference to be cleared, got %q", state.EnvAPIKeyPreference)
	}
}
