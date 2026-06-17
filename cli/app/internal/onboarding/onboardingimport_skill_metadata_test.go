package onboarding

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSkillMetadataReturnsName(t *testing.T) {
	skillDir := t.TempDir()
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: Example Skill\ndescription: Example description\n---\nBody\n"), 0o644); err != nil {
		t.Fatalf("write skill metadata: %v", err)
	}

	meta, ok := ParseSkillMetadata(path)
	if !ok {
		t.Fatal("expected skill metadata")
	}
	if meta.Name != "Example Skill" {
		t.Fatalf("name = %q, want Example Skill", meta.Name)
	}
}

func TestParseSkillMetadataRejectsMissingFile(t *testing.T) {
	if _, ok := ParseSkillMetadata(filepath.Join(t.TempDir(), "missing.md")); ok {
		t.Fatal("expected missing skill metadata to be rejected")
	}
}
