package worktreecontract

import "testing"

func TestSanitizeBranchSuggestion(t *testing.T) {
	if got := SanitizeBranchSuggestion(" Fix: My Feature!! "); got == nil || *got != "fix-my-feature" {
		t.Fatalf("suggestion = %v, want fix-my-feature", got)
	}
	if got := SanitizeBranchSuggestion(" -- !! "); got != nil {
		t.Fatalf("unusable title suggestion = %v, want absent", got)
	}
}
