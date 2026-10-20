package textutil

import (
	"strings"
	"unicode/utf8"
)

// MiddleTruncate bounds a copy in runes, keeping the beginning and ending.
func MiddleTruncate(text string, limit int) string {
	if limit < 0 {
		panic("negative middle-truncation limit")
	}
	if utf8.RuneCountInString(text) <= limit {
		return strings.Clone(text)
	}
	if limit == 0 {
		return ""
	}
	leftRunes := limit / 2
	rightRunes := limit - leftRunes - 1
	left := 0
	for range leftRunes {
		_, size := utf8.DecodeRuneInString(text[left:])
		left += size
	}
	right := len(text)
	for range rightRunes {
		_, size := utf8.DecodeLastRuneInString(text[:right])
		right -= size
	}
	return text[:left] + "…" + text[right:]
}
