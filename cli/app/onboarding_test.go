package app

import (
	"context"

	"builder/prompts"
	"builder/server/generated"
	"builder/server/runtime"
	"builder/shared/config"
	"builder/shared/theme"
	"builder/shared/toolspec"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeOnboardingTestSkill(t *testing.T, dir string, name string, description string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: "+description+"\n---\n"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
}

func TestSkillSelectionCandidatesAnnotateOpponentSource(t *testing.T) {
	state := &onboardingFlowState{
		imports: onboardingImportDiscovery{skillSymlinkItems: map[onboardingImportProviderID][]onboardingSkillImportItem{
			onboardingImportProviderCodex: {
				{ID: "codex:skill", Provider: onboardingImportProviderCodex, ProviderLabel: "Codex", TargetDirName: "skill-creator", SkillName: "skill-creator", SourceDir: "/tmp/codex/skill-creator", ModifiedAt: time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)},
				{ID: "codex:skill-copy", Provider: onboardingImportProviderCodex, ProviderLabel: "Codex", TargetDirName: "skill-creator", SkillName: "skill-creator", SourceDir: "/tmp/codex/skill-creator-copy", ModifiedAt: time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC)},
			},
		}},
		skillImport: onboardingImportSelection{Mode: onboardingImportModeSymlinkSource, Provider: onboardingImportProviderCodex},
	}
	items := skillSelectionCandidates(state)
	if len(items) != 2 {
		t.Fatalf("expected both duplicate symlink candidates to remain visible, got %d", len(items))
	}
	for _, item := range items {
		if item.DuplicateSourceNote != "skill-creator-copy" && item.DuplicateSourceNote != "skill-creator" {
			t.Fatalf("expected duplicate note to mention the sibling source, got %q", item.DuplicateSourceNote)
		}
	}
}

func TestDiscoverOnboardingImportsSkipsExistingTargets(t *testing.T) {
	home := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(globalRoot, "skills", "existing-skill"), 0o755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(globalRoot, "commands"), 0o755); err != nil {
		t.Fatalf("mkdir commands: %v", err)
	}
	if err := os.WriteFile(filepath.Join(globalRoot, "commands", "demo.md"), []byte("demo"), 0o644); err != nil {
		t.Fatalf("write command: %v", err)
	}
	discovery := discoverOnboardingImports(globalRoot)
	if discovery.err != nil {
		t.Fatalf("discover imports: %v", discovery.err)
	}
	if !discovery.skipSkills {
		t.Fatal("expected skills import flow to be skipped when skills root already exists")
	}
	if !discovery.skipCommands {
		t.Fatal("expected command import flow to be skipped when commands root already exists")
	}
}

func TestDiscoverOnboardingImportsIncludesAgentsSkillsAndCommands(t *testing.T) {
	home := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("HOME", home)
	agentsSkillsDir := filepath.Join(home, ".agents", "skills")
	writeOnboardingTestSkill(t, filepath.Join(agentsSkillsDir, "demo-skill"), "demo", "from agents")
	agentsCommandsDir := filepath.Join(home, ".agents", "commands")
	if err := os.MkdirAll(agentsCommandsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents commands: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentsCommandsDir, "review.md"), []byte("review"), 0o644); err != nil {
		t.Fatalf("write agents command: %v", err)
	}

	discovery := discoverOnboardingImports(globalRoot)
	if discovery.err != nil {
		t.Fatalf("discover imports: %v", discovery.err)
	}
	if got := discovery.skillSymlinkRoots[onboardingImportProviderAgents]; got != agentsSkillsDir {
		t.Fatalf("expected agents skill root %q, got %q", agentsSkillsDir, got)
	}
	items := discovery.skillSymlinkItems[onboardingImportProviderAgents]
	if len(items) != 1 {
		t.Fatalf("expected one agents skill import candidate, got %+v", items)
	}
	if items[0].ProviderLabel != "Agents" || items[0].TargetDirName != "demo-skill" {
		t.Fatalf("unexpected agents skill candidate: %+v", items[0])
	}
	if got := discovery.commandSymlinkRoots[onboardingImportProviderAgents]; got != agentsCommandsDir {
		t.Fatalf("expected agents command root %q, got %q", agentsCommandsDir, got)
	}
	commandItems := discovery.commandSymlinkItems[onboardingImportProviderAgents]
	if len(commandItems) != 1 {
		t.Fatalf("expected one agents slash command import candidate, got %+v", commandItems)
	}
	if commandItems[0].ProviderLabel != "Agents" || commandItems[0].TargetFileName != "review.md" {
		t.Fatalf("unexpected agents slash command candidate: %+v", commandItems[0])
	}
}

