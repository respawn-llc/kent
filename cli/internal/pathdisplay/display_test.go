package pathdisplay_test

import (
	"path/filepath"
	"testing"

	"core/cli/internal/pathdisplay"
)

func TestHomeDisplayBoundaries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, test := range []struct{ path, want string }{
		{home, "~"},
		{filepath.Join(home, "file"), "~/file"},
		{home + "-other/file", home + "-other/file"},
		{"relative/file", "relative/file"},
	} {
		if got := pathdisplay.Home(test.path); got != test.want {
			t.Fatalf("display %q = %q, want %q", test.path, got, test.want)
		}
	}
}

func TestMissingHomePreservesUsablePath(t *testing.T) {
	t.Setenv("HOME", "")
	path := filepath.Join(t.TempDir(), "file")
	if got := pathdisplay.Home(path); got != path {
		t.Fatalf("display with missing home = %q, want %q", got, path)
	}
}

func TestCompactDisplayUsesSuppliedWorkingDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "project")
	path := filepath.Join(cwd, "file")
	if got := pathdisplay.Compact(path, cwd); got != "./file" {
		t.Fatalf("compact display = %q, want ./file", got)
	}
}
