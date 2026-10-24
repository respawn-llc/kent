package skillcatalog

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	brand "core/shared/config"
)

func TestDiscoverOrdersRootsAndReadsEmbeddedGeneratedWhenUnseeded(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	globalSkillPath := writeSkill(t, filepath.Join(root, SkillsDirName, "global-skill"), "global-skill", "global")
	workspaceSkillPath := writeSkill(t, filepath.Join(workspace, brand.ConfigDirName, SkillsDirName, "workspace-skill"), "workspace-skill", "workspace")

	result, err := Discover(Options{WorkspaceRoot: workspace, ConfigRoot: root, IncludeEmbeddedGenerated: true})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	requireSkillPath(t, result.PromptSkills(), canonicalSlashPath(t, globalSkillPath))
	requireSkillPath(t, result.PromptSkills(), canonicalSlashPath(t, workspaceSkillPath))
	generatedRoot := filepath.Join(root, ".generated", SkillsDirName)
	foundGenerated := false
	for _, skill := range result.PromptSkills() {
		if skill.SourceKind == SourceKindGenerated {
			foundGenerated = true
			if !strings.HasPrefix(skill.Path, filepath.ToSlash(generatedRoot)+"/") {
				t.Fatalf("generated skill path = %q, want projected under %q", skill.Path, generatedRoot)
			}
		}
	}
	if !foundGenerated {
		t.Fatalf("expected embedded generated skills, got %+v", result.PromptSkills())
	}
}

