package runtime

import (
	"path/filepath"
	"testing"

	generatedassets "core/prompts"
)

func TestAgentsInjectionPathsUsesGlobalConfigDirWhenSet(t *testing.T) {
	globalDir := t.TempDir()
	workspace := t.TempDir()
	paths, err := agentsInjectionPaths(workspace, globalDir)
	if err != nil {
		t.Fatalf("agentsInjectionPaths: %v", err)
	}
	wantGlobal := filepath.Clean(filepath.Join(globalDir, agentsFileName))
	if len(paths) == 0 || paths[0] != wantGlobal {
		t.Fatalf("global AGENTS.md path = %v, want first entry %q", paths, wantGlobal)
	}
	wantWorkspace := filepath.Clean(filepath.Join(workspace, agentsFileName))
	if !containsPath(paths, wantWorkspace) {
		t.Fatalf("workspace AGENTS.md path missing from %v, want %q", paths, wantWorkspace)
	}
}

func TestAgentsInjectionPathsFallsBackToHomeWhenEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := agentsInjectionPaths(t.TempDir(), "")
	if err != nil {
		t.Fatalf("agentsInjectionPaths: %v", err)
	}
	wantGlobal := filepath.Clean(filepath.Join(home, agentsGlobalDirName, agentsFileName))
	if len(paths) == 0 || paths[0] != wantGlobal {
		t.Fatalf("default global AGENTS.md path = %v, want first entry %q", paths, wantGlobal)
	}
}

func TestSkillDiscoveryRootsUsesGlobalConfigDirWhenSet(t *testing.T) {
	globalDir := t.TempDir()
	roots, err := skillDiscoveryRoots("", globalDir)
	if err != nil {
		t.Fatalf("skillDiscoveryRoots: %v", err)
	}
	wantGlobal := filepath.Clean(filepath.Join(globalDir, skillsDirName))
	wantGenerated := filepath.Clean(filepath.Join(globalDir, ".generated", "skills"))
	if !containsRoot(roots, wantGlobal, skillSourceGlobal) {
		t.Fatalf("global skills root missing from %v, want %q", roots, wantGlobal)
	}
	if !containsRoot(roots, wantGenerated, skillSourceGenerated) {
		t.Fatalf("generated skills root missing from %v, want %q", roots, wantGenerated)
	}
}

func TestSkillDiscoveryRootsFallsBackToHomeWhenEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	roots, err := skillDiscoveryRoots("", "")
	if err != nil {
		t.Fatalf("skillDiscoveryRoots: %v", err)
	}
	wantGlobal := filepath.Clean(filepath.Join(home, agentsGlobalDirName, skillsDirName))
	if !containsRoot(roots, wantGlobal, skillSourceGlobal) {
		t.Fatalf("default global skills root missing from %v, want %q", roots, wantGlobal)
	}
	wantGenerated, err := generatedassets.GeneratedSkillsRoot()
	if err != nil {
		t.Fatalf("GeneratedSkillsRoot: %v", err)
	}
	if !containsRoot(roots, filepath.Clean(wantGenerated), skillSourceGenerated) {
		t.Fatalf("default generated skills root missing from %v, want %q", roots, wantGenerated)
	}
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

func containsRoot(roots []skillRoot, wantPath string, wantKind skillSourceKind) bool {
	for _, r := range roots {
		if r.Path == wantPath && r.Kind == wantKind {
			return true
		}
	}
	return false
}
