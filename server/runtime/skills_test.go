package runtime

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"core/server/llm"
	brand "core/shared/config"
)

func TestSkillsContextMessageSkipsInvalidSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspace := t.TempDir()
	invalidSkillDir := filepath.Join(workspace, brand.ConfigDirName, "skills", "invalid")
	if err := os.MkdirAll(invalidSkillDir, 0o755); err != nil {
		t.Fatalf("mkdir invalid skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(invalidSkillDir, skillFileName), []byte("---\nname: invalid\n---\n"), 0o644); err != nil {
		t.Fatalf("write invalid skill: %v", err)
	}

	content, found, err := skillsContextMessageWithDisabled(workspace, nil)
	if err != nil {
		t.Fatalf("skillsContextMessage: %v", err)
	}
	if found {
		t.Fatalf("expected no skills context for invalid skill, got %q", content)
	}
}

func TestResolveSkillDirUsesLstatWhenDirEntryTypeIsUnknown(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspace := t.TempDir()
	brokenLinkPath := filepath.Join(workspace, brand.ConfigDirName, "skills", "broken-skill")
	if err := os.MkdirAll(filepath.Dir(brokenLinkPath), 0o755); err != nil {
		t.Fatalf("mkdir broken symlink parent: %v", err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing-skill-dir"), brokenLinkPath); err != nil {
		t.Fatalf("symlink broken skill dir: %v", err)
	}

	resolution := resolveSkillDir(filepath.Dir(brokenLinkPath), fakeDirEntry{name: filepath.Base(brokenLinkPath)})
	if resolution.Discoverable {
		t.Fatalf("expected broken symlink with unknown entry type to stay undiscoverable, got %+v", resolution)
	}
	if resolution.Issue == nil {
		t.Fatalf("expected broken symlink with unknown entry type to surface an issue, got %+v", resolution)
	}
	if resolution.Issue.Path != filepath.ToSlash(brokenLinkPath) {
		t.Fatalf("expected issue path %q, got %+v", filepath.ToSlash(brokenLinkPath), resolution)
	}
	if resolution.Issue.Reason != "symlink target does not exist" {
		t.Fatalf("expected stable missing-target reason, got %+v", resolution)
	}
}

func TestSkillsContextMessageFailsOnUnreadableSkillsDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspace := t.TempDir()
	prev := readSkillsDir
	readSkillsDir = func(path string) ([]os.DirEntry, error) {
		if path == filepath.Join(workspace, brand.ConfigDirName, "skills") {
			return nil, os.ErrPermission
		}
		return prev(path)
	}
	t.Cleanup(func() {
		readSkillsDir = prev
	})

	_, _, err := skillsContextMessageWithDisabled(workspace, nil)
	if !errors.Is(err, errReadSkillsDirectory) {
		t.Fatalf("expected errReadSkillsDirectory, got %v", err)
	}
}

