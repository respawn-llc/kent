package transcript

import "strings"

type EntryVisibility string

const (
	EntryVisibilityAuto       EntryVisibility = ""
	EntryVisibilityAll        EntryVisibility = "all"
	EntryVisibilityVerbose EntryVisibility = "verbose"
)

func NormalizeEntryVisibility(visibility EntryVisibility) EntryVisibility {
	switch strings.ToLower(strings.TrimSpace(string(visibility))) {
	case "", "auto":
		return EntryVisibilityAuto
	case string(EntryVisibilityAll):
		return EntryVisibilityAll
	case string(EntryVisibilityVerbose):
		return EntryVisibilityVerbose
	default:
		return EntryVisibility(strings.TrimSpace(string(visibility)))
	}
}
