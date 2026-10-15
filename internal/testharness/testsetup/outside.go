package testsetup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests replace HOME with temporary persistence roots after process startup.
var processHome, processHomeErr = os.UserHomeDir()

func NonTemporaryDirectory(
	t testing.TB,
	prefix string,
	isTemporary func(string) bool,
) string {
	t.Helper()
	if strings.TrimSpace(prefix) == "" {
		t.Fatal("non-temporary directory prefix is required")
	}
	if isTemporary == nil {
		t.Fatal("temporary-path classifier is required")
	}

	base, configured := os.LookupEnv("KENT_TEST_NON_TEMPORARY_ROOT")
	if !configured {
		if processHomeErr != nil {
			t.Fatalf("resolve home directory for non-temporary fixture: %v", processHomeErr)
		}
		base = processHome
	}
	if strings.TrimSpace(base) == "" {
		t.Fatal("non-temporary fixture root is required")
	}
	dir, err := os.MkdirTemp(base, prefix)
	if err != nil {
		t.Fatalf("create non-temporary fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("cleanup non-temporary fixture %q: %v", dir, err)
		}
	})
	absolute, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("resolve non-temporary RAM fixture: %v", err)
	}
	if isTemporary(absolute) {
		t.Fatalf("non-temporary fixture %q is classified as temporary", absolute)
	}
	return absolute
}
