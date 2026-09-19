package auth

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
				TokenType:    "Bearer",
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
