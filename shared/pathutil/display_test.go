package pathutil_test

import (
	"path/filepath"
	"testing"

	"core/shared/pathutil"
)

func TestCompact(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	cwd := filepath.Join(home, "project")
	for _, test := range []struct {
		name, target, want string
	}{
		{"cwd file", filepath.Join(cwd, "AGENTS.md"), "AGENTS.md"},
		{"nested file", filepath.Join(cwd, ".kent", "skills", "review", "SKILL.md"), ".kent/skills/review/SKILL.md"},
		{"cwd itself", cwd, "."},
		{"home file", filepath.Join(home, ".kent", "AGENTS.md"), "~/.kent/AGENTS.md"},
		{"home itself", home, "~"},
		{"sibling", filepath.Join(home, "other", "AGENTS.md"), "~/other/AGENTS.md"},
		{"cwd prefix collision", cwd + "-other/AGENTS.md", "~/project-other/AGENTS.md"},
		{"home prefix collision", home + "-other/AGENTS.md", home + "-other/AGENTS.md"},
		{"parent outside home", filepath.Dir(home), filepath.Dir(home)},
		{"spaces", filepath.Join(cwd, "my skill", "SKILL.md"), "my skill/SKILL.md"},
		{"dots in name", filepath.Join(cwd, "..notes", "file.md"), "..notes/file.md"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := pathutil.Compact(test.target, cwd, home); got != filepath.ToSlash(test.want) {
				t.Fatalf("Compact() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCollapseHome(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".kent", "AGENTS.md")
	if got := pathutil.CollapseHome(target, home); got != "~/.kent/AGENTS.md" {
		t.Fatalf("CollapseHome() = %q", got)
	}
	other := home + "-other/file.md"
	if got := pathutil.CollapseHome(other, home); got != filepath.ToSlash(other) {
		t.Fatalf("home prefix collision shortened to %q", got)
	}
}
