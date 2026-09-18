package runtime

import (
	"fmt"
	"path/filepath"
	"strings"

	"core/prompts"
	"core/server/skillcatalog"
	"core/shared/pathutil"
)

const skillsAvailableHeader = "Available skills:"

var skillsPrompt = strings.TrimSpace(prompts.SkillsPrompt)

func formatSkillDiscoveryWarning(issue skillcatalog.Issue, cwd, home string) string {
	name := strings.TrimSpace(issue.Name)
	if name == "" {
		name = filepath.Base(strings.TrimSpace(issue.Path))
	}
	if strings.TrimSpace(issue.Path) == "" {
		return fmt.Sprintf("Skipped skill %q: %s", name, issue.Reason)
	}
	return fmt.Sprintf("Skipped skill %q at %s: %s", name, pathutil.Compact(issue.Path, cwd, home), issue.Reason)
}

func renderSkillsContext(skills []skillcatalog.Skill, cwd, home string) string {
	lines := make([]string, 0, len(skills)+2)
	lines = append(lines, skillsPrompt)
	lines = append(lines, skillsAvailableHeader)
	for _, skill := range skills {
		lines = append(lines, fmt.Sprintf("- %s: %s . %s", skill.Name, pathutil.Compact(skill.Path, cwd, home), skill.Description))
	}
	return strings.Join(lines, "\n")
}