func TestDiscoverProviderCommandSymlinkItemsPreferRootCommandsDirectory(t *testing.T) {
	base := t.TempDir()
	commandsDir := filepath.Join(base, "commands")
	nestedPluginPrompts := filepath.Join(base, "plugins", "sample", "prompts")
	if err := os.MkdirAll(commandsDir, 0o755); err != nil {
		t.Fatalf("mkdir commands: %v", err)
	}
	if err := os.MkdirAll(nestedPluginPrompts, 0o755); err != nil {
		t.Fatalf("mkdir nested prompts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(commandsDir, "review.md"), []byte("commands"), 0o644); err != nil {
		t.Fatalf("write root command: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nestedPluginPrompts, "plugin.md"), []byte("plugin"), 0o644); err != nil {
		t.Fatalf("write nested plugin prompt: %v", err)
	}
	root, items, err := discoverProviderCommandSymlinkItems(onboardingImportProvider{ID: onboardingImportProviderClaudeCode, Label: "Claude Code"}, base)
	if err != nil {
		t.Fatalf("discover provider command symlink items: %v", err)
	}
	if root != commandsDir {
		t.Fatalf("expected command symlink root %q, got %q", commandsDir, root)
	}
	if len(items) != 1 {
		t.Fatalf("expected only direct root commands to be symlinkable, got %+v", items)
	}
	if items[0].TargetFileName != "review.md" {
		t.Fatalf("expected review.md to be symlinked, got %+v", items[0])
	}
}

func TestDiscoverProviderCommandSymlinkItemsFallBackToPromptsWhenCommandsHasNoDirectMarkdown(t *testing.T) {
	base := t.TempDir()
	commandsDir := filepath.Join(base, "commands")
	promptsDir := filepath.Join(base, "prompts")
	if err := os.MkdirAll(filepath.Join(commandsDir, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested commands: %v", err)
	}
	if err := os.MkdirAll(promptsDir, 0o755); err != nil {
		t.Fatalf("mkdir prompts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(commandsDir, "nested", "ignored.md"), []byte("ignored"), 0o644); err != nil {
		t.Fatalf("write nested command: %v", err)
	}
	if err := os.WriteFile(filepath.Join(promptsDir, "review.md"), []byte("prompts"), 0o644); err != nil {
		t.Fatalf("write prompt command: %v", err)
	}
	root, items, err := discoverProviderCommandSymlinkItems(onboardingImportProvider{ID: onboardingImportProviderClaudeCode, Label: "Claude Code"}, base)
	if err != nil {
		t.Fatalf("discover provider command symlink items: %v", err)
	}
	if root != promptsDir {
		t.Fatalf("expected prompt symlink root %q, got %q", promptsDir, root)
	}
	if len(items) != 1 {
		t.Fatalf("expected prompts fallback to expose one direct command, got %+v", items)
	}
	if items[0].TargetFileName != "review.md" {
		t.Fatalf("expected review.md prompt command to be symlinked, got %+v", items[0])
	}
}

func TestExecuteSkillImportSymlinksRootDirectory(t *testing.T) {
	home := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("HOME", home)
	sourceDir := filepath.Join(home, ".codex", "skills", "local")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if _, err := executeSkillImport(globalRoot, onboardingImportDiscovery{}, onboardingImportSelection{Mode: onboardingImportModeSymlinkSource, Provider: onboardingImportProviderCodex}); err != nil {
		t.Fatalf("execute skill import: %v", err)
	}
	targetPath := filepath.Join(globalRoot, "skills")
	info, err := os.Lstat(targetPath)
	if err != nil {
		t.Fatalf("lstat target: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected %s to be a symlink", targetPath)
	}
	resolved, err := os.Readlink(targetPath)
	if err != nil {
		t.Fatalf("readlink target: %v", err)
	}
	if resolved != sourceDir {
		t.Fatalf("expected skills root symlink to point to %q, got %q", sourceDir, resolved)
	}
}

func TestExecuteSkillImportSymlinksAgentsRootDirectory(t *testing.T) {
	home := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("HOME", home)
	sourceDir := filepath.Join(home, ".agents", "skills")
	writeOnboardingTestSkill(t, filepath.Join(sourceDir, "demo-skill"), "demo", "from agents")
	if _, err := executeSkillImport(globalRoot, onboardingImportDiscovery{}, onboardingImportSelection{Mode: onboardingImportModeSymlinkSource, Provider: onboardingImportProviderAgents}); err != nil {
		t.Fatalf("execute skill import: %v", err)
	}
	targetPath := filepath.Join(globalRoot, "skills")
	resolved, err := os.Readlink(targetPath)
	if err != nil {
		t.Fatalf("readlink target: %v", err)
	}
	if resolved != sourceDir {
		t.Fatalf("expected agents skills root symlink to point to %q, got %q", sourceDir, resolved)
	}
}

func TestExecuteSkillImportReplacesEmptyTargetDirectory(t *testing.T) {
	home := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("HOME", home)
	sourceDir := filepath.Join(home, ".codex", "skills", "local")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	targetPath := filepath.Join(globalRoot, "skills")
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatalf("mkdir empty target: %v", err)
	}

	if _, err := executeSkillImport(globalRoot, onboardingImportDiscovery{}, onboardingImportSelection{Mode: onboardingImportModeSymlinkSource, Provider: onboardingImportProviderCodex}); err != nil {
		t.Fatalf("execute skill import with empty target: %v", err)
	}
	info, err := os.Lstat(targetPath)
	if err != nil {
		t.Fatalf("lstat target: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected %s to be replaced with a symlink", targetPath)
	}
}

func TestExecuteSkillImportDoesNotDeleteEmptyTargetWhenSourceValidationFails(t *testing.T) {
	globalRoot := t.TempDir()
	targetPath := filepath.Join(globalRoot, "skills")
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatalf("mkdir empty target: %v", err)
	}

	_, err := executeSkillImport(globalRoot, onboardingImportDiscovery{
		skillSymlinkRoots: map[onboardingImportProviderID]string{onboardingImportProviderCodex: filepath.Join(t.TempDir(), "missing-skills")},
	}, onboardingImportSelection{Mode: onboardingImportModeSymlinkSource, Provider: onboardingImportProviderCodex})
	if err == nil {
		t.Fatal("expected missing skill source to fail")
	}
	info, statErr := os.Lstat(targetPath)
	if statErr != nil {
		t.Fatalf("expected empty target directory to remain after source validation failure: %v", statErr)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected %s to remain a plain directory, got mode %v", targetPath, info.Mode())
	}
}

func TestProviderSkillSymlinkSourcePrefersCodexLocalSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".codex", "skills", "local"), 0o755); err != nil {
		t.Fatalf("mkdir codex local skills: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex", "skills", ".system"), 0o755); err != nil {
		t.Fatalf("mkdir codex system skills: %v", err)
	}
	resolved, err := providerSkillSymlinkSource(onboardingImportProviderCodex)
	if err != nil {
		t.Fatalf("provider skill symlink source: %v", err)
	}
	expected := filepath.Join(home, ".codex", "skills", "local")
	if resolved != expected {
		t.Fatalf("expected codex skill symlink source %q, got %q", expected, resolved)
	}
}

