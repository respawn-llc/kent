package shell

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"core/internal/testharness/postprocessfixture"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
)

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

	victimID := completedIDs[0]
	victim, err := manager.Snapshot(victimID)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range completedIDs[1:] {
		if _, err := manager.Snapshot(id); err != nil {
			t.Fatalf("refresh completed shell %s: %v", id, err)
		}
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
	<-evicted

	if _, err := manager.Snapshot(victimID); !errors.Is(err, ErrResultUnavailable) {
		t.Fatalf("least-recently-accessed completed shell error = %v, want %v", err, ErrResultUnavailable)
	}
	if _, err := manager.Snapshot(completedIDs[1]); err != nil {
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
		SessionID:      result.SessionID,
		Input:          "\n",
		YieldTime:      time.Millisecond,
		MaxOutputChars: 1_000,
	})
	if err != nil {
		t.Fatalf("release retained shell %s: %v", result.SessionID, err)
	}
	event := <-completed
	if event.Type != EventCompleted || event.Snapshot.ID != result.SessionID {
		t.Fatalf("unexpected retained shell completion: %+v, want process %s", event, result.SessionID)
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
		OwnerSessionID: fmt.Sprintf("session-%d", index),
		Postprocessor:  runner,
		Command:        []string{"/bin/sh", "-c", "read -r release"},
		DisplayCommand: "wait for release",
		Workdir:        workdir,
		YieldTime:      time.Millisecond,
		MaxOutputChars: 1_000,
		KeepStdinOpen:  true,
	})
	if err != nil {
		t.Fatalf("start retained shell %d: %v", index, err)
	}
	if !result.Backgrounded || !result.Running {
		t.Fatalf("shell %d did not move to background: %+v", index, result)
	}
	return result
}