func TestSplitMetaContextMessagesSeparatesMetaContextWithoutDeduplication(t *testing.T) {
	skillsMessage := llm.Message{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeSkills, Content: "## Skills\n### Available skills"}
	messages := []llm.Message{
		skillsMessage,
		skillsMessage,
		{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeEnvironment, Content: environmentInjectedHeader + "\nOS: darwin"},
		{Role: llm.RoleUser, Content: "request"},
	}

	meta, transcript := splitMetaContextMessages(messages)
	if len(meta) != 3 {
		t.Fatalf("expected split to preserve duplicate meta candidates, got %d", len(meta))
	}
	if meta[0].MessageType != llm.MessageTypeSkills {
		t.Fatalf("expected first meta message to be skills context, got %+v", meta[0])
	}
	if meta[1].MessageType != llm.MessageTypeSkills {
		t.Fatalf("expected second meta message to remain duplicate skills context, got %+v", meta[1])
	}
	if meta[2].MessageType != llm.MessageTypeEnvironment {
		t.Fatalf("expected third meta message to be environment context, got %+v", meta[2])
	}
	if len(transcript) != 1 || transcript[0].Role != llm.RoleUser || transcript[0].Content != "request" {
		t.Fatalf("expected transcript to contain only user request, got %+v", transcript)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	rebuilt, err := appendMissingReviewerMetaContext(messages, t.TempDir(), "gpt-5", "high", "", false, nil)
	if err != nil {
		t.Fatalf("appendMissingReviewerMetaContext: %v", err)
	}
	if len(rebuilt) != 3 {
		t.Fatalf("expected builder to canonicalize duplicate meta messages, got %d", len(rebuilt))
	}
	if rebuilt[0].MessageType != llm.MessageTypeEnvironment || rebuilt[1].MessageType != llm.MessageTypeSkills {
		t.Fatalf("expected canonical environment -> skills ordering, got %+v", rebuilt)
	}
}

func TestSplitMetaContextMessagesTreatsHeadlessContextAsMeta(t *testing.T) {
	headless := llm.Message{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeHeadlessMode, Content: "headless mode instructions"}
	messages := []llm.Message{
		headless,
		headless,
		{Role: llm.RoleUser, Content: "request"},
	}

	meta, transcript := splitMetaContextMessages(messages)
	if len(meta) != 2 {
		t.Fatalf("expected split to preserve duplicate headless meta messages, got %d", len(meta))
	}
	if meta[0].MessageType != llm.MessageTypeHeadlessMode {
		t.Fatalf("expected headless meta message, got %+v", meta[0])
	}
	if len(transcript) != 1 || transcript[0].Role != llm.RoleUser || transcript[0].Content != "request" {
		t.Fatalf("expected transcript to contain only user request, got %+v", transcript)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	rebuilt, err := appendMissingReviewerMetaContext(messages, t.TempDir(), "gpt-5", "high", "", true, nil)
	if err != nil {
		t.Fatalf("appendMissingReviewerMetaContext: %v", err)
	}
	if len(rebuilt) != 3 {
		t.Fatalf("expected builder to reconstruct environment + headless + transcript, got %d", len(rebuilt))
	}
	if rebuilt[0].MessageType != llm.MessageTypeEnvironment || rebuilt[1].MessageType != llm.MessageTypeHeadlessMode {
		t.Fatalf("expected canonical environment -> headless ordering, got %+v", rebuilt)
	}
}

func TestSplitMetaContextMessagesTreatsHeadlessExitContextAsMeta(t *testing.T) {
	headlessExit := llm.Message{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeHeadlessModeExit, Content: "interactive mode instructions"}
	messages := []llm.Message{
		headlessExit,
		headlessExit,
		{Role: llm.RoleUser, Content: "request"},
	}

	meta, transcript := splitMetaContextMessages(messages)
	if len(meta) != 2 {
		t.Fatalf("expected split to preserve duplicate headless exit meta messages, got %d", len(meta))
	}
	if meta[0].MessageType != llm.MessageTypeHeadlessModeExit {
		t.Fatalf("expected headless exit meta message, got %+v", meta[0])
	}
	if len(transcript) != 1 || transcript[0].Role != llm.RoleUser || transcript[0].Content != "request" {
		t.Fatalf("expected transcript to contain only user request, got %+v", transcript)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	rebuilt, err := appendMissingReviewerMetaContext(messages, t.TempDir(), "gpt-5", "high", "", false, nil)
	if err != nil {
		t.Fatalf("appendMissingReviewerMetaContext: %v", err)
	}
	if len(rebuilt) != 3 {
		t.Fatalf("expected builder to reconstruct environment + headless exit + transcript, got %d", len(rebuilt))
	}
	if rebuilt[0].MessageType != llm.MessageTypeEnvironment || rebuilt[1].MessageType != llm.MessageTypeHeadlessModeExit {
		t.Fatalf("expected canonical environment -> headless exit ordering, got %+v", rebuilt)
	}
}

func TestGeneratedSkillIsDisabledBySkillToggle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspace := t.TempDir()
	writeTestSkill(t, filepath.Join(home, brand.ConfigDirName, ".generated", "skills", "skill-creator"), "skill-creator", "generated")

	content, found, err := skillsContextMessageWithDisabled(workspace, map[string]bool{"skill-creator": true})
	if err != nil {
		t.Fatalf("skillsContextMessageWithDisabled: %v", err)
	}
	if found {
		t.Fatalf("expected disabled generated skill to be omitted, got %q", content)
	}
}

func TestInspectSkillsMarksConfigDisabledSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspace := t.TempDir()
	writeTestSkill(t, filepath.Join(workspace, brand.ConfigDirName, "skills", "workspace-skill"), "Workspace Skill", "from workspace")

	inspections, err := InspectSkills(workspace, "", map[string]bool{"workspace skill": true})
	if err != nil {
		t.Fatalf("InspectSkills: %v", err)
	}
	if len(inspections) != 1 {
		t.Fatalf("expected one inspection, got %d", len(inspections))
	}
	if !inspections[0].Loaded {
		t.Fatalf("expected skill to stay loadable, got %+v", inspections[0])
	}
	if !inspections[0].Disabled {
		t.Fatalf("expected skill to be marked disabled, got %+v", inspections[0])
	}
}

func TestInspectSkillsMarksGeneratedShadowedAndDisabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspace := t.TempDir()
	writeTestSkill(t, filepath.Join(workspace, brand.ConfigDirName, "skills", "skill-creator"), "skill-creator", "workspace")
	generatedPath := writeTestSkill(t, filepath.Join(home, brand.ConfigDirName, ".generated", "skills", "skill-creator"), "skill-creator", "generated")

	inspections, err := InspectSkills(workspace, "", map[string]bool{"skill-creator": true})
	if err != nil {
		t.Fatalf("InspectSkills: %v", err)
	}
	var generatedInspection *SkillInspection
	for idx := range inspections {
		if inspections[idx].Path == filepath.ToSlash(generatedPath) {
			generatedInspection = &inspections[idx]
			break
		}
	}
	if generatedInspection == nil {
		t.Fatalf("expected generated inspection, got %+v", inspections)
	}
	if generatedInspection.SourceKind != string(skillSourceGenerated) || !generatedInspection.Shadowed || !generatedInspection.Disabled {
		t.Fatalf("expected generated skill to be shadowed and disabled, got %+v", *generatedInspection)
	}
}