func TestDiscoverProviderSkillSymlinkItemsFallsBackWhenPreferredDirectoryIsEmpty(t *testing.T) {
	home := t.TempDir()
	provider, ok := onboardingImportProviderByID(onboardingImportProviderCodex)
	if !ok {
		t.Fatal("expected codex provider")
	}
	base := filepath.Join(home, ".codex")
	if err := os.MkdirAll(filepath.Join(base, "skills", "local"), 0o755); err != nil {
		t.Fatalf("mkdir local skills: %v", err)
	}
	writeOnboardingTestSkill(t, filepath.Join(base, "skills", "fallback-skill"), "fallback", "from skills root")

	root, items, err := discoverProviderSkillSymlinkItems(provider, base)
	if err != nil {
		t.Fatalf("discoverProviderSkillSymlinkItems: %v", err)
	}
	expectedRoot := filepath.Join(base, "skills")
	if root != expectedRoot {
		t.Fatalf("root = %q, want %q", root, expectedRoot)
	}
	if len(items) != 1 || items[0].TargetDirName != "fallback-skill" {
		t.Fatalf("unexpected discovered items: %+v", items)
	}
}

func TestProviderSkillSymlinkSourceErrorsWithoutSkillsRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir provider home: %v", err)
	}
	_, err := providerSkillSymlinkSource(onboardingImportProviderClaudeCode)
	if err == nil {
		t.Fatal("expected missing skills root to fail")
	}
	if !strings.Contains(err.Error(), "no skills directory found") {
		t.Fatalf("expected missing skills root error, got %v", err)
	}
}

func TestApplyImportChoiceRejectsRemovedCopyModes(t *testing.T) {
	selection := onboardingImportSelection{}
	if err := applyImportChoice(&selection, "copy:claude_code"); err == nil {
		t.Fatal("expected removed copy mode to be rejected")
	}
	if err := applyImportChoice(&selection, "merge"); err == nil {
		t.Fatal("expected removed merge mode to be rejected")
	}
}

func TestExecuteCommandImportSymlinksRootDirectory(t *testing.T) {
	home := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("HOME", home)
	sourceDir := filepath.Join(home, ".claude", "commands")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "review.md"), []byte("review"), 0o644); err != nil {
		t.Fatalf("write source command: %v", err)
	}
	if _, err := executeCommandImport(globalRoot, onboardingImportDiscovery{}, onboardingImportSelection{Mode: onboardingImportModeSymlinkSource, Provider: onboardingImportProviderClaudeCode}); err != nil {
		t.Fatalf("execute command import: %v", err)
	}
	targetPath := filepath.Join(globalRoot, "prompts")
	info, err := os.Lstat(targetPath)
	if err != nil {
		t.Fatalf("lstat target: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected %s to be a symlink", targetPath)
	}
	resolved, err := os.Readlink(targetPath)
	if err != nil {
		t.Fatalf("readlink target: %v", err)
	}
	if resolved != sourceDir {
		t.Fatalf("expected prompts root symlink to point to %q, got %q", sourceDir, resolved)
	}
}

