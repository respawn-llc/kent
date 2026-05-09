package onboardingimport

import (
	"strings"

	"builder/server/runtime"
)

type SkillMetadata struct {
	Name string
}

func ParseSkillMetadata(path string) (SkillMetadata, bool) {
	meta, ok := runtime.ParseSkillMetadata(path)
	if !ok {
		return SkillMetadata{}, false
	}
	return SkillMetadata{Name: strings.TrimSpace(meta.Name)}, true
}
