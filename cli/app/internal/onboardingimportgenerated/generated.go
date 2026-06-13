package onboardingimportgenerated

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"core/cli/app/internal/onboardingimportskills"
	"core/prompts"
	"core/shared/brand"

	"gopkg.in/yaml.v3"
)

type Item = onboardingimportskills.Item

type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func Discover() ([]Item, error) {
	entries, err := fs.ReadDir(prompts.GeneratedSkillsFS, "skills")
	if err != nil {
		return nil, fmt.Errorf("read generated skills: %w", err)
	}
	items := make([]Item, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirName := entry.Name()
		skillPath := filepath.ToSlash(filepath.Join("skills", dirName, "SKILL.md"))
		contents, readErr := fs.ReadFile(prompts.GeneratedSkillsFS, skillPath)
		if readErr != nil {
			return nil, fmt.Errorf("read generated skill %s: %w", skillPath, readErr)
		}
		name, ok := ParseName(dirName, string(contents))
		if !ok {
			return nil, fmt.Errorf("generated skill %s has invalid frontmatter", skillPath)
		}
		items = append(items, Item{
			ID:            "generated:" + dirName,
			ProviderLabel: "Preinstalled",
			SourceDir:     filepath.ToSlash(filepath.Join("~", brand.ConfigDirName, ".generated", "skills", dirName)),
			TargetDirName: dirName,
			SkillName:     name,
		})
	}
	return items, nil
}

func ParseName(fallbackName, contents string) (string, bool) {
	raw, ok := SplitFrontmatter(contents)
	if !ok {
		return "", false
	}
	var parsed frontmatter
	if err := yaml.Unmarshal([]byte(raw), &parsed); err != nil {
		return "", false
	}
	name := strings.Join(strings.Fields(parsed.Name), " ")
	if name == "" {
		name = strings.Join(strings.Fields(fallbackName), " ")
	}
	if name == "" || strings.Join(strings.Fields(parsed.Description), " ") == "" {
		return "", false
	}
	return name, true
}

func SplitFrontmatter(contents string) (string, bool) {
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
