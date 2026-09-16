package auth

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestFileStoreSaveWritesWithSecurePermissions(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "auth-state.json")
	store := NewFileStore(statePath)

	state := testAPIKeyState("secret-key")

	if err := store.Save(context.Background(), state); err != nil {
		t.Fatalf("save auth state: %v", err)
	}

	assertAuthStateFileMode(t, statePath, authStateFileMode)
}

func TestFileStorePreservesCredentialsOnRewrite(t *testing.T) {
	tests := []struct {
		name   string
		method string
		want   Method
	}{
		{
			name: "OAuth",
			method: `{"type": "oauth", "oauth": {
				"access_token": "saved-access",
				"refresh_token": "saved-refresh",
				"token_type": "Bearer",
				"expiry": "2030-01-01T12:00:00Z",
				"account_id": "saved-account",
				"email": "saved@example.invalid"
			}}`,
			want: Method{Type: MethodOAuth, OAuth: &OAuthMethod{
				AccessToken: "saved-access", RefreshToken: "saved-refresh",
				Expiry:    time.Date(2030, time.January, 1, 12, 0, 0, 0, time.UTC),
				AccountID: "saved-account", Email: "saved@example.invalid",
			}},
		},
		{
			name:   "API key",
			method: `{"type": "api_key", "api_key": {"key": "saved-key"}}`,
			want:   Method{Type: MethodAPIKey, APIKey: &APIKeyMethod{Key: "saved-key"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "auth.json")
			persisted := `{
				"scope": "global",
				"env_api_key_preference": "prefer_saved_auth",
				"updated_at": "2026-01-01T10:00:00Z",
				"method": ` + test.method + `
			}`
			if err := os.WriteFile(path, []byte(persisted), 0o600); err != nil {
				t.Fatal(err)
			}
			store := NewFileStore(path)
			before := requireAuthState(t, store.Load)
			if err := store.Save(context.Background(), before); err != nil {
				t.Fatal(err)
			}
			after := requireAuthState(t, store.Load)
			want := State{Scope: ScopeGlobal, Method: test.want, EnvAPIKeyPreference: EnvAPIKeyPreferencePreferSaved}
			if !reflect.DeepEqual(before, want) || !reflect.DeepEqual(after, want) {
				t.Fatal("credentials, identity, expiry, or auth selection were not preserved")
			}
		})
	}
}

func TestFileStoreLoadCorrectsExistingFilePermissions(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "auth-state.json")

	writeAuthStateFile(t, statePath, testOAuthState(), 0o644)

	store := NewFileStore(statePath)
	loaded := requireAuthState(t, store.Load)
	if loaded.Method.Type != MethodOAuth {
		t.Fatalf("expected oauth method, got %q", loaded.Method.Type)
	}

	assertAuthStateFileMode(t, statePath, authStateFileMode)
}

func TestFileStoreLoadDoesNotBroadenStrictPermissions(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "auth-state.json")

	writeAuthStateFile(t, statePath, testOAuthState(), 0o400)

	store := NewFileStore(statePath)
	requireAuthState(t, store.Load)

	assertAuthStateFileMode(t, statePath, 0o400)
}

func TestFileStoreSaveCorrectsExistingInsecurePermissions(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "auth-state.json")

	writeAuthStateFile(t, statePath, testAPIKeyState("seed-key"), 0o644)

	store := NewFileStore(statePath)
	next := testAPIKeyState("next-key")
	if err := store.Save(context.Background(), next); err != nil {
		t.Fatalf("save auth state: %v", err)
	}

	assertAuthStateFileMode(t, statePath, authStateFileMode)
}

func TestEnvAPIKeyOverrideStoreLoadAlwaysPrefersEnvironmentWithoutPersistedState(t *testing.T) {
	store := NewEnvAPIKeyOverrideStore(NewMemoryStore(testOAuthState()), testEnvAPIKey("  sk-env  "))

	state := requireAuthState(t, store.Load)
	if state.Method.Type != MethodAPIKey {
		t.Fatalf("expected api key override, got %q", state.Method.Type)
	}
	if state.Method.APIKey == nil || state.Method.APIKey.Key != "sk-env" {
		t.Fatalf("expected trimmed env api key, got %+v", state.Method.APIKey)
	}
}

func TestEnvAPIKeyOverrideStoreRespectsSavedPreference(t *testing.T) {
	persisted := testOAuthState()
	persisted.EnvAPIKeyPreference = EnvAPIKeyPreferencePreferEnv
	store := NewEnvAPIKeyOverrideStore(NewMemoryStore(persisted), testEnvAPIKey("sk-env"))

	state := requireAuthState(t, store.Load)
	if state.Method.Type != MethodAPIKey {
		t.Fatalf("expected api key override, got %q", state.Method.Type)
	}
	if state.Method.APIKey == nil || state.Method.APIKey.Key != "sk-env" {
		t.Fatalf("expected env api key override, got %+v", state.Method.APIKey)
	}
}

