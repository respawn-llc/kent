package skillcatalog

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"core/prompts"
	brand "core/shared/config"

	"gopkg.in/yaml.v3"
)

const (
	SkillsDirName = "skills"
	SkillFileName = "SKILL.md"
)

type SourceKind string

const (
	SourceKindGlobal    SourceKind = "global"
	SourceKindWorkspace SourceKind = "workspace"
	SourceKindGenerated SourceKind = "generated"
)

type Options struct {
	WorkspaceRoot            string
	ConfigRoot               string
	Policy                   brand.SkillPolicy
	IncludeEmbeddedGenerated bool
}

type Issue struct {
	Name       string
	Path       string
	SourceKind SourceKind
	Reason     string
}

type FailureKind uint8

const (
	FailureDirectory FailureKind = iota + 1
	FailureFile
	FailureDuplicate
)

type Failure struct {
	Kind   FailureKind
	Reason string
}

type Inspection struct {
	Name        string
	Description string
	Path        string
	SourceKind  SourceKind
	Disabled    bool
	Shadowed    bool
	Failure     *Failure
}

func (i Inspection) Loaded() bool {
	return i.Failure == nil
}

type Result struct {
	Inspections []Inspection
}

func (r Result) PromptSkills() []Inspection {
	skills := make([]Inspection, 0, len(r.Inspections))
	for _, inspection := range r.Inspections {
		if inspection.Loaded() && !inspection.Disabled && !inspection.Shadowed {
			skills = append(skills, inspection)
		}
	}
	return skills
}

func (r Result) DiscoveryWarnings() []Issue {
	var issues []Issue
	for _, inspection := range r.Inspections {
		if inspection.Failure != nil && inspection.Failure.Kind == FailureDirectory {
			issues = append(issues, Issue{
				Name: inspection.Name, Path: filepath.ToSlash(filepath.Dir(inspection.Path)),
				SourceKind: inspection.SourceKind, Reason: inspection.Failure.Reason,
			})
		}
	}
	return issues
}

func (r Result) StatusInspections() []Inspection {
	inspections := append([]Inspection(nil), r.Inspections...)
	sort.Slice(inspections, func(i, j int) bool {
		if inspections[i].Shadowed != inspections[j].Shadowed {
			return !inspections[i].Shadowed && inspections[j].Shadowed
		}
		if inspections[i].Disabled != inspections[j].Disabled {
			return !inspections[i].Disabled && inspections[j].Disabled
		}
		if inspections[i].Loaded() != inspections[j].Loaded() {
			return inspections[i].Loaded() && !inspections[j].Loaded()
		}
		return inspections[i].Path < inspections[j].Path
	})
	return inspections
}

type Root struct {
	Path string
	Kind SourceKind
}

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func Discover(opts Options) (Result, error) {
	roots, err := Roots(opts.WorkspaceRoot, opts.ConfigRoot)
	if err != nil {
		return Result{}, err
	}
	inspections := make([]Inspection, 0)
	seenLoadedPaths := map[string]bool{}
	userSkillNames := map[string]bool{}
	for _, root := range roots {
		rootInspections, err := discoverRoot(root, opts.IncludeEmbeddedGenerated)
		if err != nil {
			return Result{}, err
		}
		for _, inspection := range rootInspections {
			if inspection.Loaded() {
				nameKey := brand.NormalizeSkillName(inspection.Name)
				if !opts.Policy.SkillEnabled(nameKey) {
					inspection.Disabled = true
				}
				if seenLoadedPaths[inspection.Path] {
					inspection.Disabled = false
					inspection.Failure = &Failure{Kind: FailureDuplicate, Reason: "duplicate resolved SKILL.md path"}
				} else {
					seenLoadedPaths[inspection.Path] = true
				}
				if inspection.Loaded() && root.Kind != SourceKindGenerated {
					userSkillNames[nameKey] = true
				}
			}
			inspections = append(inspections, inspection)
		}
	}
	for idx := range inspections {
		if inspections[idx].Loaded() && inspections[idx].SourceKind == SourceKindGenerated && userSkillNames[brand.NormalizeSkillName(inspections[idx].Name)] {
			inspections[idx].Shadowed = true
		}
	}
	return Result{Inspections: inspections}, nil
}

func DiscoverGenerated(configRoot string, policy brand.SkillPolicy) ([]Inspection, error) {
	layout, err := prompts.GeneratedLayoutFor(configRoot)
	if err != nil {
		return nil, err
	}
	inspections, err := discoverEmbeddedGenerated(layout.GeneratedSkillsRoot)
	if err != nil {
		return nil, err
	}
	for idx := range inspections {
		if !policy.SkillEnabled(inspections[idx].Name) {
			inspections[idx].Disabled = true
		}
	}
	return inspections, nil
}

func Roots(workspaceRoot, configRoot string) ([]Root, error) {
	layout, err := prompts.GeneratedLayoutFor(configRoot)
	if err != nil {
		return nil, err
	}
	roots := make([]Root, 0, 3)
	seen := map[string]bool{}
	add := func(path string, kind SourceKind) {
		cleaned := filepath.Clean(path)
		if cleaned == "." || cleaned == "" || seen[cleaned] {
			return
		}
		seen[cleaned] = true
		roots = append(roots, Root{Path: cleaned, Kind: kind})
	}
	add(layout.UserSkillsRoot, SourceKindGlobal)
	if strings.TrimSpace(workspaceRoot) != "" {
		add(filepath.Join(workspaceRoot, brand.ConfigDirName, SkillsDirName), SourceKindWorkspace)
	}
	add(layout.GeneratedSkillsRoot, SourceKindGenerated)
	return roots, nil
}