func TestDiscoverDisabledSkillNamedEnabledStillEnumeratesCatalog(t *testing.T) {
	root := t.TempDir()
	enabledPath := writeSkill(t, filepath.Join(root, SkillsDirName, "enabled"), "enabled", "enabled skill")
	otherPath := writeSkill(t, filepath.Join(root, SkillsDirName, "other"), "other", "other skill")

	result, err := Discover(Options{
		ConfigRoot: root,
		Policy: brand.ResolveSkillPolicy(brand.Settings{
			SkillToggles: map[string]bool{"enabled": false},
		}),
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	disabledPath := canonicalSlashPath(t, enabledPath)
	requireSkillPath(t, result.PromptSkills(), canonicalSlashPath(t, otherPath))
	for _, skill := range result.PromptSkills() {
		if skill.Path == disabledPath {
			t.Fatalf("disabled skill named enabled remained model-visible: %+v", result.PromptSkills())
		}
	}
	var disabled Inspection
	for _, inspection := range result.Inspections {
		if inspection.Path == disabledPath {
			disabled = inspection
			break
		}
	}
	if !disabled.Loaded() || !disabled.Disabled {
		t.Fatalf("disabled inspection = %+v, want loaded and disabled", disabled)
	}
}

func TestDiscoverGeneratedShadowedByUserSkillAndDisabled(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	writeSkill(t, filepath.Join(workspace, brand.ConfigDirName, SkillsDirName, "skill-creator"), "skill-creator", "workspace")
	writeSkill(t, filepath.Join(root, ".generated", SkillsDirName, "skill-creator"), "skill-creator", "generated")

	result, err := Discover(Options{
		WorkspaceRoot: workspace,
		ConfigRoot:    root,
		Policy: brand.ResolveSkillPolicy(brand.Settings{
			SkillToggles: map[string]bool{"skill-creator": false},
		}),
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	for _, skill := range result.PromptSkills() {
		if strings.EqualFold(skill.Name, "skill-creator") {
			t.Fatalf("disabled/shadowed skill loaded: %+v", skill)
		}
	}
	var generated Inspection
	for _, inspection := range result.Inspections {
		if inspection.SourceKind == SourceKindGenerated && inspection.Name == "skill-creator" {
			generated = inspection
			break
		}
	}
	if !generated.Loaded() || !generated.Shadowed || !generated.Disabled {
		t.Fatalf("generated inspection = %+v, want loaded shadowed disabled", generated)
	}
}

func TestDiscoverDeduplicatesResolvedSkillPaths(t *testing.T) {
	root := t.TempDir()
	targetSkillPath := writeSkill(t, filepath.Join(t.TempDir(), "shared", "linked"), "linked", "shared")
	firstLink := filepath.Join(root, SkillsDirName, "first")
	secondLink := filepath.Join(root, SkillsDirName, "second")
	if err := os.MkdirAll(filepath.Dir(firstLink), 0o755); err != nil {
		t.Fatalf("mkdir links: %v", err)
	}
	if err := os.Symlink(filepath.Dir(targetSkillPath), firstLink); err != nil {
		t.Fatalf("symlink first: %v", err)
	}
	if err := os.Symlink(filepath.Dir(targetSkillPath), secondLink); err != nil {
		t.Fatalf("symlink second: %v", err)
	}

	result, err := Discover(Options{ConfigRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.PromptSkills()) != 1 {
		t.Fatalf("loaded skills = %+v, want exactly one resolved path", result.PromptSkills())
	}
	duplicates := 0
	for _, inspection := range result.Inspections {
		if inspection.Failure != nil && inspection.Failure.Kind == FailureDuplicate {
			duplicates++
		}
	}
	if duplicates != 1 {
		t.Fatalf("duplicate inspections = %d, inspections=%+v", duplicates, result.Inspections)
	}
}

func TestDiscoverFirstAliasOwnsSkillEnablement(t *testing.T) {
	root := t.TempDir()
	target := writeSkill(t, filepath.Join(t.TempDir(), "target"), "", "shared")
	skillsRoot := filepath.Join(root, SkillsDirName)
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if err := os.Symlink(filepath.Dir(target), filepath.Join(skillsRoot, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, enabled := range []bool{false, true} {
		result, err := Discover(Options{
			ConfigRoot: root,
			Policy: brand.ResolveSkillPolicy(brand.Settings{
				SkillToggles: map[string]bool{"a": enabled},
			}),
		})
		if err != nil {
			t.Fatal(err)
		}
		skills := result.PromptSkills()
		if enabled {
			if len(skills) != 1 || skills[0].Name != "a" {
				t.Fatalf("enabled first alias prompt skills = %+v, want a", skills)
			}
		} else if len(skills) != 0 {
			t.Fatalf("duplicate bypassed disabled first alias: %+v", skills)
		}
		if len(result.Inspections) != 2 {
			t.Fatalf("inspections = %+v, want both aliases", result.Inspections)
		}
		first, second := result.Inspections[0], result.Inspections[1]
		if first.Name != "a" || !first.Loaded() || first.Disabled == enabled {
			t.Fatalf("first alias enabled=%v inspection=%+v", enabled, first)
		}
		if second.Name != "b" || second.Loaded() || second.Disabled ||
			second.Failure == nil || second.Failure.Kind != FailureDuplicate {
			t.Fatalf("second alias inspection=%+v, want unloaded duplicate", second)
		}
	}
}

func TestDiscoverReportsBrokenSymlinkedSkillDirectory(t *testing.T) {
	root := t.TempDir()
	skillsRoot := filepath.Join(root, SkillsDirName)
	brokenLinkPath := filepath.Join(skillsRoot, "broken-skill")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatalf("mkdir skills root: %v", err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing-skill-dir"), brokenLinkPath); err != nil {
		t.Fatalf("symlink broken skill dir: %v", err)
	}
	invalidPath := writeSkill(t, filepath.Join(skillsRoot, "invalid"), "invalid", "")

	result, err := Discover(Options{ConfigRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	issues := result.DiscoveryWarnings()
	if len(issues) != 1 || issues[0].Path != filepath.ToSlash(brokenLinkPath) || strings.TrimSpace(issues[0].Reason) == "" {
		t.Fatalf("broken symlink issues = %+v, want one structured issue", issues)
	}
	if len(result.PromptSkills()) != 0 || len(result.Inspections) != 2 {
		t.Fatalf("invalid entries must remain status-only: %+v", result)
	}
	paths := []string{result.Inspections[0].Path, result.Inspections[1].Path}
	if !slices.Contains(paths, filepath.ToSlash(filepath.Join(brokenLinkPath, SkillFileName))) ||
		!slices.Contains(paths, canonicalSlashPath(t, invalidPath)) {
		t.Fatalf("inspection paths = %v", paths)
	}
}

func TestDiscoverPromptOrderIsRootThenEntryOrder(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	want := []string{"global-z", "global-a", "workspace-z", "workspace-a", "generated-z", "generated-a"}
	for idx, dir := range []string{
		filepath.Join(root, SkillsDirName),
		filepath.Join(workspace, brand.ConfigDirName, SkillsDirName),
		filepath.Join(root, ".generated", SkillsDirName),
	} {
		writeSkill(t, filepath.Join(dir, "a"), want[idx*2], "first")
		writeSkill(t, filepath.Join(dir, "z"), want[idx*2+1], "second")
	}
	result, err := Discover(Options{WorkspaceRoot: workspace, ConfigRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	status := result.StatusInspections()
	var statusPaths []string
	for _, skill := range status {
		statusPaths = append(statusPaths, skill.Path)
	}
	if !slices.IsSorted(statusPaths) {
		t.Fatalf("status paths are not sorted: %v", statusPaths)
	}
	for _, skill := range result.PromptSkills() {
		names = append(names, skill.Name)
	}
	if !slices.Equal(names, want) {
		t.Fatalf("prompt order = %v, want %v", names, want)
	}
}

func canonicalSlashPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("eval symlinks for %q: %v", path, err)
	}
	return filepath.ToSlash(resolved)
}

func TestDiscoverReportsUnreadableRootAsStructuredError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, SkillsDirName), []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write non-directory skills root: %v", err)
	}
	_, err := Discover(Options{ConfigRoot: root})
	if !errors.Is(err, ErrReadSkillsDirectory) {
		t.Fatalf("Discover error = %v, want ErrReadSkillsDirectory", err)
	}
}

func requireSkillPath(t *testing.T, skills []Inspection, path string) {
	t.Helper()
	for _, skill := range skills {
		if skill.Path == path {
			return
		}
	}
	t.Fatalf("skill path %q not found in %+v", path, skills)
}

func writeSkill(t *testing.T, dir, name, description string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	path := filepath.Join(dir, SkillFileName)
	contents := "---\nname: " + name + "\ndescription: " + description + "\n---\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	return path
}
