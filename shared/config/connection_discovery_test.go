package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConnectionDiscoveryReportsSourceParseAndConsumedTypeFailures(t *testing.T) {
	for _, body := range []string{`server_port = [`, `server_port = "not a port"`, `server_host = false`} {
		t.Run(body, func(t *testing.T) {
			t.Setenv(PersistenceRootEnvName, filepath.Join(t.TempDir(), "data"))
			workspace := t.TempDir()
			path := filepath.Join(workspace, ConfigDirName, "config.toml")
			writeConfigTestFile(t, path, body)
			_, err := LoadConnectionDiscovery(workspace, LoadOptions{})
			var sourceError *ConfigurationFileError
			if !errors.As(err, &sourceError) || sourceError.Source.Path != path {
				t.Fatalf("expected selected source failure, got %v", err)
			}
		})
	}
}

func TestConnectionDiscoveryLayeringAndLocalPreferences(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	t.Setenv(PersistenceRootEnvName, root)
	writeConfigTestFile(t, filepath.Join(root, "config.toml"), `
server_host = "global.example"
server_port = 1234
theme = "light"
[hooks.client]
lifecycle = ["echo", "event"]
`)
	writeConfigTestFile(t, filepath.Join(workspace, ConfigDirName, "config.toml"), `
server_host = "workspace.example"
theme = "dark"
`)
	cfg, preferences, err := LoadInteractiveConnectionDiscovery(workspace, LoadOptions{Theme: "light"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerHost != "workspace.example" || cfg.ServerPort != 1234 || preferences.Theme != "light" {
		t.Fatalf("layering: %+v, %+v", cfg, preferences)
	}
	if cfg.Source.Sources["server_host"].File.Layer != FileWorkspace ||
		cfg.Source.Sources["persistence_root"].Kind != SourceEnv ||
		len(preferences.Client.Hooks.LifecycleCommand()) != 2 {
		t.Fatalf("sources/preferences: %+v, %+v", cfg, preferences)
	}
	t.Setenv("KENT_SERVER_HOST", "env.example")
	cfg, err = LoadConnectionDiscovery(workspace, LoadOptions{})
	if err != nil || cfg.ServerHost != "env.example" || cfg.Source.Sources["server_host"].Kind != SourceEnv {
		t.Fatalf("environment targeting: %+v, %v", cfg, err)
	}
	writeConfigTestFile(t, filepath.Join(root, "config.toml"), `
theme = false
[hooks.client]
lifecycle = false
`)
	if _, err := LoadConnectionDiscovery(workspace, LoadOptions{}); err != nil {
		t.Fatalf("plain commands must not decode local preferences: %v", err)
	}
	if _, _, err := LoadInteractiveConnectionDiscovery(workspace, LoadOptions{}); err == nil {
		t.Fatal("interactive discovery accepted unreadable local preferences")
	}
}

func TestConnectionDiscoveryIgnoresOperationalSettingsWithoutSetup(t *testing.T) {
	root := filepath.Join(t.TempDir(), "server-data")
	workspace := t.TempDir()
	t.Setenv(PersistenceRootEnvName, root)
	writeConfigTestFile(t, filepath.Join(workspace, ConfigDirName, "config.toml"), `
server_host = "localhost"
server_port = 4321
model = false
tools = 42
unknown_setting = "ignored"
[worktrees]
base_dir = false
`)
	cfg, err := LoadConnectionDiscovery(workspace, LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerHost != "localhost" || cfg.ServerPort != 4321 {
		t.Fatalf("endpoint = %s:%d", cfg.ServerHost, cfg.ServerPort)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("discovery prepared persistence root: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(workspace, ConfigDirName))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		t.Fatalf("discovery wrote workspace artifacts: %v", entries)
	}
}
