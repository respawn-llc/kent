package config

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMaxSubagentDepthDefaultsAndRenders(t *testing.T) {
	_, workspace := newConfigTestEnv(t)
	cfg := loadConfigTestApp(t, workspace, LoadOptions{})
	if cfg.Settings.MaxSubagentDepth != 2 {
		t.Fatalf("max_subagent_depth = %d, want 2", cfg.Settings.MaxSubagentDepth)
	}
	if got := cfg.Source.Sources["max_subagent_depth"].Kind; got != "default" {
		t.Fatalf("max_subagent_depth source = %q, want default", got)
	}
	rendered := settingsTOMLWithRenderingOptions(configRegistry.defaultState().Settings, true, nil, nil)
	if !strings.Contains(rendered, "max_subagent_depth = 2") {
		t.Fatalf("default settings TOML missing max_subagent_depth:\n%s", rendered)
	}
}

func TestLoadMaxSubagentDepthAcceptsClosedRange(t *testing.T) {
	for _, value := range []int{0, 1, 2, 30} {
		t.Run(strconv.Itoa(value), func(t *testing.T) {
			_, _, cfg := loadConfigTestFileApp(t, "max_subagent_depth = "+strconv.Itoa(value)+"\n", LoadOptions{})
			if cfg.Settings.MaxSubagentDepth != value {
				t.Fatalf("max_subagent_depth = %d, want %d", cfg.Settings.MaxSubagentDepth, value)
			}
		})
	}
}

func TestLoadMaxSubagentDepthRejectsOutsideClosedRange(t *testing.T) {
	for _, value := range []int{-1, 31} {
		err := loadConfigTestFileError(t, "max_subagent_depth = "+strconv.Itoa(value)+"\n", LoadOptions{})
		if err == nil {
			t.Fatalf("max_subagent_depth=%d succeeded", value)
		}
	}
}

func TestLoadMaxSubagentDepthUsesGlobalThenWorkspacePrecedence(t *testing.T) {
	home, workspace, globalPath := newConfigTestFile(t)
	writeConfigTestFile(t, globalPath, "max_subagent_depth = 1\n")
	workspacePath := filepath.Join(workspace, ConfigDirName, "config.toml")
	writeConfigTestFile(t, workspacePath, "max_subagent_depth = 3\n")

	cfg := loadConfigTestApp(t, workspace, LoadOptions{})
	if cfg.Settings.MaxSubagentDepth != 3 {
		t.Fatalf("max_subagent_depth = %d, want workspace value 3 (home %s)", cfg.Settings.MaxSubagentDepth, home)
	}
	if got := cfg.Source.Sources["max_subagent_depth"].Kind; got != "file" {
		t.Fatalf("max_subagent_depth source = %q, want file", got)
	}
}

func TestLoadMaxSubagentDepthIsTOMLOnly(t *testing.T) {
	_, workspace := newConfigTestEnv(t)
	t.Setenv("KENT_MAX_SUBAGENT_DEPTH", "9")
	cfg := loadConfigTestApp(t, workspace, LoadOptions{})
	if cfg.Settings.MaxSubagentDepth != 2 {
		t.Fatalf("environment changed max_subagent_depth to %d", cfg.Settings.MaxSubagentDepth)
	}
}

func TestLoadSubagentRoleMaxSubagentDepthOverridesGlobal(t *testing.T) {
	_, _, app := loadConfigTestFileApp(t, "max_subagent_depth = 1\n[subagents.worker]\nmax_subagent_depth = 2\n", LoadOptions{})
	role := app.Settings.Subagents["worker"]
	effective, _, err := OverlaySubagentRoleSettings(app, role, true)
	if err != nil {
		t.Fatal(err)
	}
	if effective.MaxSubagentDepth != 2 {
		t.Fatalf("effective max_subagent_depth = %d, want role override 2", effective.MaxSubagentDepth)
	}
}

func TestLoadSubagentRoleMaxSubagentDepthInheritsGlobal(t *testing.T) {
	_, _, app := loadConfigTestFileApp(t, "max_subagent_depth = 1\n[subagents.worker]\n", LoadOptions{})
	role := app.Settings.Subagents["worker"]
	effective, _, err := OverlaySubagentRoleSettings(app, role, true)
	if err != nil {
		t.Fatal(err)
	}
	if effective.MaxSubagentDepth != 1 {
		t.Fatalf("effective max_subagent_depth = %d, want inherited global 1", effective.MaxSubagentDepth)
	}
}

func TestLoadSubagentRoleMaxSubagentDepthAcceptsZero(t *testing.T) {
	_, _, app := loadConfigTestFileApp(t, "max_subagent_depth = 1\n[subagents.worker]\nmax_subagent_depth = 0\n", LoadOptions{})
	role := app.Settings.Subagents["worker"]
	effective, _, err := OverlaySubagentRoleSettings(app, role, true)
	if err != nil {
		t.Fatal(err)
	}
	if effective.MaxSubagentDepth != 0 {
		t.Fatalf("effective max_subagent_depth = %d, want explicit role value 0", effective.MaxSubagentDepth)
	}
}

func TestLoadSubagentRoleMaxSubagentDepthAcceptsMaximum(t *testing.T) {
	_, _, app := loadConfigTestFileApp(t, "max_subagent_depth = 1\n[subagents.worker]\nmax_subagent_depth = 30\n", LoadOptions{})
	role := app.Settings.Subagents["worker"]
	effective, _, err := OverlaySubagentRoleSettings(app, role, true)
	if err != nil {
		t.Fatal(err)
	}
	if effective.MaxSubagentDepth != 30 {
		t.Fatalf("effective max_subagent_depth = %d, want maximum 30", effective.MaxSubagentDepth)
	}
}

func TestLoadSubagentRoleMaxSubagentDepthRejectsOutsideClosedRange(t *testing.T) {
	for _, value := range []int{-1, 31} {
		t.Run(strconv.Itoa(value), func(t *testing.T) {
			err := loadConfigTestFileError(t, "[subagents.worker]\nmax_subagent_depth = "+strconv.Itoa(value)+"\n", LoadOptions{})
			if err == nil {
				t.Fatalf("role max_subagent_depth=%d succeeded", value)
			}
			if !errors.Is(err, errSubagentRole) {
				t.Fatalf("Load error = %v, want role validation failure", err)
			}
			var validationErr *ConfigurationValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("Load error = %v, want configuration validation failure", err)
			}
		})
	}
}
