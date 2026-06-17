package protocol

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPathForContainer(t *testing.T) {
	containerDir := filepath.Join(t.TempDir(), "projects", "project-a", "sessions")
	path, err := DiscoveryPathForContainer(containerDir)
	if err != nil {
		t.Fatalf("DiscoveryPathForContainer: %v", err)
	}
	if want := filepath.Join(containerDir, DiscoveryFilename); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

func TestWriteReadAndRemoveDiscoveryRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app-server.json")
	record := DiscoveryRecord{
		Identity:  ServerIdentity{ProtocolVersion: Version, ServerID: "server-1", PID: 123},
		HTTPURL:   "http://127.0.0.1:1234",
		RPCURL:    "ws://127.0.0.1:1234/rpc",
		StartedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := WriteDiscovery(path, record); err != nil {
		t.Fatalf("WriteDiscovery: %v", err)
	}
	loaded, err := ReadDiscovery(path)
	if err != nil {
		t.Fatalf("ReadDiscovery: %v", err)
	}
	if loaded.Identity.ServerID != "server-1" || loaded.RPCURL != record.RPCURL || loaded.HTTPURL != record.HTTPURL {
		t.Fatalf("unexpected discovery record: %+v", loaded)
	}
	if err := RemoveDiscovery(path); err != nil {
		t.Fatalf("RemoveDiscovery: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected discovery record to be removed, got err=%v", err)
	}
}
