// Package gitenv prepares Git commands to operate on their selected directory.
package gitenv

import "strings"

// WithoutRepositoryOverrides removes inherited repository-local Git settings.
// User identity and global configuration remain available. The result is always
// non-nil so assigning an empty result to exec.Cmd.Env cannot inherit overrides.
func WithoutRepositoryOverrides(base []string) []string {
	filtered := make([]string, 0, len(base))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GIT_ALTERNATE_OBJECT_DIRECTORIES",
			"GIT_COMMON_DIR",
			"GIT_CONFIG",
			"GIT_CONFIG_COUNT",
			"GIT_CONFIG_PARAMETERS",
			"GIT_DIR",
			"GIT_GLOB_PATHSPECS",
			"GIT_GRAFT_FILE",
			"GIT_ICASE_PATHSPECS",
			"GIT_IMPLICIT_WORK_TREE",
			"GIT_INDEX_FILE",
			"GIT_INTERNAL_SUPER_PREFIX",
			"GIT_LITERAL_PATHSPECS",
			"GIT_NAMESPACE",
			"GIT_NOGLOB_PATHSPECS",
			"GIT_NO_REPLACE_OBJECTS",
			"GIT_OBJECT_DIRECTORY",
			"GIT_PREFIX",
			"GIT_REPLACE_REF_BASE",
			"GIT_SHALLOW_FILE",
			"GIT_WORK_TREE":
			continue
		}
		if strings.HasPrefix(key, "GIT_CONFIG_KEY_") || strings.HasPrefix(key, "GIT_CONFIG_VALUE_") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}
