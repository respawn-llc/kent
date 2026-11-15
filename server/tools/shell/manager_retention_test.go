package shell

import (
	"bytes"
	"context"
	"core/internal/testharness/testsetup"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"core/internal/testharness/postprocessfixture"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
	"core/shared/invariant"
)

func TestManagerCompletedRetentionOverflowProtectsPendingNotices(t *testing.T) {
	t.Setenv("KENT_INVARIANT_MODE", string(invariant.ModeDiagnostic))
	var diagnostics bytes.Buffer
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&diagnostics, nil)))
	t.Cleanup(func() { slog.SetDefault(logger) })

	manager := fillPendingShellRetention(t, nil)
	if got := len(manager.List()); got != 1_001 {
		t.Fatalf("pending completed shells = %d, want 1001", got)
	}
	var record struct {
		Diagnostic invariant.Diagnostic `json:"diagnostic"`
	}
	decoder := json.NewDecoder(&diagnostics)
	overflowDiagnostics := 0
	for {
		err := decoder.Decode(&record)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if record.Diagnostic.Scope == invariant.ScopeBackgroundEvent {
			overflowDiagnostics++
		}
	}
	if overflowDiagnostics != 1 {
		t.Fatalf("overflow diagnostics = %d, want 1", overflowDiagnostics)
	}
	delivered := 0
	manager.SetEventHandler(func(event Event) bool {
		if event.Type == EventCompleted || event.Type == EventKilled {
			delivered++
		}
		return true
	})
	for _, snapshot := range manager.List() {
		manager.RetryTerminalEvents(snapshot.OwnerSessionID)
	}
	if delivered != 1_001 {
		t.Fatalf("delivered notices = %d, want 1001", delivered)
	}
	if got := len(manager.List()); got != 1_000 {
		t.Fatalf("completed shells after delivery = %d, want 1000", got)
	}
}

func TestManagerCompletedRetentionOverflowPanicsInDebug(t *testing.T) {
	const childEnv = "KENT_TEST_SHELL_RETENTION_CHILD"
	if os.Getenv(childEnv) == "1" {
		fillPendingShellRetention(t, func() {
			if err := os.WriteFile(os.Getenv("KENT_TEST_SHELL_RETENTION_READY"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		})
		return
	}
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command(os.Args[0], "-test.run=^TestManagerCompletedRetentionOverflowPanicsInDebug$")
	cmd.Env = append(os.Environ(), childEnv+"=1", "KENT_TEST_SHELL_RETENTION_READY="+ready,
		"KENT_DEBUG=1", "KENT_INVARIANT_MODE="+string(invariant.ModePanic))
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		t.Fatalf("overflow subprocess error = %v, want panic exit 2\n%s", err, output)
	}
	if _, err := os.Stat(ready); err != nil {
		t.Fatalf("subprocess did not reach the retention boundary: %v\n%s", err, output)
	}
}

func fillPendingShellRetention(t *testing.T, atLimit func()) *Manager {
	t.Helper()
	manager := newShellTestManager(t, time.Millisecond)
	completed := make(chan Event, 1)
	manager.SetEventHandler(func(event Event) bool {
		if event.Type == EventCompleted || event.Type == EventKilled {
			completed <- event
			return false
		}
		return true
	})
	runner := postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin})
	workdir := t.TempDir()
	for index := range 1_001 {
		if index == 1_000 && atLimit != nil {
			atLimit()
		}
		startAndCompleteRetainedShell(t, manager, runner, completed, workdir, index)
	}
	return manager
}