func TestExecuteCommandImportSymlinksAgentsRootDirectory(t *testing.T) {
	home := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("HOME", home)
	sourceDir := filepath.Join(home, ".agents", "commands")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "review.md"), []byte("review"), 0o644); err != nil {
		t.Fatalf("write source command: %v", err)
	}
	if _, err := executeCommandImport(globalRoot, onboardingImportDiscovery{}, onboardingImportSelection{Mode: onboardingImportModeSymlinkSource, Provider: onboardingImportProviderAgents}); err != nil {
		t.Fatalf("execute command import: %v", err)
	}
	targetPath := filepath.Join(globalRoot, "prompts")
	resolved, err := os.Readlink(targetPath)
	if err != nil {
		t.Fatalf("readlink target: %v", err)
	}
	if resolved != sourceDir {
		t.Fatalf("expected agents prompts root symlink to point to %q, got %q", sourceDir, resolved)
	}
}

func TestExecuteCommandImportValidatesSourceDirectory(t *testing.T) {
	globalRoot := t.TempDir()
	missingSource := filepath.Join(t.TempDir(), "missing-prompts")
	_, err := executeCommandImport(globalRoot, onboardingImportDiscovery{
		commandSymlinkRoots: map[onboardingImportProviderID]string{onboardingImportProviderClaudeCode: missingSource},
	}, onboardingImportSelection{Mode: onboardingImportModeSymlinkSource, Provider: onboardingImportProviderClaudeCode})
	if err == nil {
		t.Fatal("expected missing command source to fail")
	}
	if !strings.Contains(err.Error(), "inspect slash command source Claude Code") {
		t.Fatalf("expected source validation error, got %v", err)
	}
}

func TestExecuteCommandImportDoesNotDeleteEmptyTargetWhenSourceValidationFails(t *testing.T) {
	globalRoot := t.TempDir()
	targetPath := filepath.Join(globalRoot, "prompts")
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatalf("mkdir empty target: %v", err)
	}

	_, err := executeCommandImport(globalRoot, onboardingImportDiscovery{
		commandSymlinkRoots: map[onboardingImportProviderID]string{onboardingImportProviderClaudeCode: filepath.Join(t.TempDir(), "missing-prompts")},
	}, onboardingImportSelection{Mode: onboardingImportModeSymlinkSource, Provider: onboardingImportProviderClaudeCode})
	if err == nil {
		t.Fatal("expected missing command source to fail")
	}
	info, statErr := os.Lstat(targetPath)
	if statErr != nil {
		t.Fatalf("expected empty target directory to remain after source validation failure: %v", statErr)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected %s to remain a plain directory, got mode %v", targetPath, info.Mode())
	}
}

