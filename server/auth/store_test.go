package auth

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"core/shared/config"
)

func TestFileStoreSaveWritesWithSecurePermissions(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "auth-state.json")
	store := NewFileStore(statePath)

	state := testOAuthState()

	if err := store.Save(context.Background(), state); err != nil {
		t.Fatalf("save auth state: %v", err)
	}

	assertAuthStateFileMode(t, statePath, authStateFileMode)
}

func TestFileStorePreservesCredentialsOnRewrite(t *testing.T) {
	tests := []struct {
		name       string
		credential string
		want       OAuthMethod
	}{
		{
			name: "OAuth",
			credential: `{
				"access_token": "saved-access",
				"refresh_token": "saved-refresh",
				"token_type": "Bearer",
				"expiry": "2030-01-01T12:00:00Z",
				"account_id": "saved-account",
				"email": "saved@example.invalid"
			}`,
			want: OAuthMethod{
				AccessToken: "saved-access", RefreshToken: "saved-refresh",
				Expiry:    time.Date(2030, time.January, 1, 12, 0, 0, 0, time.UTC),
				AccountID: "saved-account", Email: "saved@example.invalid",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "auth.json")
			persisted := `{"connections": {"work": ` + test.credential + `}}`
			if err := os.WriteFile(path, []byte(persisted), 0o600); err != nil {
				t.Fatal(err)
			}
			store := NewFileStore(path)
			before := requireAuthState(t, store.Load)
			if err := store.Save(context.Background(), before); err != nil {
				t.Fatal(err)
			}
			after := requireAuthState(t, store.Load)
			want := State{Connections: map[config.ConnectionID]OAuthMethod{"work": test.want}}
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
	if loaded.Connections["work"].AccessToken != "oauth-access" {
		t.Fatal("expected connection credential")
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

	writeAuthStateFile(t, statePath, testOAuthState(), 0o644)

	store := NewFileStore(statePath)
	next := testOAuthState()
	if err := store.Save(context.Background(), next); err != nil {
		t.Fatalf("save auth state: %v", err)
	}

	assertAuthStateFileMode(t, statePath, authStateFileMode)
}

func testOAuthState() State {
	return State{
		Connections: map[config.ConnectionID]OAuthMethod{
			"work": {
				AccessToken:  "oauth-access",
				RefreshToken: "oauth-refresh",
				Expiry:       time.Date(2026, time.January, 1, 11, 0, 0, 0, time.UTC),
			},
		},
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
