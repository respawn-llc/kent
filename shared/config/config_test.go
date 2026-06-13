package config

import (
	"core/shared/brand"
	"core/shared/toolspec"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestPreparePersistenceRootRefusesProcessStartRootUnderGoTest(t *testing.T) {
	originalHome := processStartHome
	originalAccountHome := processStartAccountHome
	processStartHome = filepath.Join(string(filepath.Separator), "kent-test-home")
	processStartAccountHome = ""
	t.Cleanup(func() {
		processStartHome = originalHome
		processStartAccountHome = originalAccountHome
	})

	_, err := preparePersistenceRoot(filepath.Join(processStartHome, brand.ConfigDirName))
	if err == nil {
		t.Fatal("expected process-start persistence root to be refused under go test")
	}
	if !strings.Contains(err.Error(), "refusing to use protected persistence root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPreparePersistenceRootAllowsIsolatedTempHomeUnderGoTest(t *testing.T) {
	originalHome := processStartHome
	originalAccountHome := processStartAccountHome
	processStartHome = t.TempDir()
	processStartAccountHome = filepath.Join(string(filepath.Separator), "builder-real-home")
	t.Cleanup(func() {
		processStartHome = originalHome
		processStartAccountHome = originalAccountHome
	})

	if _, err := preparePersistenceRoot(filepath.Join(processStartHome, brand.ConfigDirName)); err != nil {
		t.Fatalf("prepare temp persistence root: %v", err)
	}
}

func TestLoadUsesDefaultsWithoutCreatingConfigOnFirstUse(t *testing.T) {
	home, workspace := newConfigTestEnv(t)
	cfg := loadConfigTestApp(t, workspace, LoadOptions{})

	settingsPath := filepath.Join(home, brand.ConfigDirName, "config.toml")
	if _, err := os.Stat(settingsPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected config file to stay absent, got err=%v", err)
	}
	rgConfigPath := filepath.Join(home, brand.ConfigDirName, managedRGConfigName)
	rgConfigBytes, err := os.ReadFile(rgConfigPath)
	if err != nil {
		t.Fatalf("read managed rg config: %v", err)
	}
	if string(rgConfigBytes) != managedRGConfigContents {
		t.Fatalf("managed rg config contents mismatch: %q", string(rgConfigBytes))
	}
	if cfg.Source.CreatedDefaultConfig {
		t.Fatalf("expected CreatedDefaultConfig=false")
	}
	if cfg.Source.SettingsFileExists {
		t.Fatalf("expected SettingsFileExists=false")
	}
	if cfg.Settings.Model != defaultModel {
		t.Fatalf("default model mismatch: %q", cfg.Settings.Model)
	}
	if cfg.Settings.WebSearch != "native" {
		t.Fatalf("default web_search mismatch: %q", cfg.Settings.WebSearch)
	}
	if cfg.Settings.ModelVerbosity != defaultModelVerbosity {
		t.Fatalf("default model_verbosity mismatch: %q", cfg.Settings.ModelVerbosity)
	}
	if cfg.Settings.NotificationMethod != "auto" {
		t.Fatalf("default notification_method mismatch: %q", cfg.Settings.NotificationMethod)
	}
	if !cfg.Settings.ToolPreambles {
		t.Fatalf("expected default tool_preambles=true")
	}
	if cfg.Settings.PriorityRequestMode {
		t.Fatalf("expected default priority_request_mode=false")
	}
	if cfg.Settings.Debug {
		t.Fatalf("expected default debug=false")
	}
	if got := cfg.PersistenceRoot; got != filepath.Join(home, brand.ConfigDirName) {
		t.Fatalf("default persistence root mismatch: %q", got)
	}
	if _, err := os.Stat(filepath.Join(cfg.PersistenceRoot, "sessions")); err == nil || !os.IsNotExist(err) {
		t.Fatalf("expected legacy sessions root to stay absent, got %v", err)
	}
	if !cfg.Settings.EnabledTools[toolspec.ToolExecCommand] || !cfg.Settings.EnabledTools[toolspec.ToolViewImage] || !cfg.Settings.EnabledTools[toolspec.ToolPatch] || !cfg.Settings.EnabledTools[toolspec.ToolAskQuestion] {
		t.Fatalf("expected all default tools enabled: %+v", cfg.Settings.EnabledTools)
	}
	if cfg.Settings.EnabledTools[toolspec.ToolTriggerHandoff] {
		t.Fatalf("expected %s disabled in static defaults", toolspec.ToolTriggerHandoff)
	}
	if got := cfg.Source.Sources["tools.trigger_handoff"]; got != "default" {
		t.Fatalf("expected untouched %s source to remain default, got %q", toolspec.ToolTriggerHandoff, got)
	}
	if !cfg.Settings.EnabledTools[toolspec.ToolWebSearch] {
		t.Fatalf("expected web_search tool enabled by default: %+v", cfg.Settings.EnabledTools)
	}
	if cfg.Settings.ContextCompactionThresholdTokens != defaultCompactionThreshold {
		t.Fatalf("default compaction threshold mismatch: %d", cfg.Settings.ContextCompactionThresholdTokens)
	}
	if cfg.Settings.PreSubmitCompactionLeadTokens != 35_000 {
		t.Fatalf("default pre-submit runway mismatch: %d", cfg.Settings.PreSubmitCompactionLeadTokens)
	}
	if cfg.Settings.MinimumExecToBgSeconds != defaultMinimumExecToBgSec {
		t.Fatalf("default minimum_exec_to_bg_seconds mismatch: %d", cfg.Settings.MinimumExecToBgSeconds)
	}
	if cfg.Settings.ModelContextWindow != defaultModelContextWindow {
		t.Fatalf("default model context window mismatch: %d", cfg.Settings.ModelContextWindow)
	}
	if cfg.Settings.Store {
		t.Fatalf("expected default store=false")
	}
	if cfg.Settings.AllowNonCwdEdits {
		t.Fatalf("expected default allow_non_cwd_edits=false")
	}
	if cfg.Settings.CompactionMode != CompactionModeLocal {
		t.Fatalf("expected default compaction_mode=local, got %q", cfg.Settings.CompactionMode)
	}
	if cfg.Settings.ShellOutputMaxChars != 16000 {
		t.Fatalf("default shell_output_max_chars mismatch: %d", cfg.Settings.ShellOutputMaxChars)
	}
	if cfg.Settings.BGShellsOutput != BGShellsOutputDefault {
		t.Fatalf("default bg_shells_output mismatch: %q", cfg.Settings.BGShellsOutput)
	}
	if cfg.Settings.Shell.PostprocessingMode != ShellPostprocessingModeBuiltin {
		t.Fatalf("default shell.postprocessing_mode mismatch: %q", cfg.Settings.Shell.PostprocessingMode)
	}
	if cfg.Settings.Shell.PostprocessHook != "" {
		t.Fatalf("default shell.postprocess_hook mismatch: %q", cfg.Settings.Shell.PostprocessHook)
	}
	if got := cfg.Settings.Worktrees.BaseDir; got != filepath.Join(cfg.PersistenceRoot, "worktrees") {
		t.Fatalf("default worktrees.base_dir mismatch: %q", got)
	}
	if cfg.Settings.Worktrees.SetupScript != "" {
		t.Fatalf("expected default worktrees.setup_script empty, got %q", cfg.Settings.Worktrees.SetupScript)
	}
	if cfg.Settings.Reviewer.Frequency != defaultReviewerFrequency {
		t.Fatalf("expected default reviewer.frequency=%s, got %q", defaultReviewerFrequency, cfg.Settings.Reviewer.Frequency)
	}
	if cfg.Settings.Reviewer.Model != cfg.Settings.Model {
		t.Fatalf("default reviewer model mismatch: %q", cfg.Settings.Reviewer.Model)
	}
	if cfg.Settings.Reviewer.ThinkingLevel != cfg.Settings.ThinkingLevel {
		t.Fatalf("default reviewer thinking_level mismatch: %q", cfg.Settings.Reviewer.ThinkingLevel)
	}
	if cfg.Settings.Reviewer.TimeoutSeconds != 60 {
		t.Fatalf("default reviewer timeout mismatch: %d", cfg.Settings.Reviewer.TimeoutSeconds)
	}
	if cfg.Settings.Reviewer.VerboseOutput {
		t.Fatalf("expected default reviewer verbose_output=false")
	}
}

func TestLoadUsesExplicitConfigRootWithoutHomeMutation(t *testing.T) {
	configRoot := t.TempDir()
	workspace := t.TempDir()

	cfg, err := Load(workspace, LoadOptions{ConfigRoot: configRoot})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Source.HomeSettingsPath != filepath.Join(configRoot, "config.toml") {
		t.Fatalf("home settings path = %q, want explicit config root", cfg.Source.HomeSettingsPath)
	}
	if cfg.PersistenceRoot != configRoot {
		t.Fatalf("persistence root = %q, want explicit config root", cfg.PersistenceRoot)
	}
	if _, err := os.Stat(filepath.Join(configRoot, managedRGConfigName)); err != nil {
		t.Fatalf("expected managed rg config in explicit config root: %v", err)
	}
}

func TestLoadExplicitConfigRootOverridesNestedPersistenceRoot(t *testing.T) {
	configRoot := t.TempDir()
	otherRoot := t.TempDir()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(configRoot, "config.toml"), []byte("persistence_root = \""+filepath.ToSlash(otherRoot)+"\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(workspace, LoadOptions{ConfigRoot: configRoot})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.PersistenceRoot != configRoot {
		t.Fatalf("persistence root = %q, want explicit config root %q", cfg.PersistenceRoot, configRoot)
	}
	if got := cfg.Source.Sources["persistence_root"]; got != "config_root" {
		t.Fatalf("persistence_root source = %q, want config_root", got)
	}
}

func TestWriteManagedRGConfigFileForSettingsPathRejectsEmptyPath(t *testing.T) {
	if _, err := writeManagedRGConfigFileForSettingsPath(" \t "); err == nil {
		t.Fatal("expected empty settings path error")
	}
}

func TestLoadHonorsHOMEEnvironmentForDefaultConfigRoot(t *testing.T) {
	home, workspace := newConfigTestEnv(t)

	cfg := loadConfigTestApp(t, workspace, LoadOptions{})
	if cfg.PersistenceRoot != filepath.Join(home, brand.ConfigDirName) {
		t.Fatalf("persistence root = %q, want HOME-scoped root", cfg.PersistenceRoot)
	}
	if cfg.Source.HomeSettingsPath != filepath.Join(home, brand.ConfigDirName, "config.toml") {
		t.Fatalf("home settings path = %q, want HOME-scoped config", cfg.Source.HomeSettingsPath)
	}
}

func TestLoadTrimsWorkspaceRootBeforeResolving(t *testing.T) {
	_, workspace := newConfigTestEnv(t)

	cfg, err := Load("  "+workspace+"  ", LoadOptions{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.WorkspaceRoot != workspace {
		t.Fatalf("workspace root = %q, want %q", cfg.WorkspaceRoot, workspace)
	}
}

func TestLoadAppliesWorkspaceConfigBeforeEnvBeforeCLI(t *testing.T) {
	home, workspace := newConfigTestEnv(t)
	t.Setenv("KENT_MODEL", "env-model")
	if err := os.MkdirAll(filepath.Join(home, brand.ConfigDirName), 0o755); err != nil {
		t.Fatalf("create home config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, brand.ConfigDirName, "config.toml"), []byte("model = \"home-model\"\n"), 0o644); err != nil {
		t.Fatalf("write home config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, brand.ConfigDirName), 0o755); err != nil {
		t.Fatalf("create workspace config dir: %v", err)
	}
	workspaceConfigPath := filepath.Join(workspace, brand.ConfigDirName, "config.toml")
	if err := os.WriteFile(workspaceConfigPath, []byte("model = \"workspace-model\"\nthinking_level = \"high\"\n"), 0o644); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}

	cfg, err := Load(workspace, LoadOptions{ThinkingLevel: "low"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Settings.Model != "env-model" {
		t.Fatalf("model = %q, want env-model", cfg.Settings.Model)
	}
	if cfg.Settings.ThinkingLevel != "low" {
		t.Fatalf("thinking level = %q, want cli override", cfg.Settings.ThinkingLevel)
	}
	if cfg.Source.SettingsPath != workspaceConfigPath || !cfg.Source.WorkspaceSettingsFileExists {
		t.Fatalf("unexpected workspace source report: %+v", cfg.Source)
	}
	if cfg.Source.Sources["model"] != "env" || cfg.Source.Sources["thinking_level"] != "cli" {
		t.Fatalf("unexpected sources: %+v", cfg.Source.Sources)
	}
}

func TestLoadGlobalSkipsWorkspaceConfigLayer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KENT_MODEL", "env-model")
	if err := os.MkdirAll(filepath.Join(home, brand.ConfigDirName), 0o755); err != nil {
		t.Fatalf("create home config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, brand.ConfigDirName, "config.toml"), []byte("model = \"home-model\"\n"), 0o644); err != nil {
		t.Fatalf("write home config: %v", err)
	}

	cfg, err := LoadGlobal(LoadOptions{})
	if err != nil {
		t.Fatalf("load global: %v", err)
	}
	if cfg.WorkspaceRoot != "" {
		t.Fatalf("workspace root = %q, want empty", cfg.WorkspaceRoot)
	}
	if cfg.Settings.Model != "env-model" {
		t.Fatalf("model = %q, want env-model", cfg.Settings.Model)
	}
	if cfg.Source.WorkspaceSettingsLayerEnabled || cfg.Source.WorkspaceSettingsPath != "" {
		t.Fatalf("unexpected workspace source report: %+v", cfg.Source)
	}
}

func TestEnsureManagedRGConfigFilePreservesExistingContents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := ResolveManagedRGConfigPath()
	if err != nil {
		t.Fatalf("resolve managed rg config path: %v", err)
	}
	if err := ensureSettingsDir(path); err != nil {
		t.Fatalf("ensure settings dir: %v", err)
	}
	const existing = "--max-columns=80\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatalf("write existing managed rg config: %v", err)
	}

	createdPath, created, err := EnsureManagedRGConfigFile()
	if err != nil {
		t.Fatalf("ensure managed rg config file: %v", err)
	}
	if created {
		t.Fatal("expected existing managed rg config not to be replaced")
	}
	if createdPath != path {
		t.Fatalf("managed rg config path = %q, want %q", createdPath, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read managed rg config: %v", err)
	}
	if string(data) != existing {
		t.Fatalf("managed rg config contents = %q, want %q", string(data), existing)
	}
}

func TestLoadSubagentRoleFromFile(t *testing.T) {
	home, workspace, configPath := newConfigTestFile(t)
	contents := strings.Join([]string{
		"model = \"gpt-5.5\"",
		"",
		"[subagents.fast]",
		"model = \"gpt-5.4-mini\"",
		"thinking_level = \"low\"",
		"",
		"[subagents.fast.reviewer]",
		"system_prompt_file = \"fast-reviewer.md\"",
		"",
		"[subagents.fast.tools]",
		"patch = false",
	}, "\n")
	writeConfigTestFile(t, configPath, contents)

	cfg := loadConfigTestApp(t, workspace, LoadOptions{})
	role, ok := cfg.Settings.Subagents[BuiltInSubagentRoleFast]
	if !ok {
		t.Fatalf("expected fast subagent role, got %+v", cfg.Settings.Subagents)
	}
	if role.Settings.Model != "gpt-5.4-mini" {
		t.Fatalf("role model = %q, want gpt-5.4-mini", role.Settings.Model)
	}
	if role.Settings.ThinkingLevel != "low" {
		t.Fatalf("role thinking = %q, want low", role.Settings.ThinkingLevel)
	}
	if role.Settings.EnabledTools[toolspec.ToolPatch] {
		t.Fatalf("expected fast role patch tool disabled, got %+v", role.Settings.EnabledTools)
	}
	if want := filepath.Join(home, brand.ConfigDirName, "fast-reviewer.md"); role.Settings.Reviewer.SystemPromptFile != want {
		t.Fatalf("role reviewer system prompt file = %q, want %q", role.Settings.Reviewer.SystemPromptFile, want)
	}
	if role.Sources["model"] != "file" || role.Sources["thinking_level"] != "file" || role.Sources["tools.patch"] != "file" || role.Sources["reviewer.system_prompt_file"] != "file" {
		t.Fatalf("unexpected role sources: %+v", role.Sources)
	}
	if _, exists := role.Sources["reviewer.model"]; exists {
		t.Fatalf("did not expect inherited reviewer model to be marked explicit, got %+v", role.Sources)
	}
}

func TestLoadSubagentRoleMetadataFromFile(t *testing.T) {
	_, workspace, configPath := newConfigTestFile(t)
	contents := strings.Join([]string{
		"[subagents.research]",
		"description = \"  Deep    repo\\nresearch  \"",
		"agent_callable = false",
		"thinking_level = \"high\"",
	}, "\n")
	writeConfigTestFile(t, configPath, contents)

	cfg := loadConfigTestApp(t, workspace, LoadOptions{})
	role := cfg.Settings.Subagents["research"]
	if role.Description != "Deep repo research" {
		t.Fatalf("description = %q, want normalized description", role.Description)
	}
	if role.AgentCallable || !role.AgentCallableSet {
		t.Fatalf("agent callable metadata = (%t, %t), want false set", role.AgentCallable, role.AgentCallableSet)
	}
	if _, exists := role.Sources["description"]; exists {
		t.Fatalf("description should not be runtime source, got %+v", role.Sources)
	}
	if _, exists := role.Sources["agent_callable"]; exists {
		t.Fatalf("agent_callable should not be runtime source, got %+v", role.Sources)
	}
}

func TestLoadSubagentRoleRejectsReservedNames(t *testing.T) {
	for _, reserved := range []string{"default", "none", "self"} {
		t.Run(reserved, func(t *testing.T) {
			err := loadConfigTestFileError(t, "[subagents."+reserved+"]\nmodel = \"gpt-5.5\"\n", LoadOptions{})
			if err == nil {
				t.Fatal("expected reserved role to fail")
			}
			if !strings.Contains(err.Error(), "invalid subagents key") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoadSubagentRoleRejectsInvalidMetadata(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "description type", body: "[subagents.worker]\ndescription = 123\n", wantErr: "expected string"},
		{name: "agent callable type", body: "[subagents.worker]\nagent_callable = \"no\"\n", wantErr: "expected boolean"},
		{name: "description length", body: "[subagents.worker]\ndescription = \"" + strings.Repeat("x", MaxSubagentDescriptionChars+1) + "\"\n", wantErr: "description must be <="},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := loadConfigTestFileError(t, tt.body, LoadOptions{})
			if err == nil {
				t.Fatal("expected metadata error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestSubagentRoleHasMeaningfulDiffComparesProviderReviewerAndTimeoutValues(t *testing.T) {
	base := Settings{
		Timeouts: Timeouts{ModelRequestSeconds: 100},
		ProviderCapabilities: ProviderCapabilitiesOverride{
			ProviderID:           "openai",
			SupportsResponsesAPI: true,
		},
		Reviewer: ReviewerSettings{
			Model:          "gpt-5.5",
			TimeoutSeconds: 60,
		},
	}

	same := SubagentRole{
		Settings: base,
		Sources: map[string]string{
			"timeouts.model_request_seconds":                        "file",
			"provider_capabilities.supports_responses_api":          "file",
			"reviewer.model":                                        "file",
			"reviewer.provider_capabilities.supports_responses_api": "file",
		},
	}
	if SubagentRoleHasMeaningfulDiff(base, same) {
		t.Fatal("expected equal provider/reviewer/timeout values to be no-op")
	}

	changedTimeout := same
	changedTimeout.Settings = base
	changedTimeout.Settings.Timeouts.ModelRequestSeconds = 200
	if !SubagentRoleHasMeaningfulDiff(base, changedTimeout) {
		t.Fatal("expected timeout change to be meaningful")
	}

	changedProvider := same
	changedProvider.Settings = base
	changedProvider.Settings.ProviderCapabilities.SupportsResponsesAPI = false
	if !SubagentRoleHasMeaningfulDiff(base, changedProvider) {
		t.Fatal("expected provider capability change to be meaningful")
	}

	changedReviewer := same
	changedReviewer.Settings = base
	changedReviewer.Settings.Reviewer.Model = "gpt-5.4-mini"
	if !SubagentRoleHasMeaningfulDiff(base, changedReviewer) {
		t.Fatal("expected reviewer change to be meaningful")
	}
}

func TestAppendSystemPromptFileFromConfigResolvesConfigRelativePath(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), brand.ConfigDirName, "config.toml")
	state := configRegistry.defaultState()

	if err := appendSystemPromptFileFromConfig(
		settingsFile{"system_prompt_file": "prompts/SYSTEM.md"},
		configPath,
		SystemPromptFileScopeWorkspaceConfig,
		&state,
	); err != nil {
		t.Fatalf("append system prompt file: %v", err)
	}

	want := filepath.Join(filepath.Dir(configPath), "prompts", "SYSTEM.md")
	if got := state.Settings.SystemPromptFiles; len(got) != 1 || got[0].Path != want || got[0].Scope != SystemPromptFileScopeWorkspaceConfig {
		t.Fatalf("system prompt files = %+v, want %q %s", got, want, SystemPromptFileScopeWorkspaceConfig)
	}
}

func TestParseSubagentRoleSystemPromptFileResolvesConfigRelativePath(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), brand.ConfigDirName, "config.toml")

	role, err := parseSubagentRole(settingsFile{"system_prompt_file": "fast-system.md"}, configPath, "fast")
	if err != nil {
		t.Fatalf("parse subagent role: %v", err)
	}

	want := filepath.Join(filepath.Dir(configPath), "fast-system.md")
	if got := role.Settings.SystemPromptFiles; len(got) != 1 || got[0].Path != want || got[0].Scope != SystemPromptFileScopeSubagent {
		t.Fatalf("subagent system prompt files = %+v, want %q %s", got, want, SystemPromptFileScopeSubagent)
	}
	if role.Sources["system_prompt_file"] != "file" {
		t.Fatalf("system_prompt_file source = %q, want file", role.Sources["system_prompt_file"])
	}
}

func TestLoadSubagentRoleRejectsNestedSubagentsTable(t *testing.T) {
	contents := strings.Join([]string{
		"model = \"gpt-5.5\"",
		"",
		"[subagents.fast]",
		"thinking_level = \"low\"",
		"",
		"[subagents.fast.subagents.worker]",
		"thinking_level = \"high\"",
	}, "\n")

	err := loadConfigTestFileError(t, contents, LoadOptions{})
	if err == nil {
		t.Fatal("expected nested subagents table to fail")
	}
	if !strings.Contains(err.Error(), "unknown settings key(s): subagents.fast.subagents") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadSubagentRoleRejectsUnknownKeys(t *testing.T) {
	contents := strings.Join([]string{
		"model = \"gpt-5.5\"",
		"",
		"[subagents.fast]",
		"thinking_level = \"low\"",
		"unknown_toggle = true",
	}, "\n")

	err := loadConfigTestFileError(t, contents, LoadOptions{})
	if err == nil {
		t.Fatal("expected unknown subagent key to fail")
	}
	if !strings.Contains(err.Error(), "unknown settings key(s): subagents.fast.unknown_toggle") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadResolvesWorktreeBaseDirRelativeToPersistenceRoot(t *testing.T) {
	home, workspace, configPath := newConfigTestFile(t)
	configText := strings.Join([]string{
		"persistence_root = \"~/custom-kent\"",
		"",
		"[worktrees]",
		"base_dir = \"managed/worktrees\"",
		"setup_script = \"scripts/setup-worktree.sh\"",
		"",
	}, "\n")
	writeConfigTestFile(t, configPath, configText)

	cfg := loadConfigTestApp(t, workspace, LoadOptions{})

	if got, want := cfg.PersistenceRoot, filepath.Join(home, "custom-kent"); got != want {
		t.Fatalf("persistence root = %q, want %q", got, want)
	}
	if got, want := cfg.Settings.Worktrees.BaseDir, filepath.Join(cfg.PersistenceRoot, "managed", "worktrees"); got != want {
		t.Fatalf("worktrees.base_dir = %q, want %q", got, want)
	}
	if got := cfg.Settings.Worktrees.SetupScript; got != "scripts/setup-worktree.sh" {
		t.Fatalf("worktrees.setup_script = %q, want scripts/setup-worktree.sh", got)
	}
}

func TestLoadDerivesDefaultWorktreeBaseDirFromPersistenceRoot(t *testing.T) {
	configText := "persistence_root = \"~/custom-kent\"\n"
	home, _, cfg := loadConfigTestFileApp(t, configText, LoadOptions{})

	if got, want := cfg.PersistenceRoot, filepath.Join(home, "custom-kent"); got != want {
		t.Fatalf("persistence root = %q, want %q", got, want)
	}
	if got, want := cfg.Settings.Worktrees.BaseDir, filepath.Join(cfg.PersistenceRoot, "worktrees"); got != want {
		t.Fatalf("worktrees.base_dir = %q, want %q", got, want)
	}
}

func TestLoadCreatesWorktreeBaseDir(t *testing.T) {
	configText := strings.Join([]string{
		"persistence_root = \"~/custom-kent\"",
		"",
		"[worktrees]",
		"base_dir = \"managed/worktrees\"",
	}, "\n")
	_, _, cfg := loadConfigTestFileApp(t, configText, LoadOptions{})
	info, err := os.Stat(cfg.Settings.Worktrees.BaseDir)
	if err != nil {
		t.Fatalf("stat worktree base dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected worktree base dir, got mode %v", info.Mode())
	}
}

func TestLoadSubagentRoleRejectsInvalidValues(t *testing.T) {
	contents := strings.Join([]string{
		"model = \"gpt-5.5\"",
		"",
		"[subagents.fast]",
		"provider_override = \"bogus\"",
	}, "\n")

	err := loadConfigTestFileError(t, contents, LoadOptions{})
	if err == nil {
		t.Fatal("expected invalid subagent role values to fail")
	}
	if !strings.Contains(err.Error(), "invalid subagents.fast") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadSubagentRoleAllowsReviewerAuthNoneToInheritParentBaseURL(t *testing.T) {
	contents := strings.Join([]string{
		"model = \"gpt-5.5\"",
		"openai_base_url = \"http://127.0.0.1:8080/v1\"",
		"",
		"[subagents.fast.reviewer]",
		"auth = \"none\"",
	}, "\n")

	_, _, cfg := loadConfigTestFileApp(t, contents, LoadOptions{})
	role := cfg.Settings.Subagents[BuiltInSubagentRoleFast]
	if role.Settings.Reviewer.Auth != "none" {
		t.Fatalf("expected subagent reviewer.auth=none, got %q", role.Settings.Reviewer.Auth)
	}
}

func TestLoadSubagentRoleAllowsReviewerAuthNoneWithExplicitFirstPartyBaseURL(t *testing.T) {
	contents := strings.Join([]string{
		"model = \"gpt-5.5\"",
		"",
		"[subagents.fast.reviewer]",
		"auth = \"none\"",
		"openai_base_url = \"https://api.openai.com/v1\"",
	}, "\n")

	_, _, cfg := loadConfigTestFileApp(t, contents, LoadOptions{})
	role := cfg.Settings.Subagents[BuiltInSubagentRoleFast]
	if role.Settings.Reviewer.Auth != "none" {
		t.Fatalf("expected subagent reviewer.auth=none, got %q", role.Settings.Reviewer.Auth)
	}
}

func TestLoadSubagentRoleRejectsPersistenceRoot(t *testing.T) {
	contents := strings.Join([]string{
		"model = \"gpt-5.5\"",
		"",
		"[subagents.fast]",
		"persistence_root = \"/tmp/custom\"",
	}, "\n")

	err := loadConfigTestFileError(t, contents, LoadOptions{})
	if err == nil {
		t.Fatal("expected persistence_root in subagent role to fail")
	}
	if !strings.Contains(err.Error(), "persistence_root is not supported in subagent roles") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSettingsTOMLRoundTripsCapabilityOverrides(t *testing.T) {
	settings := configRegistry.defaultState().Settings
	settings.ModelCapabilities.SupportsReasoningEffort = true
	settings.ProviderCapabilities = ProviderCapabilitiesOverride{
		ProviderID:                     "openai-compatible",
		SupportsResponsesAPI:           true,
		SupportsRequestInputTokenCount: true,
		SupportsPromptCacheKey:         true,
		SupportsServerSideContextEdit:  true,
	}
	toml := settingsTOMLWithRenderingOptions(settings, true, nil, nil)

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(toml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	raw, err := readSettingsFile(path)
	if err != nil {
		t.Fatalf("read settings file: %v", err)
	}
	state := configRegistry.defaultState()
	sources := configRegistry.defaultSourceMap()
	if err := configRegistry.applyFile(raw, path, &state, sources); err != nil {
		t.Fatalf("apply file: %v", err)
	}
	if !state.Settings.ModelCapabilities.SupportsReasoningEffort {
		t.Fatal("expected model capability override to round-trip")
	}
	if state.Settings.ProviderCapabilities.ProviderID != "openai-compatible" {
		t.Fatalf("expected provider_id to round-trip, got %q", state.Settings.ProviderCapabilities.ProviderID)
	}
	if !state.Settings.ProviderCapabilities.SupportsResponsesAPI {
		t.Fatal("expected supports_responses_api to round-trip")
	}
	if !state.Settings.ProviderCapabilities.SupportsRequestInputTokenCount {
		t.Fatal("expected supports_request_input_token_count to round-trip")
	}
	if !state.Settings.ProviderCapabilities.SupportsServerSideContextEdit {
		t.Fatal("expected supports_server_side_context_edit to round-trip")
	}
}

func TestWriteSettingsFileForOnboardingDoesNotOverwriteExistingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, brand.ConfigDirName, "config.toml")
	writeConfigTestFile(t, configPath, "model = \"existing\"\n")
	_, err := WriteSettingsFileForOnboarding(configRegistry.defaultState().Settings)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected existing settings file error, got %v", err)
	}
	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read settings file: %v", err)
	}
	if string(contents) != "model = \"existing\"\n" {
		t.Fatalf("expected existing settings file contents to remain unchanged, got %q", string(contents))
	}
}

func TestValidateThemeAllowsAutoAndEmpty(t *testing.T) {
	for _, value := range []string{"", "auto", "light", "dark"} {
		if err := validateTheme(settingsState{Settings: Settings{Theme: value}}, nil); err != nil {
			t.Fatalf("validate theme %q: %v", value, err)
		}
	}
}

func TestLoadReviewerDefaultsInheritMainSettingsWhenUnset(t *testing.T) {
	home, workspace := newConfigTestEnv(t)

	configPath := filepath.Join(home, brand.ConfigDirName, "config.toml")
	writeConfigTestFile(t, configPath, "model = \"gpt-main-file\"\nthinking_level = \"xhigh\"\nprovider_override = \"openai\"\nopenai_base_url = \"http://127.0.0.1:8080/v1\"\n[reviewer]\nfrequency = \"all\"\n")

	cfg := loadConfigTestApp(t, workspace, LoadOptions{})
	if cfg.Settings.Reviewer.Model != "gpt-main-file" {
		t.Fatalf("expected reviewer.model to inherit file main model, got %q", cfg.Settings.Reviewer.Model)
	}
	if cfg.Settings.Reviewer.ThinkingLevel != "xhigh" {
		t.Fatalf("expected reviewer.thinking_level to inherit file main thinking level, got %q", cfg.Settings.Reviewer.ThinkingLevel)
	}
	if cfg.Settings.Reviewer.ProviderOverride != "openai" {
		t.Fatalf("expected reviewer.provider_override to inherit file main provider override, got %q", cfg.Settings.Reviewer.ProviderOverride)
	}
	if cfg.Settings.Reviewer.OpenAIBaseURL != "http://127.0.0.1:8080/v1" {
		t.Fatalf("expected reviewer.openai_base_url to inherit file main base URL, got %q", cfg.Settings.Reviewer.OpenAIBaseURL)
	}

	t.Setenv("KENT_MODEL", "gpt-main-env")
	t.Setenv("KENT_THINKING_LEVEL", "medium")
	t.Setenv("KENT_OPENAI_BASE_URL", "http://localhost:9090/v1")
	t.Setenv("KENT_REVIEWER_MODEL", "")
	t.Setenv("KENT_REVIEWER_THINKING_LEVEL", "")
	t.Setenv("KENT_REVIEWER_PROVIDER_OVERRIDE", "")
	t.Setenv("KENT_REVIEWER_OPENAI_BASE_URL", "")
	cfg = loadConfigTestApp(t, workspace, LoadOptions{})
	if cfg.Settings.Reviewer.Model != "gpt-main-env" {
		t.Fatalf("expected reviewer.model to inherit env main model, got %q", cfg.Settings.Reviewer.Model)
	}
	if cfg.Settings.Reviewer.ThinkingLevel != "medium" {
		t.Fatalf("expected reviewer.thinking_level to inherit env main thinking level, got %q", cfg.Settings.Reviewer.ThinkingLevel)
	}
	if cfg.Settings.Reviewer.OpenAIBaseURL != "http://localhost:9090/v1" {
		t.Fatalf("expected reviewer.openai_base_url to inherit env main base URL, got %q", cfg.Settings.Reviewer.OpenAIBaseURL)
	}
}

func TestLoadReviewerOpenAIProviderOverrideInheritsMainOpenAIBaseURL(t *testing.T) {
	home, workspace := newConfigTestEnv(t)

	configPath := filepath.Join(home, brand.ConfigDirName, "config.toml")
	writeConfigTestFile(t, configPath, "openai_base_url = \"http://127.0.0.1:8080/v1\"\n[reviewer]\nprovider_override = \"openai\"\nmodel = \"local-reviewer\"\n")

	cfg := loadConfigTestApp(t, workspace, LoadOptions{})
	if cfg.Settings.Reviewer.ProviderOverride != "openai" {
		t.Fatalf("expected explicit reviewer.provider_override, got %q", cfg.Settings.Reviewer.ProviderOverride)
	}
	if cfg.Settings.Reviewer.OpenAIBaseURL != "http://127.0.0.1:8080/v1" {
		t.Fatalf("expected reviewer.openai_base_url to inherit main OpenAI base URL, got %q", cfg.Settings.Reviewer.OpenAIBaseURL)
	}
}
