package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"core/shared/runtimeids"
	"core/shared/sessioncontract"
)

func TestResolveScopedSessionDirReturnsRealPathInsideContainer(t *testing.T) {
	root := t.TempDir()
	containerDir := filepath.Join(root, "workspace-a")
	store, err := Create(containerDir, "workspace-a", "/tmp/workspace-a", testSessionCategory, sessionTestPersistence.options()...)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	resolvedDir, err := ResolveScopedSessionDir(containerDir, store.Meta().SessionID)
	if err != nil {
		t.Fatalf("ResolveScopedSessionDir: %v", err)
	}
	realStoreDir, err := filepath.EvalSymlinks(store.Dir())
	if err != nil {
		t.Fatalf("EvalSymlinks store dir: %v", err)
	}
	if resolvedDir != realStoreDir {
		t.Fatalf("resolved dir = %q, want %q", resolvedDir, realStoreDir)
	}
}

func TestResolveScopedSessionDirRejectsSymlinkOutsideContainer(t *testing.T) {
	root := t.TempDir()
	containerA := filepath.Join(root, "workspace-a")
	containerB := filepath.Join(root, "workspace-b")
	if err := os.MkdirAll(containerA, 0o755); err != nil {
		t.Fatalf("mkdir container A: %v", err)
	}
	escaped, err := Create(containerB, "workspace-b", "/tmp/workspace-b", testSessionCategory, sessionTestPersistence.options()...)
	if err != nil {
		t.Fatalf("create escaped session: %v", err)
	}
	escapedID := runtimeids.NewSessionID().String()
	if err := os.Symlink(escaped.Dir(), filepath.Join(containerA, escapedID)); err != nil {
		t.Fatalf("symlink escaped session: %v", err)
	}
	if _, err := ResolveScopedSessionDir(containerA, escapedID); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}

func TestResolveScopedSessionDirWrapsSessionNotFound(t *testing.T) {
	root := t.TempDir()
	containerDir := filepath.Join(root, "workspace-a")
	if err := os.MkdirAll(containerDir, 0o755); err != nil {
		t.Fatalf("mkdir container: %v", err)
	}

	_, err := ResolveScopedSessionDir(containerDir, runtimeids.NewSessionID().String())
	if err == nil {
		t.Fatal("expected missing scoped session to fail")
	}
	if !errors.Is(err, sessioncontract.ErrSessionNotFound) {
		t.Fatalf("error = %v, want ErrSessionNotFound", err)
	}
}

func TestResolveScopedSessionDirUsesRuntimeSessionIDSyntax(t *testing.T) {
	containerDir := t.TempDir()
	sessionID := runtimeids.NewSessionID().String()
	sessionDir := filepath.Join(containerDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session: %v", err)
	}
	if _, err := ResolveScopedSessionDir(containerDir, sessionID); err != nil {
		t.Fatalf("resolve canonical session ID: %v", err)
	}
	if _, err := ResolveScopedSessionDir(containerDir, " "+sessionID); err == nil {
		t.Fatal("padded session ID succeeded")
	}
}
