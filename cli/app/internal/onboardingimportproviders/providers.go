package onboardingimportproviders

import (
	"path/filepath"
	"sort"
	"strings"

	"core/cli/app/internal/onboardingimportchoice"
	"core/cli/app/internal/onboardingimportfs"
)

type ProviderID = onboardingimportchoice.ProviderID
type Provider = onboardingimportfs.Provider

const (
	ClaudeCode ProviderID = "claude_code"
	Codex      ProviderID = "codex"
	Agents     ProviderID = "agents"
)

func Supported() []Provider {
	return []Provider{
		{ID: ClaudeCode, Label: "Claude Code", HomeEntry: ".claude", SkillSourceCandidates: []string{"skills"}, SupportsCommandImport: true},
		{ID: Codex, Label: "Codex", HomeEntry: ".codex", SkillSourceCandidates: []string{filepath.Join("skills", "local"), "skills"}, SupportsCommandImport: true},
		{ID: Agents, Label: "Agents", HomeEntry: ".agents", SkillSourceCandidates: []string{"skills"}, SupportsCommandImport: true},
	}
}

func SkillSupported() []Provider {
	providers := Supported()
	filtered := make([]Provider, 0, len(providers))
	for _, provider := range providers {
		if len(provider.SkillSourceCandidates) == 0 {
			continue
		}
		filtered = append(filtered, provider)
	}
	return filtered
}

func CommandSupported() []Provider {
	providers := Supported()
	filtered := make([]Provider, 0, len(providers))
	for _, provider := range providers {
		if !provider.SupportsCommandImport {
			continue
		}
		filtered = append(filtered, provider)
	}
	return filtered
}

func ByID(providerID ProviderID) (Provider, bool) {
	for _, provider := range Supported() {
		if provider.ID == providerID {
			return provider, true
		}
	}
	return Provider{}, false
}

func Label(providerID ProviderID) string {
	if supported, ok := ByID(providerID); ok {
		return supported.Label
	}
	return string(providerID)
}

func Order(providerID ProviderID) int {
	for index, provider := range Supported() {
		if provider.ID == providerID {
			return index
		}
	}
	return len(Supported())
}

func OrderList() []ProviderID {
	providers := Supported()
	order := make([]ProviderID, 0, len(providers))
	for _, provider := range providers {
		order = append(order, provider.ID)
	}
	return order
}

func Labels(providers []Provider) string {
	labels := make([]string, 0, len(providers))
	for _, provider := range providers {
		labels = append(labels, provider.Label)
	}
	return strings.Join(labels, ", ")
}

func SortedProviderIDs[T any](byProvider map[ProviderID][]T) []ProviderID {
	providers := make([]ProviderID, 0, len(byProvider))
	for provider := range byProvider {
		providers = append(providers, provider)
	}
	sort.Slice(providers, func(i, j int) bool {
		leftOrder := Order(providers[i])
		rightOrder := Order(providers[j])
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		return providers[i] < providers[j]
	})
	return providers
}
