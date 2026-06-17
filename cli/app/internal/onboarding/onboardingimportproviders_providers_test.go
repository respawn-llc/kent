package onboarding

import (
	"path/filepath"
	"testing"
)

func TestSupportedProviderCatalogPreservesOrderAndCapabilities(t *testing.T) {
	providers := Supported()
	if len(providers) != 3 {
		t.Fatalf("providers = %+v", providers)
	}
	if providers[0].ID != ClaudeCode || providers[1].ID != Codex || providers[2].ID != Agents {
		t.Fatalf("provider order = %+v", providers)
	}
	codex := providers[1]
	if codex.HomeEntry != ".codex" || len(codex.SkillSourceCandidates) != 2 || codex.SkillSourceCandidates[0] != filepath.Join("skills", "local") || !codex.SupportsCommandImport {
		t.Fatalf("codex provider = %+v", codex)
	}
}

func TestProviderLookupLabelsAndOrdering(t *testing.T) {
	provider, ok := ByID(Codex)
	if !ok || provider.Label != "Codex" {
		t.Fatalf("codex lookup = %+v ok=%t", provider, ok)
	}
	if Label("unknown") != "unknown" {
		t.Fatalf("unknown label should fall back to id")
	}
	if Labels([]Provider{{Label: "Claude Code"}, {Label: "Codex"}}) != "Claude Code, Codex" {
		t.Fatalf("labels did not preserve display text")
	}
	if Order(ClaudeCode) >= Order(Codex) || Order("unknown") <= Order(Agents) {
		t.Fatalf("unexpected order: claude=%d codex=%d agents=%d unknown=%d", Order(ClaudeCode), Order(Codex), Order(Agents), Order("unknown"))
	}
}

func TestSortedProviderIDsUsesCatalogOrderThenStableFallback(t *testing.T) {
	got := SortedProviderIDs(map[ProviderID][]int{
		"zeta":       {1},
		Codex:        {1},
		ClaudeCode:   {1},
		"alpha":      {1},
		Agents:       {1},
		"emptyKnown": nil,
	})
	want := []ProviderID{ClaudeCode, Codex, Agents, "alpha", "emptyKnown", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("sorted providers = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("sorted providers = %v, want %v", got, want)
		}
	}
}
