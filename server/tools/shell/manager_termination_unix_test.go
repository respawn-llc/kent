//go:build darwin || linux

package shell

import (
	"context"
	"testing"
	"time"

	"core/internal/testharness/postprocessfixture"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
)

func TestManagerTerminationPublishesKilledAfterCleanShellExit(t *testing.T) {
	for _, command := range []string{
		"sleep 2 &",
		"trap 'exit 0' TERM INT; while :; do sleep 0.1; done",
	} {
		t.Run(command, func(t *testing.T) {
			manager := newShellTestManager(t, 50*time.Millisecond)
			terminal := make(chan Event, 1)
			manager.SetEventHandler(func(event Event) bool {
				if event.Type == EventCompleted || event.Type == EventKilled {
					terminal <- event
				}
				return true
			})
			result, err := manager.Start(context.Background(), ExecRequest{
				Postprocessor:  postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
				Command:        []string{"/bin/sh", "-c", command},
				DisplayCommand: command,
				Workdir:        t.TempDir(),
				YieldTime:      50 * time.Millisecond,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !result.Running || !result.MovedToBackground {
				t.Fatalf("not backgrounded: %+v", result)
			}
			if err := manager.Kill(result.SessionID); err != nil {
				t.Fatal(err)
			}
			select {
			case event := <-terminal:
				if event.Type != EventKilled || event.Snapshot.State != "killed" || !event.Snapshot.KillRequested || event.Snapshot.Running {
					t.Fatalf("inconsistent termination: %+v", event)
				}
			case <-time.After(time.Second):
				t.Fatal("termination did not finish while descendant remained running")
			}
		})
	}
}
