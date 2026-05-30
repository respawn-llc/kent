package config

import (
	"os"
	"path/filepath"
	"testing"
)

func newConfigTestEnv(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	return home, workspace
}

func newConfigTestFile(t *testing.T) (string, string, string) {
	t.Helper()
	home, workspace := newConfigTestEnv(t)
	configPath := filepath.Join(home, ".builder", "config.toml")
	ensureConfigTestDir(t, configPath)
	return home, workspace, configPath
}

func ensureConfigTestDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
}

func writeConfigTestFile(t *testing.T, path string, contents string) {
	t.Helper()
	ensureConfigTestDir(t, path)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func loadConfigTestFileApp(t *testing.T, contents string, opts LoadOptions) (string, string, App) {
	t.Helper()
	home, workspace, configPath := newConfigTestFile(t)
	writeConfigTestFile(t, configPath, contents)
	return home, workspace, loadConfigTestApp(t, workspace, opts)
}

func loadConfigTestFileError(t *testing.T, contents string, opts LoadOptions) error {
	t.Helper()
	_, workspace, configPath := newConfigTestFile(t)
	writeConfigTestFile(t, configPath, contents)
	_, err := Load(workspace, opts)
	return err
}

func loadConfigTestApp(t *testing.T, workspace string, opts LoadOptions) App {
	t.Helper()
	cfg, err := Load(workspace, opts)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return cfg
}

func assertConfigSource(t *testing.T, cfg App, key string, want string) {
	t.Helper()
	if got := cfg.Source.Sources[key]; got != want {
		t.Fatalf("expected %s source %s, got %q", key, want, got)
	}
}
