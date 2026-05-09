package onboardingimportskills

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"builder/cli/app/internal/onboardingimportchoice"
)

type Item struct {
	ID                  string
	Provider            onboardingimportchoice.ProviderID
	ProviderLabel       string
	SourceDir           string
	TargetDirName       string
	SkillName           string
	ModifiedAt          time.Time
	DuplicateSourceNote string
}

func Candidates(imported []Item, generated []Item, existingSkillNames map[string]bool) []Item {
	items := make([]Item, 0, len(imported)+len(generated))
	shadowingNames := cloneBoolMap(existingSkillNames)
	items = append(items, imported...)
	for _, item := range imported {
		if normalized := NormalizeName(item.SkillName); normalized != "" {
			shadowingNames[normalized] = true
		}
	}
	for _, item := range generated {
		if shadowingNames[NormalizeName(item.SkillName)] {
			continue
		}
		items = append(items, item)
	}
	return AnnotateDuplicateSources(items)
}

func ToggleAllTitle(items []Item, selection map[string]bool) string {
	if AllSelected(items, selection) {
		return "Disable all"
	}
	return "Enable all"
}

func AllSelected(items []Item, selection map[string]bool) bool {
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		if !selection[item.ID] {
			return false
		}
	}
	return true
}

func AnnotateDuplicateSources(items []Item) []Item {
	if len(items) == 0 {
		return nil
	}
	out := append([]Item(nil), items...)
	groups := groupByTargetDirName(out)
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		for index, item := range group {
			opponents := make([]string, 0, len(group)-1)
			for opponentIndex, opponent := range group {
				if index == opponentIndex {
					continue
				}
				label := opponent.ProviderLabel
				if strings.TrimSpace(label) == strings.TrimSpace(item.ProviderLabel) {
					label = filepath.Base(opponent.SourceDir)
				}
				opponents = append(opponents, label)
			}
			outIndex := indexOfItem(out, item.ID)
			if outIndex >= 0 {
				out[outIndex].DuplicateSourceNote = strings.Join(uniqueStrings(opponents), ", ")
			}
		}
	}
	return out
}

func SanitizeName(raw string) string {
	return strings.Join(strings.Fields(raw), " ")
}

func NormalizeName(raw string) string {
	return strings.ToLower(SanitizeName(raw))
}

func cloneBoolMap(values map[string]bool) map[string]bool {
	cloned := make(map[string]bool, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func groupByTargetDirName(items []Item) map[string][]Item {
	groups := map[string][]Item{}
	for _, item := range items {
		key := strings.ToLower(strings.TrimSpace(item.TargetDirName))
		groups[key] = append(groups[key], item)
	}
	return groups
}

func indexOfItem(items []Item, id string) int {
	for index, item := range items {
		if item.ID == id {
			return index
		}
	}
	return -1
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
