package onboardingimportchoice

import (
	"errors"
	"fmt"
	"strings"
)

type ProviderID string

type Mode string

const (
	ModeNone          Mode = "none"
	ModeSymlinkSource Mode = "symlink_source"
)

type Selection struct {
	Mode     Mode
	Provider ProviderID
}

func NormalizeSelection(selection Selection) Selection {
	if strings.TrimSpace(string(selection.Mode)) == "" {
		selection.Mode = ModeNone
	}
	return selection
}

func ApplyChoice(selection Selection, choiceID string) (Selection, error) {
	if strings.TrimSpace(choiceID) == "" {
		return Selection{}, errors.New("invalid import choice")
	}
	parts := strings.Split(choiceID, ":")
	switch parts[0] {
	case "none":
		return Selection{Mode: ModeNone}, nil
	case "symlink":
		if len(parts) != 2 {
			return Selection{}, errors.New("invalid provider symlink choice")
		}
		return Selection{Mode: ModeSymlinkSource, Provider: ProviderID(parts[1])}, nil
	default:
		return Selection{}, fmt.Errorf("unknown import choice %q", choiceID)
	}
}

func RecommendedSymlinkChoiceID[T any](byProvider map[ProviderID][]T, providerOrder []ProviderID) string {
	provider, ok := ProviderWithMostItems(byProvider, providerOrder)
	if !ok {
		return "none"
	}
	return "symlink:" + string(provider)
}

func ProviderWithMostItems[T any](byProvider map[ProviderID][]T, providerOrder []ProviderID) (ProviderID, bool) {
	bestProvider := ProviderID("")
	bestCount := 0
	found := false
	for provider, items := range byProvider {
		count := len(items)
		if count == 0 {
			continue
		}
		if !found || count > bestCount || count == bestCount && providerRank(provider, providerOrder) < providerRank(bestProvider, providerOrder) {
			bestProvider = provider
			bestCount = count
			found = true
			continue
		}
		if count == bestCount && providerRank(provider, providerOrder) == providerRank(bestProvider, providerOrder) && provider < bestProvider {
			bestProvider = provider
		}
	}
	return bestProvider, found
}

func providerRank(provider ProviderID, providerOrder []ProviderID) int {
	for index, ordered := range providerOrder {
		if ordered == provider {
			return index
		}
	}
	return len(providerOrder)
}
