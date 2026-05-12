package onboardingimport

import (
	"strings"

	"builder/cli/app/internal/serverbridge"
)

type SkillMetadata struct {
	Name string
}

func ParseSkillMetadata(path string) (SkillMetadata, bool) {
	meta, ok := serverbridge.ParseSkillMetadata(path)
	if !ok {
		return SkillMetadata{}, false
	}
	return SkillMetadata{Name: strings.TrimSpace(meta.Name)}, true
}
