package testgit

import (
	"strings"
	"testing"
)

func TestSanitizeEnvRemovesParentGitIdentityBeforeAppendingTestIdentity(t *testing.T) {
	env := AppendCommitIdentityEnv(SanitizeEnv([]string{
		"PATH=/bin",
		"GIT_AUTHOR_NAME=parent-author",
		"GIT_AUTHOR_EMAIL=parent-author@example.test",
		"GIT_COMMITTER_NAME=parent-committer",
		"GIT_COMMITTER_EMAIL=parent-committer@example.test",
		"GIT_CONFIG_KEY_0=user.name",
		"GIT_CONFIG_VALUE_0=parent",
		"GIT_DIR=/repo/.git",
	}))

	values := map[string][]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		values[key] = append(values[key], value)
	}
	for _, key := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"} {
		if got := len(values[key]); got != 1 {
			t.Fatalf("%s count = %d, want 1 in env %v", key, got, env)
		}
	}
	if values["GIT_AUTHOR_NAME"][0] != "kent-test" || values["GIT_COMMITTER_NAME"][0] != "kent-test" {
		t.Fatalf("unexpected git identity env: %v", values)
	}
	if _, ok := values["GIT_CONFIG_KEY_0"]; ok {
		t.Fatalf("GIT_CONFIG_KEY_0 survived sanitization: %v", env)
	}
	if _, ok := values["GIT_DIR"]; ok {
		t.Fatalf("GIT_DIR survived sanitization: %v", env)
	}
	if values["PATH"][0] != "/bin" {
		t.Fatalf("PATH = %v, want /bin", values["PATH"])
	}
}
