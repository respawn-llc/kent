package transport

import (
	"strings"

	"core/shared/gitenv"
)

func sanitizeTestGitEnv(base []string) []string {
	filtered := make([]string, 0, len(base))
	for _, entry := range gitenv.WithoutRepositoryOverrides(base) {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GIT_AUTHOR_") || strings.HasPrefix(key, "GIT_COMMITTER_") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func appendTestGitCommitIdentityEnv(env []string) []string {
	return append(env,
		"GIT_AUTHOR_NAME=kent-test",
		"GIT_AUTHOR_EMAIL=kent-test@example.invalid",
		"GIT_COMMITTER_NAME=kent-test",
		"GIT_COMMITTER_EMAIL=kent-test@example.invalid",
	)
}
