package pathdisplay_test

import (
	"os"
	"path/filepath"
	"testing"

	"core/cli/internal/pathdisplay"
)

func TestCompactDisplayBoundaries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, test := range []struct{ path, want string }{
		{home, "~"},
		{filepath.Join(home, "file"), "~/file"},
		{home + "-other/file", home + "-other/file"},
		{"relative/file", "relative/file"},
	} {
		if got := pathdisplay.Compact(test.path, nil); got != test.want {
			t.Fatalf("display %q = %q, want %q", test.path, got, test.want)
		}
	}
}

func TestMissingHomePreservesUsablePath(t *testing.T) {
	t.Setenv("HOME", "")
	path := filepath.Join(t.TempDir(), "file")
	if got := pathdisplay.Compact(path, nil); got != path {
		t.Fatalf("display with missing home = %q, want %q", got, path)
	}
}

func TestCompactDisplayUsesSuppliedWorkingDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "project")
	path := filepath.Join(cwd, "file")
	if got := pathdisplay.Compact(path, &cwd); got != "./file" {
		t.Fatalf("compact display = %q, want ./file", got)
	}
}

func TestCompactDisplayUsesCurrentWorkingDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "project")
	if err := os.Mkdir(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	if got := pathdisplay.Compact(filepath.Join(cwd, "file"), nil); got != "./file" {
		t.Fatalf("display = %q, want ./file", got)
	}
}

func TestMissingHomeStillCompactsWorkingDirectory(t *testing.T) {
	t.Setenv("HOME", "")
	cwd := t.TempDir()
	path := filepath.Join(cwd, "file")
	if got := pathdisplay.Compact(path, &cwd); got != "./file" {
		t.Fatalf("display with missing home = %q, want ./file", got)
	}
}