func TestEnvAPIKeyOverrideStoreKeepsSavedOAuthWhenPreferencePrefersSaved(t *testing.T) {
	persisted := testOAuthState()
	persisted.EnvAPIKeyPreference = EnvAPIKeyPreferencePreferSaved
	store := NewEnvAPIKeyOverrideStore(NewMemoryStore(persisted), testEnvAPIKey("sk-env"))

	state := requireAuthState(t, store.Load)
	if state.Method.Type != MethodOAuth {
		t.Fatalf("expected saved oauth method, got %q", state.Method.Type)
	}
}

func TestEnvAPIKeyOverrideStoreSaveDelegatesToBaseStore(t *testing.T) {
	base := NewMemoryStore(EmptyState())
	store := NewEnvAPIKeyOverrideStore(base, func(string) (string, bool) { return "", false })

	want := testAPIKeyState("sk-saved")
	if err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("save auth state: %v", err)
	}

	loaded := requireAuthState(t, base.Load)
	if loaded.Method.Type != MethodAPIKey {
		t.Fatalf("expected delegated api key save, got %q", loaded.Method.Type)
	}
	if loaded.Method.APIKey == nil || loaded.Method.APIKey.Key != "sk-saved" {
		t.Fatalf("expected delegated saved key, got %+v", loaded.Method.APIKey)
	}
}

func TestEnvAPIKeyOverrideStoreLoadPersistedReturnsBaseState(t *testing.T) {
	base := NewMemoryStore(testOAuthState())
	store := NewEnvAPIKeyOverrideStore(base, testEnvAPIKey("sk-env"))

	loaded := requireAuthState(t, store.LoadPersisted)
	if loaded.Method.Type != MethodOAuth {
		t.Fatalf("expected base oauth state, got %q", loaded.Method.Type)
	}
}

type persistedDecoratorStore struct {
	loadState      State
	persistedState State
}

func (s *persistedDecoratorStore) Load(context.Context) (State, error) {
	return s.loadState, nil
}

func (s *persistedDecoratorStore) LoadPersisted(context.Context) (State, error) {
	return s.persistedState, nil
}

func (s *persistedDecoratorStore) Save(context.Context, State) error {
	return nil
}

func TestEnvAPIKeyOverrideStoreLoadPersistedDelegatesToBasePersistedLoader(t *testing.T) {
	base := &persistedDecoratorStore{
		loadState: State{
			Scope:  ScopeGlobal,
			Method: Method{Type: MethodAPIKey, APIKey: &APIKeyMethod{Key: "runtime-key"}},
		},
		persistedState: State{
			Scope:  ScopeGlobal,
			Method: Method{Type: MethodOAuth, OAuth: &OAuthMethod{AccessToken: "persisted-access", RefreshToken: "persisted-refresh"}},
		},
	}
	store := NewEnvAPIKeyOverrideStore(base, func(string) (string, bool) { return "sk-env", true })

	loaded := requireAuthState(t, store.LoadPersisted)
	if loaded.Method.Type != MethodOAuth {
		t.Fatalf("expected persisted oauth state, got %q", loaded.Method.Type)
	}
}

func testAPIKeyState(key string) State {
	return State{
		Scope:  ScopeGlobal,
		Method: Method{Type: MethodAPIKey, APIKey: &APIKeyMethod{Key: key}},
	}
}

func testOAuthState() State {
	return State{
		Scope: ScopeGlobal,
		Method: Method{
			Type: MethodOAuth,
			OAuth: &OAuthMethod{
				AccessToken:  "oauth-access",
				RefreshToken: "oauth-refresh",
				Expiry:       time.Date(2026, time.January, 1, 11, 0, 0, 0, time.UTC),
			},
		},
	}
}

func testEnvAPIKey(value string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		if key == "OPENAI_API_KEY" {
			return value, true
		}
		return "", false
	}
}

func writeAuthStateFile(t *testing.T, path string, state State, mode os.FileMode) {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal auth state: %v", err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatalf("write auth state: %v", err)
	}
}

func assertAuthStateFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat auth state: %v", err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("auth state mode = %04o, want %04o", got, want)
	}
}

func requireAuthState(t *testing.T, load func(context.Context) (State, error)) State {
	t.Helper()
	state, err := load(context.Background())
	if err != nil {
		t.Fatalf("load auth state: %v", err)
	}
	return state
}
