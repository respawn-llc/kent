package config

import (
	"path/filepath"
	"strconv"
	"testing"
)

func TestShellConcurrencyConfiguration(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		_, workspace := newConfigTestEnv(t)
		if got := loadConfigTestApp(t, workspace, LoadOptions{}).Settings.Shell.MaxConcurrent; got != 100 {
			t.Fatalf("default = %d, want 100", got)
		}
	})
	for _, value := range []int{1, 20, 1000} {
		t.Run(strconv.Itoa(value), func(t *testing.T) {
			_, _, cfg := loadConfigTestFileApp(t, "[shell]\nmax_concurrent = "+strconv.Itoa(value)+"\n", LoadOptions{})
			if cfg.Settings.Shell.MaxConcurrent != value {
				t.Fatalf("limit = %d, want %d", cfg.Settings.Shell.MaxConcurrent, value)
			}
		})
	}
	for _, value := range []string{"0", "-1", "1.5", `"20"`} {
		t.Run("invalid "+value, func(t *testing.T) {
			if err := loadConfigTestFileError(t, "[shell]\nmax_concurrent = "+value+"\n", LoadOptions{}); err == nil {
				t.Fatal("invalid shell limit accepted")
			}
		})
	}
	t.Run("workspace cannot override server", func(t *testing.T) {
		_, workspace, globalPath := newConfigTestFile(t)
		writeConfigTestFile(t, globalPath, "[shell]\nmax_concurrent = 20\n")
		writeConfigTestFile(t, filepath.Join(workspace, ConfigDirName, "config.toml"), "[shell]\nmax_concurrent = 30\n")
		if _, err := Load(workspace, workspace, LoadOptions{}); err == nil {
			t.Fatal("workspace shell limit accepted")
		}
	})
	t.Run("subagent cannot override server", func(t *testing.T) {
		if err := loadConfigTestFileError(t, "[subagents.worker.shell]\nmax_concurrent = 20\n", LoadOptions{}); err == nil {
			t.Fatal("subagent shell limit accepted")
		}
	})
}