func TestExecuteCommandImportFallsBackToPromptsWhenCommandsHasNoDirectMarkdown(t *testing.T) {
	home := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("HOME", home)
	commandsDir := filepath.Join(home, ".claude", "commands")
	promptsDir := filepath.Join(home, ".claude", "prompts")
	if err := os.MkdirAll(filepath.Join(commandsDir, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested commands: %v", err)
	}
	if err := os.MkdirAll(promptsDir, 0o755); err != nil {
		t.Fatalf("mkdir prompts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(commandsDir, "nested", "ignored.md"), []byte("ignored"), 0o644); err != nil {
		t.Fatalf("write nested command: %v", err)
	}
	if err := os.WriteFile(filepath.Join(promptsDir, "review.md"), []byte("prompts"), 0o644); err != nil {
		t.Fatalf("write prompt command: %v", err)
	}

	if _, err := executeCommandImport(globalRoot, onboardingImportDiscovery{}, onboardingImportSelection{Mode: onboardingImportModeSymlinkSource, Provider: onboardingImportProviderClaudeCode}); err != nil {
		t.Fatalf("execute command import: %v", err)
	}
	targetPath := filepath.Join(globalRoot, "prompts")
	resolved, err := os.Readlink(targetPath)
	if err != nil {
		t.Fatalf("readlink target: %v", err)
	}
	if resolved != promptsDir {
		t.Fatalf("expected prompts root symlink to point to %q, got %q", promptsDir, resolved)
	}
}

func TestExecuteCommandImportFallsBackToAgentsPromptsWhenCommandsMissing(t *testing.T) {
	home := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("HOME", home)
	promptsDir := filepath.Join(home, ".agents", "prompts")
	if err := os.MkdirAll(promptsDir, 0o755); err != nil {
		t.Fatalf("mkdir prompts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(promptsDir, "review.md"), []byte("prompts"), 0o644); err != nil {
		t.Fatalf("write prompt command: %v", err)
	}

	if _, err := executeCommandImport(globalRoot, onboardingImportDiscovery{}, onboardingImportSelection{Mode: onboardingImportModeSymlinkSource, Provider: onboardingImportProviderAgents}); err != nil {
		t.Fatalf("execute command import: %v", err)
	}
	targetPath := filepath.Join(globalRoot, "prompts")
	resolved, err := os.Readlink(targetPath)
	if err != nil {
		t.Fatalf("readlink target: %v", err)
	}
	if resolved != promptsDir {
		t.Fatalf("expected prompts root symlink to point to %q, got %q", promptsDir, resolved)
	}
}

func TestExecuteOnboardingImportsTreatsZeroValueModesAsNone(t *testing.T) {
	rollback, err := executeOnboardingImports(t.TempDir(), onboardingFlowState{})
	if err != nil {
		t.Fatalf("execute onboarding imports: %v", err)
	}
	if rollback == nil {
		t.Fatal("expected rollback func")
	}
}

func TestOnboardingModelBackspaceTogglesMultiSelect(t *testing.T) {
	model := newOnboardingModel(t.TempDir(), onboardingFlowState{theme: "dark"})
	model.currentScreen = onboardingScreen{
		ID:        "skills_enabled",
		Kind:      onboardingScreenMulti,
		Title:     "Choose enabled skills",
		Options:   []onboardingOption{{ID: "one", Title: "One"}},
		Selection: map[string]bool{"one": true},
	}
	model.selection = map[string]bool{"one": true}
	model.cursor = 0
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	updated := next.(*onboardingModel)
	if updated.selection["one"] {
		t.Fatal("expected backspace to toggle the current multi-select option off")
	}
}

func TestOnboardingModelCtrlHTogglesMultiSelect(t *testing.T) {
	model := newOnboardingModel(t.TempDir(), onboardingFlowState{theme: "dark"})
	model.currentScreen = onboardingScreen{
		ID:        "skills_enabled",
		Kind:      onboardingScreenMulti,
		Title:     "Choose enabled skills",
		Options:   []onboardingOption{{ID: "one", Title: "One"}},
		Selection: map[string]bool{"one": true},
	}
	model.selection = map[string]bool{"one": true}
	model.cursor = 0
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	updated := next.(*onboardingModel)
	if updated.selection["one"] {
		t.Fatal("expected ctrl+h to toggle the current multi-select option off")
	}
}

func TestBuildSkillSelectionScreenAddsToggleAllOptionWhenThereAreMoreThanTwoItems(t *testing.T) {
	state := &onboardingFlowState{
		imports: onboardingImportDiscovery{skillSymlinkItems: map[onboardingImportProviderID][]onboardingSkillImportItem{
			onboardingImportProviderCodex: {
				{ID: "codex:one", Provider: onboardingImportProviderCodex, ProviderLabel: "Codex", TargetDirName: "one", SkillName: "one"},
				{ID: "codex:two", Provider: onboardingImportProviderCodex, ProviderLabel: "Codex", TargetDirName: "two", SkillName: "two"},
				{ID: "codex:three", Provider: onboardingImportProviderCodex, ProviderLabel: "Codex", TargetDirName: "three", SkillName: "three"},
			},
		}},
		skillImport: onboardingImportSelection{Mode: onboardingImportModeSymlinkSource, Provider: onboardingImportProviderCodex},
	}
	screen := buildSkillSelectionScreen(state)
	if len(screen.Options) == 0 || screen.Options[0].ID != onboardingToggleAllOptionID {
		t.Fatalf("expected first option to be toggle-all action, got %+v", screen.Options)
	}
	if screen.Options[0].Title != "Disable all" {
		t.Fatalf("expected initial toggle-all label to disable all, got %q", screen.Options[0].Title)
	}
}

func TestDiscoverOnboardingImportsIncludesGeneratedSkillCandidates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	discovery := discoverOnboardingImports(filepath.Join(home, ".builder"))
	if discovery.err != nil {
		t.Fatalf("discover onboarding imports: %v", discovery.err)
	}
	if len(discovery.generatedSkillItems) == 0 {
		t.Fatal("expected generated skills to be listed during onboarding")
	}

	seen := map[string]bool{}
	for _, item := range discovery.generatedSkillItems {
		seen[item.SkillName] = true
		if item.ProviderLabel != "Preinstalled" {
			t.Fatalf("expected generated skill provider label Preinstalled, got %+v", item)
		}
	}
	for _, name := range []string{"builder-dogfooding", "creating-skills"} {
		if !seen[name] {
			t.Fatalf("expected generated skill %q in onboarding candidates, got %+v", name, discovery.generatedSkillItems)
		}
	}
}

func TestBuildSkillSelectionScreenShowsGeneratedSkillsWithoutImport(t *testing.T) {
	state := &onboardingFlowState{
		imports: onboardingImportDiscovery{generatedSkillItems: []onboardingSkillImportItem{
			{ID: "generated:builder-dogfooding", ProviderLabel: "Preinstalled", TargetDirName: "builder-dogfooding", SkillName: "builder-dogfooding"},
			{ID: "generated:creating-skills", ProviderLabel: "Preinstalled", TargetDirName: "creating-skills", SkillName: "creating-skills"},
		}},
		skillImport: onboardingImportSelection{Mode: onboardingImportModeNone},
	}
	screen := buildSkillSelectionScreen(state)
	if len(screen.Options) != 2 {
		t.Fatalf("expected generated skills as selectable options, got %+v", screen.Options)
	}
	for _, want := range []string{"Preinstalled / builder-dogfooding", "Preinstalled / creating-skills"} {
		found := false
		for _, option := range screen.Options {
			if option.Title == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected option %q, got %+v", want, screen.Options)
		}
	}
}

func TestBuildSkillTogglesCanDisableGeneratedSkillWithoutImport(t *testing.T) {
	state := &onboardingFlowState{
		imports: onboardingImportDiscovery{generatedSkillItems: []onboardingSkillImportItem{
			{ID: "generated:builder-dogfooding", ProviderLabel: "Preinstalled", TargetDirName: "builder-dogfooding", SkillName: "builder-dogfooding"},
			{ID: "generated:creating-skills", ProviderLabel: "Preinstalled", TargetDirName: "creating-skills", SkillName: "creating-skills"},
		}},
		skillImport: onboardingImportSelection{Mode: onboardingImportModeNone},
	}
	toggles := buildSkillToggles(state, map[string]bool{
		"generated:builder-dogfooding": true,
		"generated:creating-skills":    false,
	})
	disabled, ok := toggles["creating-skills"]
	if len(toggles) != 1 || !ok || disabled {
		t.Fatalf("expected disabled generated skill toggle, got %+v", toggles)
	}
}

func TestSkillSelectionCandidatesHideGeneratedSkillsShadowedByExistingSkills(t *testing.T) {
	state := &onboardingFlowState{
		imports: onboardingImportDiscovery{
			existingSkillNames: map[string]bool{"builder-dogfooding": true},
			generatedSkillItems: []onboardingSkillImportItem{
				{ID: "generated:builder-dogfooding", ProviderLabel: "Preinstalled", TargetDirName: "builder-dogfooding", SkillName: "builder-dogfooding"},
				{ID: "generated:creating-skills", ProviderLabel: "Preinstalled", TargetDirName: "creating-skills", SkillName: "creating-skills"},
			},
		},
	}
	items := skillSelectionCandidates(state)
	if len(items) != 1 || items[0].SkillName != "creating-skills" {
		t.Fatalf("expected only unshadowed generated skill, got %+v", items)
	}
}

func TestDiscoverOnboardingImportsHidesGeneratedSkillsShadowedByWorkspaceSkills(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	writeOnboardingTestSkill(t, filepath.Join(workspace, ".builder", "skills", "builder-dogfooding"), "builder-dogfooding", "workspace override")

	discovery := discoverOnboardingImportsForWorkspace(filepath.Join(home, ".builder"), workspace)
	if discovery.err != nil {
		t.Fatalf("discover onboarding imports: %v", discovery.err)
	}
	for _, item := range skillSelectionCandidates(&onboardingFlowState{imports: discovery}) {
		if normalizeOnboardingSkillName(item.SkillName) == "builder-dogfooding" {
			t.Fatalf("expected workspace skill to shadow generated builder-dogfooding, got %+v", item)
		}
	}
}

func TestReviewSummaryIncludesGeneratedSkillSelectionWithoutImport(t *testing.T) {
	state := &onboardingFlowState{
		settings: config.Settings{
			Theme:        theme.Auto,
			Model:        "gpt-5.5",
			EnabledTools: map[toolspec.ID]bool{},
		},
		imports: onboardingImportDiscovery{generatedSkillItems: []onboardingSkillImportItem{
			{ID: "generated:builder-dogfooding", ProviderLabel: "Preinstalled", TargetDirName: "builder-dogfooding", SkillName: "builder-dogfooding"},
			{ID: "generated:creating-skills", ProviderLabel: "Preinstalled", TargetDirName: "creating-skills", SkillName: "creating-skills"},
		}},
		skillSelection: map[string]bool{
			"generated:builder-dogfooding": true,
			"generated:creating-skills":    false,
		},
	}
	lines := reviewSummaryLines(state)
	hasEnabledLine := false
	for _, line := range lines {
		switch line {
		case "- Enabled skills: `1 enabled, 1 disabled`":
			hasEnabledLine = true
		case "- Skills import:":
			t.Fatalf("did not expect import summary when only generated skills were configured, got %q", lines)
		}
	}
	if !hasEnabledLine {
		t.Fatalf("expected generated skill counts in review summary, got %q", lines)
	}
}

func TestOnboardingFinalWritePersistsDisabledGeneratedSkillAndRuntimeHonorsIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, err := generated.Sync(context.Background(), generated.SyncOptions{HomeDir: home, FS: prompts.GeneratedSkillsFS}); err != nil {
		t.Fatalf("sync generated skills: %v", err)
	}
	defaultCfg, err := config.LoadGlobal(config.LoadOptions{})
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	state := onboardingFlowState{
		settings: defaultCfg.Settings,
		imports: onboardingImportDiscovery{generatedSkillItems: []onboardingSkillImportItem{
			{ID: "generated:builder-dogfooding", ProviderLabel: "Preinstalled", TargetDirName: "builder-dogfooding", SkillName: "builder-dogfooding"},
			{ID: "generated:creating-skills", ProviderLabel: "Preinstalled", TargetDirName: "creating-skills", SkillName: "creating-skills"},
		}},
		skillSelection: map[string]bool{
			"generated:builder-dogfooding": false,
			"generated:creating-skills":    true,
		},
	}
	state.settings.SkillToggles = buildSkillToggles(&state, state.skillSelection)
	model := newOnboardingModel(filepath.Join(home, ".builder"), state)
	msg := model.finalizeCmd(false)()
	done, ok := msg.(onboardingFinalizeDoneMsg)
	if !ok {
		t.Fatalf("expected onboarding finalize message, got %T", msg)
	}
	if done.err != nil {
		t.Fatalf("finalize onboarding: %v", done.err)
	}
	cfg, err := config.LoadGlobal(config.LoadOptions{})
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	disabled := config.DisabledSkillToggles(cfg.Settings)
	if !disabled["builder-dogfooding"] {
		t.Fatalf("expected disabled generated skill in loaded config, got toggles=%+v disabled=%+v", cfg.Settings.SkillToggles, disabled)
	}
	inspections, err := runtime.InspectSkills("", disabled)
	if err != nil {
		t.Fatalf("inspect skills: %v", err)
	}
	foundDisabled := false
	for _, inspection := range inspections {
		if inspection.SourceKind == "generated" && inspection.Name == "builder-dogfooding" {
			foundDisabled = inspection.Disabled
		}
	}
	if !foundDisabled {
		t.Fatalf("expected runtime inspection to mark generated builder-dogfooding disabled, got %+v", inspections)
	}
}

func TestOnboardingModelToggleAllHotkeyTogglesMultiSelection(t *testing.T) {
	model := newOnboardingModel(t.TempDir(), onboardingFlowState{theme: "dark"})
	model.currentScreen = onboardingScreen{
		ID:      "skills_enabled",
		Kind:    onboardingScreenMulti,
		Title:   "Choose enabled skills",
		Options: []onboardingOption{{ID: onboardingToggleAllOptionID, Title: "Disable all"}, {ID: "one", Title: "One"}, {ID: "two", Title: "Two"}, {ID: "three", Title: "Three"}},
	}
	model.selection = map[string]bool{"one": true, "two": true, "three": true}
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	updated := next.(*onboardingModel)
	for _, id := range []string{"one", "two", "three"} {
		if updated.selection[id] {
			t.Fatalf("expected %q to be toggled off", id)
		}
	}
	if updated.currentScreen.Options[0].Title != "Enable all" {
		t.Fatalf("expected toggle-all label to update after hotkey, got %q", updated.currentScreen.Options[0].Title)
	}
}

func TestOnboardingModelToggleAllMenuItemTogglesMultiSelection(t *testing.T) {
	model := newOnboardingModel(t.TempDir(), onboardingFlowState{theme: "dark"})
	model.currentScreen = onboardingScreen{
		ID:      "skills_enabled",
		Kind:    onboardingScreenMulti,
		Title:   "Choose enabled skills",
		Options: []onboardingOption{{ID: onboardingToggleAllOptionID, Title: "Disable all"}, {ID: "one", Title: "One"}, {ID: "two", Title: "Two"}, {ID: "three", Title: "Three"}},
	}
	model.selection = map[string]bool{"one": true, "two": true, "three": true}
	model.cursor = 0
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeySpace})
	updated := next.(*onboardingModel)
	for _, id := range []string{"one", "two", "three"} {
		if updated.selection[id] {
			t.Fatalf("expected %q to be toggled off", id)
		}
	}
}

