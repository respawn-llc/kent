package prompts_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"core/prompts"
	"core/shared/config"
)

func TestGeneratedSyncSeedsDefaultRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	result, err := prompts.GeneratedSync(context.Background(), prompts.GeneratedSyncOptions{})
	if err != nil {
		t.Fatalf("GeneratedSync: %v", err)
	}
	wantSkillsRoot := filepath.Join(home, config.ConfigDirName, ".generated", "skills")
	if result.GeneratedSkillsRoot != wantSkillsRoot {
		t.Fatalf("generated skills root = %q, want %q", result.GeneratedSkillsRoot, wantSkillsRoot)
	}
	if entries, err := os.ReadDir(wantSkillsRoot); err != nil {
		t.Fatalf("expected generated skills root to be seeded: %v", err)
	} else if len(entries) == 0 {
		t.Fatal("expected generated skills root to contain at least one skill")
	}
	if result.RecoveredWarning != "" {
		t.Fatalf("did not expect recovered warning on clean seed, got %+v", result)
	}
}
