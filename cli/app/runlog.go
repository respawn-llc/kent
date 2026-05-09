package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const runLogFileName = "steps.log"

type runLogger struct {
	mu                   sync.Mutex
	fp                   writeStringCloser
	onDiagnostic         func(runLoggerDiagnostic)
	reportedWriteFailure bool
}

type writeStringCloser interface {
	WriteString(string) (int, error)
	Close() error
}

type runLoggerDiagnostic struct {
	Kind    string
	Message string
	Err     error
}

func newRunLogger(sessionDir string, onDiagnostic func(runLoggerDiagnostic)) (*runLogger, error) {
	fp, err := os.OpenFile(filepath.Join(sessionDir, runLogFileName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &runLogger{onDiagnostic: onDiagnostic}, nil
		}
		return nil, fmt.Errorf("open run log: %w", err)
	}
	return &runLogger{fp: fp, onDiagnostic: onDiagnostic}, nil
}

func (l *runLogger) Close() error {
	if l == nil || l.fp == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fp.Close()
}

func (l *runLogger) Logf(format string, args ...any) {
	if l == nil || l.fp == nil {
		return
	}
	line := fmt.Sprintf(format, args...)
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return
	}

	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := l.fp.WriteString(stamp + " " + line + "\n"); err != nil && !l.reportedWriteFailure {
		l.reportedWriteFailure = true
		if l.onDiagnostic != nil {
			l.onDiagnostic(runLoggerDiagnostic{
				Kind:    "write_failed",
				Message: fmt.Sprintf("run log write failed; observability degraded: %v", err),
				Err:     err,
			})
		}
	}
}

func reportRunLoggerDiagnostic(w io.Writer, diag runLoggerDiagnostic) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintln(w, formatRunLoggerDiagnostic(diag))
}

func formatRunLoggerDiagnostic(diag runLoggerDiagnostic) string {
	message := strings.TrimSpace(diag.Message)
	if message == "" {
		message = "run logger diagnostic"
	}
	parts := []string{"run_logger.diagnostic"}
	if kind := strings.TrimSpace(diag.Kind); kind != "" {
		parts = append(parts, fmt.Sprintf("kind=%s", kind))
	}
	parts = append(parts, fmt.Sprintf("message=%q", message))
	if diag.Err != nil {
		parts = append(parts, fmt.Sprintf("err=%q", diag.Err.Error()))
	}
	return strings.Join(parts, " ")
}
