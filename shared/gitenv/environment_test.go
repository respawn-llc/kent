package gitenv_test

import (
	"slices"
	"testing"

	"core/shared/gitenv"
)

func TestWithoutRepositoryOverrides(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"HOME=/tmp/home",
		"GIT_DIR=/tmp/repo/.git",
		"GIT_WORK_TREE=/tmp/repo",
		"GIT_COMMON_DIR=/tmp/common",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.hooksPath",
		"GIT_CONFIG_VALUE_0=.githooks",
		"GIT_CONFIG_KEY_extra=core.editor",
		"GIT_CONFIG_VALUE_extra=editor",
		"GIT_AUTHOR_NAME=Author",
		"GIT_COMMITTER_EMAIL=author@example.invalid",
		"GIT_CONFIG_GLOBAL=/tmp/config",
		"GIT_CONFIG_SYSTEM=/tmp/system-config",
		"GIT_CONFIG_NOSYSTEM=1",
		"OTHER=contains=equals",
		"PATH=/bin",
	}
	original := slices.Clone(base)
	want := []string{
		"PATH=/usr/bin",
		"HOME=/tmp/home",
		"GIT_AUTHOR_NAME=Author",
		"GIT_COMMITTER_EMAIL=author@example.invalid",
		"GIT_CONFIG_GLOBAL=/tmp/config",
		"GIT_CONFIG_SYSTEM=/tmp/system-config",
		"GIT_CONFIG_NOSYSTEM=1",
		"OTHER=contains=equals",
		"PATH=/bin",
	}
	if got := gitenv.WithoutRepositoryOverrides(base); !slices.Equal(got, want) {
		t.Fatalf("environment = %q, want %q", got, want)
	}
	if !slices.Equal(base, original) {
		t.Fatalf("input changed: %q", base)
	}
}

func TestEmptyEnvironmentDoesNotInheritOverrides(t *testing.T) {
	for _, input := range [][]string{nil, {}, {"GIT_DIR=/tmp/repo/.git"}} {
		if got := gitenv.WithoutRepositoryOverrides(input); got == nil || len(got) != 0 {
			t.Fatalf("environment = %#v, want non-nil empty environment", got)
		}
	}
}