func TestOnboardingModelRefreshToggleAllTracksCheckedState(t *testing.T) {
	model := newOnboardingModel(t.TempDir(), onboardingFlowState{theme: "dark"})
	model.currentScreen = onboardingScreen{
		ID:      "skills_enabled",
		Kind:    onboardingScreenMulti,
		Title:   "Choose enabled skills",
		Options: []onboardingOption{{ID: onboardingToggleAllOptionID, Title: "Disable all"}, {ID: "one", Title: "One"}, {ID: "two", Title: "Two"}},
	}
	model.selection = map[string]bool{"one": true, "two": true}
	model.refreshToggleAllOption()
	if !model.selection[onboardingToggleAllOptionID] {
		t.Fatal("expected toggle-all action to render checked when all options are enabled")
	}
	if got := model.currentScreen.Options[0].Title; got != "Disable all" {
		t.Fatalf("expected toggle-all title to stay on Disable all, got %q", got)
	}

	model.selection["two"] = false
	model.refreshToggleAllOption()
	if model.selection[onboardingToggleAllOptionID] {
		t.Fatal("expected toggle-all action to render unchecked when not all options are enabled")
	}
	if got := model.currentScreen.Options[0].Title; got != "Enable all" {
		t.Fatalf("expected toggle-all title to switch to Enable all, got %q", got)
	}
}