func TestInspectSkillsLoadsSymlinkedSkillDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspace := t.TempDir()
	targetSkillPath := writeTestSkill(t, filepath.Join(t.TempDir(), "shared-skills", "linked-skill"), "linked-skill", "from symlink")
	linkPath := filepath.Join(workspace, brand.ConfigDirName, "skills", "linked-skill")
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatalf("mkdir symlink parent: %v", err)
	}
	if err := os.Symlink(filepath.Dir(targetSkillPath), linkPath); err != nil {
		t.Fatalf("symlink skill dir: %v", err)
	}

	inspections, err := InspectSkills(workspace, "", nil)
	if err != nil {
		t.Fatalf("InspectSkills: %v", err)
	}
	if len(inspections) != 1 {
		t.Fatalf("expected one inspection, got %d", len(inspections))
	}
	if !inspections[0].Loaded {
		t.Fatalf("expected symlinked skill inspection to load, got %+v", inspections[0])
	}
	if inspections[0].Path != filepath.ToSlash(targetSkillPath) {
		t.Fatalf("expected inspection path %q, got %+v", filepath.ToSlash(targetSkillPath), inspections[0])
	}
}

func TestInspectSkillsReportsBrokenSymlinkedSkillDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspace := t.TempDir()
	brokenLinkPath := filepath.Join(workspace, brand.ConfigDirName, "skills", "broken-skill")
	if err := os.MkdirAll(filepath.Dir(brokenLinkPath), 0o755); err != nil {
		t.Fatalf("mkdir broken symlink parent: %v", err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing-skill-dir"), brokenLinkPath); err != nil {
		t.Fatalf("symlink broken skill dir: %v", err)
	}

	inspections, err := InspectSkills(workspace, "", nil)
	if err != nil {
		t.Fatalf("InspectSkills: %v", err)
	}
	if len(inspections) != 1 {
		t.Fatalf("expected one inspection, got %d", len(inspections))
	}
	if inspections[0].Loaded {
		t.Fatalf("expected broken symlinked skill inspection to fail, got %+v", inspections[0])
	}
	brokenSkillPath := filepath.ToSlash(filepath.Join(brokenLinkPath, skillFileName))
	if inspections[0].Path != brokenSkillPath {
		t.Fatalf("expected inspection path %q, got %+v", brokenSkillPath, inspections[0])
	}
	if inspections[0].Reason != "symlink target does not exist" {
		t.Fatalf("expected missing target reason, got %+v", inspections[0])
	}
}

func writeTestSkill(t *testing.T, dir string, name string, description string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	skillPath := filepath.Join(dir, skillFileName)
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# Body\n"
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}
	if canonical, err := filepath.EvalSymlinks(skillPath); err == nil {
		return canonical
	}
	return skillPath
}

type fakeDirEntry struct {
	name string
}

func (f fakeDirEntry) Name() string             { return f.name }
func (fakeDirEntry) IsDir() bool                { return false }
func (fakeDirEntry) Type() fs.FileMode          { return 0 }
func (fakeDirEntry) Info() (fs.FileInfo, error) { return nil, fs.ErrNotExist }
