package prompts_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"core/prompts"
)

func TestGeneratedSyncSeedsDefaultRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	layout, err := prompts.GeneratedLayoutFor("")
	if err != nil {
		t.Fatalf("GeneratedLayoutFor: %v", err)
	}
	if err := prompts.GeneratedSync(context.Background(), ""); err != nil {
		t.Fatalf("GeneratedSync: %v", err)
	}
	if entries, err := os.ReadDir(layout.GeneratedSkillsRoot); err != nil {
		t.Fatalf("expected generated skills root to be seeded: %v", err)
	} else if len(entries) == 0 {
		t.Fatal("expected generated skills root to contain at least one skill")
	}
}

func TestGeneratedSyncRecoversEditedTreeAndSeedsReplacement(t *testing.T) {
	configRoot := t.TempDir()
	layout, err := prompts.GeneratedLayoutFor(configRoot)
	if err != nil {
		t.Fatalf("GeneratedLayoutFor: %v", err)
	}
	if err := prompts.GeneratedSync(context.Background(), configRoot); err != nil {
		t.Fatalf("seed generated tree: %v", err)
	}

	const editedName = "operator-edit.txt"
	const editedContent = "keep this recovered content"
	editedPath := filepath.Join(layout.GeneratedRoot, editedName)
	if err := os.WriteFile(editedPath, []byte(editedContent), 0o644); err != nil {
		t.Fatalf("write generated edit: %v", err)
	}
	if err := prompts.GeneratedSync(context.Background(), configRoot); err != nil {
		t.Fatalf("recover generated tree: %v", err)
	}

	recoveryEntries, err := os.ReadDir(layout.RecoveryRoot)
	if err != nil {
		t.Fatalf("read recovered root: %v", err)
	}
	if len(recoveryEntries) != 1 || !recoveryEntries[0].IsDir() {
		t.Fatalf("recovery entries = %v, want one recovery directory", recoveryEntries)
	}
	recoveredContent, err := os.ReadFile(filepath.Join(
		layout.RecoveryRoot,
		recoveryEntries[0].Name(),
		filepath.Base(layout.GeneratedRoot),
		editedName,
	))
	if err != nil {
		t.Fatalf("read archived edit: %v", err)
	}
	if string(recoveredContent) != editedContent {
		t.Fatalf("archived edit content = %q, want original content", recoveredContent)
	}
	if _, err := os.Stat(editedPath); !os.IsNotExist(err) {
		t.Fatalf("recovered edit remains in generated tree, stat err = %v", err)
	}
	generatedSkills, err := os.ReadDir(layout.GeneratedSkillsRoot)
	if err != nil {
		t.Fatalf("read reseeded generated skills: %v", err)
	}
	if len(generatedSkills) == 0 {
		t.Fatal("expected generated skills to be reseeded")
	}
	recovered, err := prompts.RecoveredRootNonEmptyFor(configRoot)
	if err != nil {
		t.Fatalf("RecoveredRootNonEmptyFor: %v", err)
	}
	if !recovered {
		t.Fatal("expected recovered root to contain the archived edit")
	}
}