func TestOnboardingSubmitCurrentScreenShowsValidationError(t *testing.T) {
	model := newOnboardingModel(t.TempDir(), onboardingFlowState{})
	model.stepIndex = 2
	model.syncScreen(true)
	setSingleLineEditorValue(&model.input, "")
	next, _ := model.submitCurrentScreen()
	updated := next.(*onboardingModel)
	if updated.errorText == "" {
		t.Fatal("expected submit validation error to be captured")
	}
	if updated.currentScreen.ErrorText == "" {
		t.Fatal("expected submit validation error to be shown on the current screen")
	}
}

func TestOnboardingWorkflowStartsWithThemeStep(t *testing.T) {
	workflow := newOnboardingWorkflow(&onboardingFlowState{})
	steps := workflow.visibleSteps(&onboardingFlowState{})
	if len(steps) == 0 {
		t.Fatal("expected onboarding workflow to include steps")
	}
	if steps[0].ID() != "theme" {
		t.Fatalf("expected first onboarding step to be theme, got %q", steps[0].ID())
	}
}

func TestThemeStepDefaultsToDetectedTheme(t *testing.T) {
	original := lipgloss.HasDarkBackground()
	defer lipgloss.SetHasDarkBackground(original)

	lipgloss.SetHasDarkBackground(false)
	lightState := &onboardingFlowState{}
	lightScreen := newOnboardingWorkflow(lightState).visibleSteps(lightState)[0].Build(lightState)
	if lightScreen.DefaultOptionID != "light" {
		t.Fatalf("expected light background detection to preselect light theme, got %q", lightScreen.DefaultOptionID)
	}

	lipgloss.SetHasDarkBackground(true)
	darkState := &onboardingFlowState{}
	darkScreen := newOnboardingWorkflow(darkState).visibleSteps(darkState)[0].Build(darkState)
	if darkScreen.DefaultOptionID != "dark" {
		t.Fatalf("expected dark background detection to preselect dark theme, got %q", darkScreen.DefaultOptionID)
	}
}