func TestManagerRetainsMostRecentlyAccessedCompletedShellsWithoutEvictingRunningShells(t *testing.T) {
	const completedRetentionLimit = 1_000

	manager := newShellTestManager(t, time.Millisecond)
	runner := postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin})
	completed := make(chan Event, 1)
	manager.SetEventHandler(func(event Event) bool {
		if event.Type == EventCompleted || event.Type == EventKilled {
			completed <- event
		}
		return true
	})
	workdir := t.TempDir()
	completedIDs := make([]string, 0, completedRetentionLimit)
	for index := range completedRetentionLimit {
		completedIDs = append(
			completedIDs,
			startAndCompleteRetainedShell(t, manager, runner, completed, workdir, index),
		)
	}

	victimID := completedIDs[1]
	var victim *Snapshot
	for _, snapshot := range manager.List() {
		if snapshot.ID == victimID {
			victim = &snapshot
			break
		}
	}
	if victim == nil {
		t.Fatal("completed shell missing from process list")
	}
	if _, _, err := manager.InlineOutput(completedIDs[0], 100); err != nil {
		t.Fatalf("access oldest completed shell: %v", err)
	}
	if got := len(manager.List()); got != completedRetentionLimit {
		t.Fatalf("retained process count = %d, want %d", got, completedRetentionLimit)
	}
	evicted := make(chan struct{}, 1)
	manager.SetBackgroundListChangeHandler(func(sessionID string) {
		if sessionID == victim.OwnerSessionID {
			select {
			case evicted <- struct{}{}:
			default:
			}
		}
	})

	running := startRetainedShell(
		t,
		manager,
		runner,
		workdir,
		completedRetentionLimit,
	)
	_ = manager.List()
	startAndCompleteRetainedShell(t, manager, runner, completed, workdir, completedRetentionLimit+1)
	select {
	case <-evicted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for oldest completed shell eviction")
	}

	if _, err := manager.Snapshot(victimID); !errors.Is(err, ErrResultUnavailable) {
		t.Fatalf("least-recently-accessed completed shell error = %v, want %v", err, ErrResultUnavailable)
	}
	if _, err := manager.Snapshot(completedIDs[0]); err != nil {
		t.Fatalf("recently accessed completed shell was evicted: %v", err)
	}
	runningSnapshot, err := manager.Snapshot(running.SessionID)
	if err != nil {
		t.Fatalf("running shell was evicted: %v", err)
	}
	if !runningSnapshot.Running {
		t.Fatalf("protected shell is not running: %+v", runningSnapshot)
	}
	if got := len(manager.List()); got != completedRetentionLimit+1 {
		t.Fatalf("retained process count with running shell = %d, want %d", got, completedRetentionLimit+1)
	}
	if _, err := os.Stat(victim.LogPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("evicted shell log error = %v, want not found", err)
	}
}

func startAndCompleteRetainedShell(
	t *testing.T,
	manager *Manager,
	runner *postprocess.Runner,
	completed <-chan Event,
	workdir string,
	index int,
) string {
	t.Helper()
	result := startRetainedShell(t, manager, runner, workdir, index)
	_, err := manager.WriteStdin(context.Background(), WriteRequest{
		SessionID: result.SessionID,
		Input:     "\n",
		YieldTime: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("release retained shell %s: %v", result.SessionID, err)
	}
	var event Event
	select {
	case event = <-completed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for shell completion")
	}
	if event.Type != EventCompleted || event.Snapshot.ID != result.SessionID {
		t.Fatalf("unexpected retained shell completion: %+v, want process %s", event, result.SessionID)
	}
	if _, err := manager.WriteStdin(context.Background(), WriteRequest{SessionID: result.SessionID}); err != nil {
		t.Fatalf("collect completed shell %s: %v", result.SessionID, err)
	}
	return result.SessionID
}

func startRetainedShell(
	t *testing.T,
	manager *Manager,
	runner *postprocess.Runner,
	workdir string,
	index int,
) ExecResult {
	t.Helper()
	result, err := manager.Start(context.Background(), ExecRequest{
		OwnerSessionShortID: testsetup.ShellLogSessionShortID(t),
		OwnerSessionID:      fmt.Sprintf("session-%d", index),
		Postprocessor:       runner,
		Command:             []string{"/bin/sh", "-c", "read -r release"},
		DisplayCommand:      "wait for release",
		Workdir:             workdir,
		YieldTime:           time.Millisecond,
		MaxOutputChars:      1_000,
		KeepStdinOpen:       true,
	})
	if err != nil {
		t.Fatalf("start retained shell %d: %v", index, err)
	}
	if !result.Backgrounded || !result.Running {
		t.Fatalf("shell %d did not move to background: %+v", index, result)
	}
	return result
}
