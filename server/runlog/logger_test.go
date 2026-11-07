package runlog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"core/server/sessionstore"
)

func TestRunLoggerWritesStepsFile(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewRunLogger(dir, nil)
	if err != nil {
		t.Fatalf("NewRunLogger: %v", err)
	}
	logger.Logf("step.start user_chars=%d", 10)
	if err := logger.Close(); err != nil {
		t.Fatalf("close logger: %v", err)
	}
	info, err := os.Stat(session.RunLogPath(dir))
	if err != nil {
		t.Fatalf("stat run log: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("run log is empty")
	}
}

func TestRunLoggerNoopsWhenSessionDirDoesNotExist(t *testing.T) {
	missingDir := filepath.Join(t.TempDir(), "missing-session")
	logger, err := NewRunLogger(missingDir, nil)
	if err != nil {
		t.Fatalf("NewRunLogger: %v", err)
	}
	logger.Logf("hello")
	if err := logger.Close(); err != nil {
		t.Fatalf("close logger: %v", err)
	}
	if _, err := os.Stat(session.RunLogPath(missingDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat missing run log: %v", err)
	}
}

func TestRunLoggerReportsWriteFailureOnce(t *testing.T) {
	fp, err := os.CreateTemp(t.TempDir(), "steps.log")
	if err != nil {
		t.Fatalf("create run log: %v", err)
	}
	if err := fp.Close(); err != nil {
		t.Fatalf("close run log: %v", err)
	}
	var diagnostics []string
	logger := &RunLogger{
		fp: fp,
		onDiagnostic: func(message string) {
			diagnostics = append(diagnostics, message)
		},
	}
	logger.Logf("first")
	logger.Logf("second")

	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %d, want 1", len(diagnostics))
	}
	if strings.TrimSpace(diagnostics[0]) == "" {
		t.Fatal("write failure diagnostic is empty")
	}
}
