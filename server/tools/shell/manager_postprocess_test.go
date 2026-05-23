package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadOutputFileLimitedRejectsOversizedLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shell.log")
	if err := os.WriteFile(path, []byte("abcdef"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := readOutputFileLimited(path, 5); err == nil || !strings.Contains(err.Error(), "exceeds full-read limit") {
		t.Fatalf("expected full-read limit error, got %v", err)
	}
}

func TestReadOutputFileLimitedReadsWithinLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shell.log")
	if err := os.WriteFile(path, []byte("abcdef"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := readOutputFileLimited(path, 6)
	if err != nil {
		t.Fatalf("readOutputFileLimited: %v", err)
	}
	if got != "abcdef" {
		t.Fatalf("content = %q, want abcdef", got)
	}
}
