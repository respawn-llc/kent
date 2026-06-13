package discovery

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"core/shared/protocol"
)

func PathForContainer(containerDir string) (string, error) {
	trimmed := strings.TrimSpace(containerDir)
	if trimmed == "" {
		return "", fmt.Errorf("container dir is required")
	}
	return filepath.Join(trimmed, protocol.DiscoveryFilename), nil
}

func Write(path string, record protocol.DiscoveryRecord) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return fmt.Errorf("discovery path is required")
	}
	if err := os.MkdirAll(filepath.Dir(trimmed), 0o755); err != nil {
		return fmt.Errorf("create discovery dir: %w", err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal discovery record: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(trimmed), ".app-server.*.json")
	if err != nil {
		return fmt.Errorf("create temporary discovery file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		_ = tmp.Close()
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write discovery record: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		return fmt.Errorf("chmod discovery record: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close discovery record: %w", err)
	}
	if err := os.Rename(tmpPath, trimmed); err != nil {
		return fmt.Errorf("replace discovery record: %w", err)
	}
	cleanup = false
	return nil
}

func Read(path string) (protocol.DiscoveryRecord, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return protocol.DiscoveryRecord{}, fmt.Errorf("discovery path is required")
	}
	data, err := os.ReadFile(trimmed)
	if err != nil {
		return protocol.DiscoveryRecord{}, err
	}
	var record protocol.DiscoveryRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return protocol.DiscoveryRecord{}, fmt.Errorf("decode discovery record: %w", err)
	}
	return record, nil
}

func Remove(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil
	}
	if err := os.Remove(trimmed); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove discovery record: %w", err)
	}
	return nil
}
