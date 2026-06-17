package status

import (
	"core/server/runtime"
	"core/shared/textutil"
	"os"
	"strings"
)

func SkillInspectionsFromRuntime(skills []runtime.SkillInspection) []SkillInspection {
	if len(skills) == 0 {
		return nil
	}
	converted := make([]SkillInspection, 0, len(skills))
	for _, skill := range skills {
		converted = append(converted, SkillInspection{
			Name:        skill.Name,
			Description: skill.Description,
			Path:        skill.Path,
			SourceKind:  skill.SourceKind,
			Loaded:      skill.Loaded,
			Disabled:    skill.Disabled,
			Shadowed:    skill.Shadowed,
			Reason:      skill.Reason,
		})
	}
	return converted
}

func EstimateSkillTokens(skills []SkillInspection) map[string]int {
	paths := make([]string, 0, len(skills))
	for _, skill := range skills {
		if !skill.Loaded || skill.Disabled {
			continue
		}
		path := strings.TrimSpace(skill.Path)
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	return EstimatePathTokens(paths)
}

func EstimatePathTokens(paths []string) map[string]int {
	counts := map[string]int{}
	for _, rawPath := range paths {
		path := strings.TrimSpace(rawPath)
		if path == "" {
			continue
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		counts[path] = textutil.ApproxTextTokenCount(string(contents))
	}
	return counts
}
