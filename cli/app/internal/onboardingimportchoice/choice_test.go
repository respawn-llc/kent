package onboardingimportchoice

import "testing"

func TestApplyChoice(t *testing.T) {
	selected, err := ApplyChoice(Selection{Mode: ModeSymlinkSource, Provider: "codex"}, "none")
	if err != nil {
		t.Fatalf("apply none: %v", err)
	}
	if selected.Mode != ModeNone || selected.Provider != "" {
		t.Fatalf("none selection = %+v", selected)
	}

	selected, err = ApplyChoice(Selection{}, "symlink:claude_code")
	if err != nil {
		t.Fatalf("apply symlink: %v", err)
	}
	if selected.Mode != ModeSymlinkSource || selected.Provider != "claude_code" {
		t.Fatalf("symlink selection = %+v", selected)
	}
}

func TestApplyChoiceRejectsInvalidChoice(t *testing.T) {
	for _, choiceID := range []string{"", "symlink", "copy:claude_code"} {
		if _, err := ApplyChoice(Selection{}, choiceID); err == nil {
			t.Fatalf("expected %q to be rejected", choiceID)
		}
	}
}

func TestRecommendedSymlinkChoiceIDUsesCountThenProviderOrder(t *testing.T) {
	order := []ProviderID{"claude_code", "codex", "agents"}
	if got := RecommendedSymlinkChoiceID(map[ProviderID][]int{
		"codex":       {1, 2},
		"agents":      {1, 2, 3},
		"claude_code": {1},
	}, order); got != "symlink:agents" {
		t.Fatalf("choice = %q, want agents", got)
	}
	if got := RecommendedSymlinkChoiceID(map[ProviderID][]int{
		"codex":       {1, 2},
		"claude_code": {1, 2},
	}, order); got != "symlink:claude_code" {
		t.Fatalf("tie choice = %q, want claude_code", got)
	}
	if got := RecommendedSymlinkChoiceID(map[ProviderID][]int{"codex": nil}, order); got != "none" {
		t.Fatalf("empty choice = %q, want none", got)
	}
}

func TestNormalizeSelectionDefaultsEmptyModeToNone(t *testing.T) {
	got := NormalizeSelection(Selection{})
	if got.Mode != ModeNone {
		t.Fatalf("mode = %q, want none", got.Mode)
	}
}