func TestThemeStepChoicePreservesAutoWhenKeepingDetectedDefault(t *testing.T) {
	original := lipgloss.HasDarkBackground()
	defer lipgloss.SetHasDarkBackground(original)

	lipgloss.SetHasDarkBackground(true)
	state := &onboardingFlowState{settings: config.Settings{Theme: theme.Auto}}
	themeStep := newOnboardingWorkflow(state).visibleSteps(state)[0]
	if err := themeStep.ApplyChoice(state, "dark"); err != nil {
		t.Fatalf("apply detected theme choice: %v", err)
	}
	if state.settings.Theme != theme.Auto {
		t.Fatalf("expected detected default to preserve auto, got %q", state.settings.Theme)
	}

	lipgloss.SetHasDarkBackground(false)
	state = &onboardingFlowState{settings: config.Settings{Theme: theme.Auto}}
	themeStep = newOnboardingWorkflow(state).visibleSteps(state)[0]
	if err := themeStep.ApplyChoice(state, "dark"); err != nil {
		t.Fatalf("apply explicit override: %v", err)
	}
	if state.settings.Theme != theme.Dark {
		t.Fatalf("expected overriding detected default to persist explicit dark, got %q", state.settings.Theme)
	}
}

func TestOnboardingDefaultsPathPersistsChosenTheme(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	model := newOnboardingModel(t.TempDir(), onboardingFlowState{settings: config.Settings{Theme: "light"}, theme: "light"})
	msg := model.finalizeCmd(true)()
	done, ok := msg.(onboardingFinalizeDoneMsg)
	if !ok {
		t.Fatalf("expected onboarding finalize message, got %T", msg)
	}
	if done.err != nil {
		t.Fatalf("finalize defaults path: %v", done.err)
	}
	if !done.result.Completed || !done.result.CreatedDefaultConfig {
		t.Fatalf("expected defaults path to create config, got %+v", done.result)
	}
	contents, err := os.ReadFile(done.result.SettingsPath)
	if err != nil {
		t.Fatalf("read written settings: %v", err)
	}
	if !strings.Contains(string(contents), "theme = \"light\"") {
		t.Fatalf("expected defaults path to persist chosen theme, got %q", string(contents))
	}
}