func discoverRoot(root Root, includeEmbeddedGenerated bool) ([]Inspection, error) {
	if root.Kind == SourceKindGenerated && includeEmbeddedGenerated {
		return discoverEmbeddedGenerated(root.Path)
	}
	entries, err := os.ReadDir(root.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w %q: %w", ErrReadSkillsDirectory, root.Path, err)
	}
	inspections := make([]Inspection, 0)
	for _, entry := range entries {
		resolution := resolveSkillDir(root.Path, entry)
		if resolution.Failure != nil {
			inspections = append(inspections, Inspection{
				Name: sanitizeSkillSingleLine(entry.Name()), Path: filepath.ToSlash(filepath.Join(root.Path, entry.Name(), SkillFileName)),
				SourceKind: root.Kind, Failure: resolution.Failure,
			})
		}
		if !resolution.Discoverable {
			continue
		}
		skillPath := filepath.Join(resolution.SkillDir, SkillFileName)
		inspection := InspectSkillAtPath(entry.Name(), skillPath)
		inspection.SourceKind = root.Kind
		inspections = append(inspections, inspection)
	}
	return inspections, nil
}

func discoverEmbeddedGenerated(projectedRoot string) ([]Inspection, error) {
	entries, err := fs.ReadDir(prompts.GeneratedSkillsFS, SkillsDirName)
	if err != nil {
		return nil, fmt.Errorf("%w embedded generated skills: %w", ErrReadSkillsDirectory, err)
	}
	inspections := make([]Inspection, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		embeddedPath := filepath.ToSlash(filepath.Join(SkillsDirName, entry.Name(), SkillFileName))
		contents, err := fs.ReadFile(prompts.GeneratedSkillsFS, embeddedPath)
		if err != nil {
			continue
		}
		projectedPath := filepath.ToSlash(filepath.Join(projectedRoot, entry.Name(), SkillFileName))
		inspection := inspectSkillContents(entry.Name(), projectedPath, string(contents))
		inspection.SourceKind = SourceKindGenerated
		inspections = append(inspections, inspection)
	}
	return inspections, nil
}

type skillDirResolution struct {
	SkillDir     string
	Discoverable bool
	Failure      *Failure
}

func resolveSkillDir(root string, entry os.DirEntry) skillDirResolution {
	skillDir := filepath.Join(root, entry.Name())
	info, err := os.Lstat(skillDir)
	if err != nil {
		return skillDirResolution{Failure: &Failure{Kind: FailureDirectory, Reason: formatSkillDirResolutionFailure(err)}}
	}
	if info.IsDir() {
		return skillDirResolution{SkillDir: skillDir, Discoverable: true}
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return skillDirResolution{}
	}
	targetInfo, err := os.Stat(skillDir)
	if err != nil {
		return skillDirResolution{Failure: &Failure{Kind: FailureDirectory, Reason: formatSkillDirResolutionFailure(err)}}
	}
	if targetInfo.IsDir() {
		return skillDirResolution{SkillDir: skillDir, Discoverable: true}
	}
	return skillDirResolution{Failure: &Failure{Kind: FailureDirectory, Reason: "symlink target is not a directory"}}
}

func formatSkillDirResolutionFailure(err error) string {
	if os.IsNotExist(err) {
		return "symlink target does not exist"
	}
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return strings.TrimSpace(pathErr.Err.Error())
	}
	return strings.TrimSpace(err.Error())
}

func InspectSkillAtPath(fallbackName, skillPath string) Inspection {
	resolvedPath := filepath.ToSlash(skillPath)
	if canonical, err := filepath.EvalSymlinks(skillPath); err == nil {
		resolvedPath = filepath.ToSlash(canonical)
	}
	contents, err := os.ReadFile(skillPath)
	if err != nil {
		reason := "could not read SKILL.md"
		if os.IsNotExist(err) {
			reason = "missing SKILL.md"
		}
		return Inspection{Name: sanitizeSkillSingleLine(fallbackName), Path: resolvedPath, Failure: &Failure{Kind: FailureFile, Reason: reason}}
	}
	return inspectSkillContents(fallbackName, resolvedPath, string(contents))
}

func inspectSkillContents(fallbackName, resolvedPath, contents string) Inspection {
	frontmatter, ok := extractSkillFrontmatter(contents)
	if !ok {
		return Inspection{Name: sanitizeSkillSingleLine(fallbackName), Path: resolvedPath, Failure: &Failure{Kind: FailureFile, Reason: "missing or invalid frontmatter"}}
	}
	var parsed skillFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatter), &parsed); err != nil {
		return Inspection{Name: sanitizeSkillSingleLine(fallbackName), Path: resolvedPath, Failure: &Failure{Kind: FailureFile, Reason: "invalid frontmatter YAML"}}
	}
	name := sanitizeSkillSingleLine(parsed.Name)
	if name == "" {
		name = sanitizeSkillSingleLine(fallbackName)
	}
	description := sanitizeSkillSingleLine(parsed.Description)
	if name == "" || description == "" {
		return Inspection{Name: name, Path: resolvedPath, Failure: &Failure{Kind: FailureFile, Reason: "missing name or description"}}
	}
	return Inspection{Name: name, Description: description, Path: resolvedPath}
}

func extractSkillFrontmatter(contents string) (string, bool) {
	lines := strings.Split(contents, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", false
	}
	frontmatterLines := make([]string, 0)
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			return strings.Join(frontmatterLines, "\n"), len(frontmatterLines) > 0
		}
		frontmatterLines = append(frontmatterLines, line)
	}
	return "", false
}

func sanitizeSkillSingleLine(raw string) string {
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}
