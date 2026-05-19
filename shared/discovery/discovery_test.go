package discovery

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"builder/shared/protocol"
)

func TestPathForContainer(t *testing.T) {
	containerDir := filepath.Join(t.TempDir(), "projects", "project-a", "sessions")
	path, err := PathForContainer(containerDir)
	if err != nil {
		t.Fatalf("PathForContainer: %v", err)
	}
	if want := filepath.Join(containerDir, protocol.DiscoveryFilename); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

func TestWriteReadAndRemoveDiscoveryRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app-server.json")
	record := protocol.DiscoveryRecord{
		Identity:  protocol.ServerIdentity{ProtocolVersion: protocol.Version, ServerID: "server-1", PID: 123},
		HTTPURL:   "http://127.0.0.1:1234",
		RPCURL:    "ws://127.0.0.1:1234/rpc",
		StartedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := Write(path, record); err != nil {
		t.Fatalf("Write: %v", err)
	}
	loaded, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if loaded.Identity.ServerID != "server-1" || loaded.RPCURL != record.RPCURL || loaded.HTTPURL != record.HTTPURL {
		t.Fatalf("unexpected discovery record: %+v", loaded)
	}
	if err := Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected discovery record to be removed, got err=%v", err)
	}
}
