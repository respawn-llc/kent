package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWorkflowConfigDefaults(t *testing.T) {
	_, workspace := newConfigTestEnv(t)

	cfg := loadConfigTestApp(t, workspace, LoadOptions{})
	if cfg.Settings.Workflow.CompletionMode != WorkflowCompletionModeAuto {
		t.Fatalf("completion mode = %q, want auto", cfg.Settings.Workflow.CompletionMode)
	}
	if cfg.Settings.Workflow.Concurrency != 5 {
		t.Fatalf("concurrency = %d, want 5", cfg.Settings.Workflow.Concurrency)
	}
	if cfg.Settings.Workflow.MaxFinalAnswerViolations != 3 {
		t.Fatalf("max final answer violations = %d, want 3", cfg.Settings.Workflow.MaxFinalAnswerViolations)
	}
	if cfg.Settings.Workflow.MaxInvalidCompletionAttempts != 5 {
		t.Fatalf("max invalid completion attempts = %d, want 5", cfg.Settings.Workflow.MaxInvalidCompletionAttempts)
	}
}

func TestDefaultSettingsTOMLRendersWorkflowDefaults(t *testing.T) {
	rendered := defaultSettingsTOML()
	if !strings.Contains(rendered, "[workflow]") {
		t.Fatalf("default TOML missing workflow section:\n%s", rendered)
	}
	for _, want := range []string{
		"completion_mode = \"auto\"",
		"concurrency = 5",
		"max_final_answer_violations = 3",
		"max_invalid_completion_attempts = 5",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("default TOML missing %q:\n%s", want, rendered)
		}
	}
}

func TestLoadWorkflowConfigFromFile(t *testing.T) {
	_, workspace, configPath := newConfigTestFile(t)
	writeConfigTestFile(t, configPath, `[workflow]
completion_mode = "tool"
concurrency = 7
max_final_answer_violations = 4
max_invalid_completion_attempts = 6
`)

	cfg := loadConfigTestApp(t, workspace, LoadOptions{})
	if cfg.Settings.Workflow.CompletionMode != WorkflowCompletionModeTool || cfg.Settings.Workflow.Concurrency != 7 || cfg.Settings.Workflow.MaxFinalAnswerViolations != 4 || cfg.Settings.Workflow.MaxInvalidCompletionAttempts != 6 {
		t.Fatalf("workflow settings = %+v", cfg.Settings.Workflow)
	}
	if got := cfg.Source.Sources["workflow.completion_mode"]; got != "file" {
		t.Fatalf("workflow.completion_mode source = %q, want file", got)
	}
}

func TestLoadWorkflowConfigValidation(t *testing.T) {
	for name, payload := range map[string]string{
		"completion_mode":                 "[workflow]\ncompletion_mode = \"invalid\"\n",
		"concurrency":                     "[workflow]\nconcurrency = 0\n",
		"max_final_answer_violations":     "[workflow]\nmax_final_answer_violations = 0\n",
		"max_invalid_completion_attempts": "[workflow]\nmax_invalid_completion_attempts = 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			workspace := t.TempDir()
			t.Setenv("HOME", home)
			configPath := filepath.Join(home, ".builder", "config.toml")
			if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(configPath, []byte(payload), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(workspace, LoadOptions{})
			if err == nil || !strings.Contains(err.Error(), "workflow.") {
				t.Fatalf("Load error = %v, want workflow validation error", err)
			}
		})
	}
}

func TestLoadSubagentRoleWorkflowConfigValidation(t *testing.T) {
	home, workspace := newConfigTestEnv(t)
	configPath := filepath.Join(home, ".builder", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("[subagents.fast.workflow]\nconcurrency = 0\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(workspace, LoadOptions{})
	if err == nil || !strings.Contains(err.Error(), "workflow.concurrency") {
		t.Fatalf("Load error = %v, want subagent workflow validation error", err)
	}
}
