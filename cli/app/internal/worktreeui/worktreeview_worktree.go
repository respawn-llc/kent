package worktreeui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"core/shared/serverapi"
)

// ErrMainWorkspaceNotDeletable is returned when deletion targets the current
// main workspace. Callers and tests match this with errors.Is rather than
// comparing rendered message text.
var ErrMainWorkspaceNotDeletable = errors.New("main workspace is not deletable")

func DisplayName(item serverapi.WorktreeView) string {
	if trimmed := strings.TrimSpace(item.DisplayName); trimmed != "" {
		return trimmed
	}
	if item.IsMain {
		return "main"
	}
	if trimmed := strings.TrimSpace(item.BranchName); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(item.CanonicalRoot); trimmed != "" {
		return filepath.Base(trimmed)
	}
	return strings.TrimSpace(item.WorktreeID)
}

func SanitizeBranchSuggestion(raw string) string {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	if trimmed == "" {
		return ""
	}
	var builder strings.Builder
	lastDash := false
	for _, r := range trimmed {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			builder.WriteRune(r)
			lastDash = false
		case r == '/' || r == '-' || r == '_':
			if builder.Len() == 0 || lastDash {
				continue
			}
			builder.WriteRune('-')
			lastDash = true
		default:
			if builder.Len() == 0 || lastDash {
				continue
			}
			builder.WriteRune('-')
			lastDash = true
		}
	}
	result := strings.Trim(builder.String(), "-/")
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	return result
}

func DeleteCanAutoDeleteBranch(item serverapi.WorktreeView) bool {
	return item.Managed && item.CreatedBranch && strings.TrimSpace(item.BranchName) != ""
}

func ResolveDeletionTarget(entries []serverapi.WorktreeView, token string) (serverapi.WorktreeView, error) {
	trimmedToken := strings.TrimSpace(token)
	if trimmedToken != "" {
		return ResolveToken(entries, trimmedToken)
	}
	for _, item := range entries {
		if item.IsCurrent {
			if item.IsMain {
				return serverapi.WorktreeView{}, fmt.Errorf("%w; choose another worktree", ErrMainWorkspaceNotDeletable)
			}
			return item, nil
		}
	}
	return serverapi.WorktreeView{}, serverapi.ErrWorktreeNotFound
}

func ResolveToken(entries []serverapi.WorktreeView, token string) (serverapi.WorktreeView, error) {
	trimmedToken := strings.TrimSpace(token)
	if trimmedToken == "" {
		return serverapi.WorktreeView{}, serverapi.ErrWorktreeNotFound
	}
	matchers := []func(serverapi.WorktreeView, string) bool{
		func(item serverapi.WorktreeView, token string) bool {
			return strings.TrimSpace(item.WorktreeID) == token
		},
		func(item serverapi.WorktreeView, token string) bool {
			return strings.TrimSpace(item.CanonicalRoot) == token
		},
		func(item serverapi.WorktreeView, token string) bool {
			return strings.TrimSpace(item.DisplayName) == token
		},
		func(item serverapi.WorktreeView, token string) bool {
			return strings.TrimSpace(item.BranchName) == token || (token == "main" && item.IsMain)
		},
	}
	for _, matcher := range matchers {
		uniqueMatches := make(map[string]serverapi.WorktreeView, len(entries))
		orderedKeys := make([]string, 0, len(entries))
		for _, item := range entries {
			if !matcher(item, trimmedToken) {
				continue
			}
			key := tokenMatchKey(item)
			if _, ok := uniqueMatches[key]; ok {
				continue
			}
			uniqueMatches[key] = item
			orderedKeys = append(orderedKeys, key)
		}
		if len(orderedKeys) == 1 {
			return uniqueMatches[orderedKeys[0]], nil
		}
		if len(orderedKeys) > 1 {
			names := make([]string, 0, len(orderedKeys))
			for _, key := range orderedKeys {
				names = append(names, DisplayName(uniqueMatches[key]))
			}
			return serverapi.WorktreeView{}, fmt.Errorf("worktree %q is ambiguous: %s", trimmedToken, strings.Join(names, ", "))
		}
	}
	return serverapi.WorktreeView{}, fmt.Errorf("worktree %q not found", trimmedToken)
}

func tokenMatchKey(item serverapi.WorktreeView) string {
	if trimmed := strings.TrimSpace(item.WorktreeID); trimmed != "" {
		return "id:" + trimmed
	}
	if trimmed := strings.TrimSpace(item.CanonicalRoot); trimmed != "" {
		return "root:" + trimmed
	}
	return "name:" + DisplayName(item)
}
