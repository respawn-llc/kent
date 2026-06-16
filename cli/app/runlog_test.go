package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunLoggerWritesStepsFile(t *testing.T) {
	dir := t.TempDir()
	logger, err := newRunLogger(dir, nil)
	if err != nil {
		t.Fatalf("newRunLogger failed: %v", err)
	}
	logger.Logf("step.start user_chars=%d", 10)
	logger.Logf("step.error err=%q", "boom")
	if err := logger.Close(); err != nil {
		t.Fatalf("close logger: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, runLogFileName))
	if err != nil {
		t.Fatalf("expected run log file for persisted session: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("expected run log file to contain logged steps, got empty file")
	}
}

func TestNewRunLoggerNoopsWhenSessionDirDoesNotExist(t *testing.T) {
	missingDir := filepath.Join(t.TempDir(), "missing-session")
	logger, err := newRunLogger(missingDir, nil)
	if err != nil {
		t.Fatalf("new run logger: %v", err)
	}
	logger.Logf("hello %s", "world")
	if err := logger.Close(); err != nil {
		t.Fatalf("close run logger: %v", err)
	}
	if _, err := os.Stat(filepath.Join(missingDir, runLogFileName)); !os.IsNotExist(err) {
		t.Fatalf("expected no run log file for non-persisted session, stat err=%v", err)
	}
}

type failingWriteCloser struct{}

func (failingWriteCloser) WriteString(string) (int, error) {
	return 0, errors.New("disk full")
}

func (failingWriteCloser) Close() error {
	return nil
}

func TestRunLoggerReportsWriteFailureDiagnosticOnce(t *testing.T) {
	var diagnostics []runLoggerDiagnostic
	logger := &runLogger{
		fp: failingWriteCloser{},
		onDiagnostic: func(diag runLoggerDiagnostic) {
			diagnostics = append(diagnostics, diag)
		},
	}
	logger.Logf("first")
	logger.Logf("second")

	if len(diagnostics) != 1 {
		t.Fatalf("expected one diagnostic, got %d", len(diagnostics))
	}
	if diagnostics[0].Kind != "write_failed" {
		t.Fatalf("expected write_failed diagnostic kind, got %+v", diagnostics[0])
	}
	if diagnostics[0].Err == nil || !strings.Contains(diagnostics[0].Err.Error(), "disk full") {
		t.Fatalf("expected underlying write error, got %+v", diagnostics[0])
	}
}
