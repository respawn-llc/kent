package runlog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"core/server/agentruntime"
	"core/server/sessionstore"
	"core/server/sessionstore/sessiontest"
	"core/shared/sessioncontract"
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

func TestDurabilityObserverStreamsTypedSessionAndFlushObservations(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewRunLogger(dir, nil)
	if err != nil {
		t.Fatalf("NewRunLogger: %v", err)
	}
	observer := NewDurabilityObserver()
	observer.Attach(logger)
	observer.ObserveEventLogAppend(session.EventLogAppendObservation{
		RecordCount: 3,
		Latency:     4 * time.Millisecond,
		Succeeded:   true,
	})
	observer.ObserveEventLogSync(session.EventLogSyncObservation{
		Latency:   2 * time.Millisecond,
		Succeeded: true,
	})
	observer.ObserveResultGroupFlush(runtime.ResultGroupFlushObservation{
		Reason:      runtime.ResultGroupFlushStepBoundary,
		ResultCount: 2,
		RecordCount: 3,
		Latency:     6 * time.Millisecond,
		Succeeded:   true,
	})
	if err := logger.Close(); err != nil {
		t.Fatalf("close logger: %v", err)
	}
	body, err := os.ReadFile(session.RunLogPath(dir))
	if err != nil {
		t.Fatalf("read run log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 3 {
		t.Fatalf("durability log lines = %d, want 3", len(lines))
	}
}

func TestDurabilityLoggerFailureDoesNotAlterAppendReceipt(t *testing.T) {
	observer := NewDurabilityObserver()
	fp, err := os.CreateTemp(t.TempDir(), "steps.log")
	if err != nil {
		t.Fatalf("create run log: %v", err)
	}
	if err := fp.Close(); err != nil {
		t.Fatalf("close run log: %v", err)
	}
	logger := &RunLogger{fp: fp}
	observer.Attach(logger)
	persistence := sessiontest.NewPersistence()
	options := append(
		persistence.Options(),
		session.WithDurabilityObserver(observer),
	)
	store, err := session.Create(
		t.TempDir(),
		"workspace",
		t.TempDir(),
		sessioncontract.SessionCategoryMain,
		options...,
	)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	eventLog, err := store.MaterializeEventLog()
	if err != nil {
		t.Fatalf("materialize event log: %v", err)
	}
	content := "durable"

	_, receipt, err := eventLog.AppendRecord(nil, session.MessageRecord{
		Role:    session.MessageRoleAssistant,
		Content: &content,
	})
	if err != nil {
		t.Fatalf("append record with failed durability logger: %v", err)
	}
	if !receipt.Committed {
		t.Fatal("append receipt is uncommitted after durability logger failure")
	}
}
