package onboardingimportfs

import (
	"os"
	"path/filepath"
	"testing"

	"builder/cli/app/internal/onboardingimportchoice"
)

func TestExecuteSymlinkValidatesSourceBeforeReplacingEmptyTarget(t *testing.T) {
	globalRoot := t.TempDir()
	targetPath := filepath.Join(globalRoot, "skills")
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	_, err := ExecuteSymlink(targetPath, filepath.Join(t.TempDir(), "missing"), "skills", "skills source Codex")
	if err == nil {
		t.Fatal("expected missing source to fail")
	}
	info, statErr := os.Lstat(targetPath)
	if statErr != nil {
		t.Fatalf("expected target to remain after source validation failure: %v", statErr)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected target to remain plain directory, got mode %v", info.Mode())
	}
}

func TestRollbackCreatedPathsRemovesPartialSkillImportAfterCommandFailure(t *testing.T) {
	globalRoot := t.TempDir()
	skillSource := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(skillSource, 0o755); err != nil {
		t.Fatalf("mkdir skill source: %v", err)
	}
	created, err := ExecuteSymlink(filepath.Join(globalRoot, "skills"), skillSource, "skills", "skills source Claude Code")
	if err != nil {
		t.Fatalf("execute skill symlink: %v", err)
	}
	_, commandErr := ExecuteSymlink(filepath.Join(globalRoot, "prompts"), filepath.Join(t.TempDir(), "missing-prompts"), "slash command", "slash commands from Claude Code")
	if commandErr == nil {
		t.Fatal("expected command symlink to fail")
	}
	if err := RollbackCreatedPaths(created); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(globalRoot, "skills")); !os.IsNotExist(err) {
		t.Fatalf("expected partial skills symlink to be removed, got %v", err)
	}
}

func TestShouldSkipTargets(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	skip, err := ShouldSkipTarget(missing)
	if err != nil || skip {
		t.Fatalf("missing skip=%v err=%v, want false nil", skip, err)
	}
	empty := filepath.Join(root, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatalf("mkdir empty: %v", err)
	}
	skip, err = ShouldSkipTarget(empty)
	if err != nil || skip {
		t.Fatalf("empty skip=%v err=%v, want false nil", skip, err)
	}
	populated := filepath.Join(root, "populated")
	if err := os.MkdirAll(populated, 0o755); err != nil {
		t.Fatalf("mkdir populated: %v", err)
	}
	if err := os.WriteFile(filepath.Join(populated, "file"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write populated: %v", err)
	}
	skip, err = ShouldSkipTarget(populated)
	if err != nil || !skip {
		t.Fatalf("populated skip=%v err=%v, want true nil", skip, err)
	}
}

func TestProviderCommandSourceRequiresDirectMarkdown(t *testing.T) {
	base := t.TempDir()
	commands := filepath.Join(base, "commands")
	prompts := filepath.Join(base, "prompts")
	if err := os.MkdirAll(filepath.Join(commands, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested commands: %v", err)
	}
	if err := os.WriteFile(filepath.Join(commands, "nested", "ignored.md"), []byte("ignored"), 0o644); err != nil {
		t.Fatalf("write nested command: %v", err)
	}
	if err := os.MkdirAll(prompts, 0o755); err != nil {
		t.Fatalf("mkdir prompts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(prompts, "review.md"), []byte("review"), 0o644); err != nil {
		t.Fatalf("write prompt command: %v", err)
	}
	provider := Provider{ID: onboardingimportchoice.ProviderID("claude_code"), Label: "Claude Code"}
	root, items, err := DiscoverProviderCommands(provider, base)
	if err != nil {
		t.Fatalf("discover commands: %v", err)
	}
	if root != prompts || len(items) != 1 || items[0].TargetFileName != "review.md" {
		t.Fatalf("unexpected command discovery root=%q items=%+v", root, items)
	}
}
