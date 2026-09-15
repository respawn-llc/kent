package worktreecontract

import (
	"strings"
	"unicode"
)

// SanitizeBranchSuggestion returns no suggestion when the title has no letters or digits.
func SanitizeBranchSuggestion(raw string) *string {
	var builder strings.Builder
	separator := false
	for _, r := range strings.ToLower(raw) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if separator {
				builder.WriteRune('-')
			}
			builder.WriteRune(r)
			separator = false
		} else if builder.Len() > 0 {
			separator = true
		}
	}
	if builder.Len() == 0 {
		return nil
	}
	result := builder.String()
	return &result
}
