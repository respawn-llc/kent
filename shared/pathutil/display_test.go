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
		{"cwd file", filepath.Join(cwd, "AGENTS.md"), "./AGENTS.md"},
		{"nested file", filepath.Join(cwd, ".kent", "skills", "review", "SKILL.md"), "./.kent/skills/review/SKILL.md"},
		{"cwd itself", cwd, "./"},
		{"home file", filepath.Join(home, ".kent", "AGENTS.md"), "~/.kent/AGENTS.md"},
		{"home itself", home, "~"},
		{"sibling", filepath.Join(home, "other", "AGENTS.md"), "~/other/AGENTS.md"},
		{"cwd prefix collision", cwd + "-other/AGENTS.md", "~/project-other/AGENTS.md"},
		{"home prefix collision", home + "-other/AGENTS.md", home + "-other/AGENTS.md"},
		{"parent outside home", filepath.Dir(home), filepath.Dir(home)},
		{"spaces", filepath.Join(cwd, "my skill", "SKILL.md"), "./my skill/SKILL.md"},
		{"dots in name", filepath.Join(cwd, "..notes", "file.md"), "./..notes/file.md"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := pathutil.Compact(test.target, &home, &cwd); got != filepath.ToSlash(test.want) {
				t.Fatalf("Compact() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCompactUsesCurrentWorkingDirectory(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	home := filepath.Dir(cwd)
	if got := pathutil.Compact(filepath.Join(cwd, "file"), &home, nil); got != "./file" {
		t.Fatalf("Compact() = %q, want ./file", got)
	}
}

func TestCompactHomeBoundaries(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".kent", "AGENTS.md")
	if got := pathutil.Compact(target, &home, nil); got != "~/.kent/AGENTS.md" {
		t.Fatalf("Compact() = %q", got)
	}
	other := home + "-other/file.md"
	if got := pathutil.Compact(other, &home, nil); got != filepath.ToSlash(other) {
		t.Fatalf("home prefix collision shortened to %q", got)
	}
}

func TestCompactPreservesRelativePaths(t *testing.T) {
	cwd := t.TempDir()
	home := filepath.Dir(cwd)
	for _, path := range []string{".", "..", "./file", "../file", "relative/file", "..notes/file"} {
		t.Run(path, func(t *testing.T) {
			if got := pathutil.Compact(path, &home, &cwd); got != filepath.ToSlash(path) {
				t.Fatalf("Compact(%q) = %q", path, got)
			}
		})
	}
}
