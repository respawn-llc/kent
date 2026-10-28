package patchformat

import (
	"path/filepath"
	"testing"
)

func TestAbsolutePatchPathDisplayPreservesModelInput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "project")
	for _, path := range []string{
		cwd + "/nested/../file.go",
		home + "/elsewhere/./file.go",
	} {
		presentation := Render("*** Begin Patch\n*** Add File: "+path+"\n+hello\n*** End Patch", cwd)
		if !presentation.Valid() || presentation.Changes == nil {
			t.Fatalf("invalid patch presentation: %+v", presentation)
		}
		display := presentation.Changes.Files[0].Path
		if display.Relative != path || display.Absolute != filepath.Clean(path) {
			t.Fatalf("path = %+v, want display %q and canonical target %q", display, path, filepath.Clean(path))
		}
	}
}
