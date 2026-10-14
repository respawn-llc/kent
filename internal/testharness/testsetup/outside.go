package testsetup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	if !configured || base == "" {
		t.Fatal("non-temporary RAM fixtures require running tests through Just")
	}
	dir, err := os.MkdirTemp(base, prefix)
	if err != nil {
		t.Fatalf("create non-temporary RAM fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("cleanup non-temporary RAM fixture %q: %v", dir, err)
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
