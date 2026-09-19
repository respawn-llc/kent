package shell

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"core/internal/testharness/postprocessfixture"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
)

func TestManagerConcurrentShellLimitReleasesCapacity(t *testing.T) {
	manager := newShellTestManager(t, time.Millisecond, WithMaxConcurrent(1))
	req := ExecRequest{
		Command: []string{"/bin/sh", "-c", "read value"},
		Workdir: t.TempDir(), KeepStdinOpen: true, YieldTime: time.Millisecond,
		Postprocessor: postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
	}
	first, err := manager.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.Start(context.Background(), req)
	var limit *ConcurrentShellLimitError
	if !errors.As(err, &limit) || limit.Limit != 1 {
		t.Fatalf("start at capacity = %v, want limit with one running shell", err)
	}
	_, err = manager.WriteStdin(context.Background(), WriteRequest{
		SessionID: first.SessionID, Input: "done\n", YieldTime: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(context.Background(), req); err != nil {
		t.Fatalf("start after exit: %v", err)
	}
}

func TestManagerConcurrentStartsRespectLimit(t *testing.T) {
	manager := newShellTestManager(t, time.Millisecond, WithMaxConcurrent(2))
	req := ExecRequest{
		Command: []string{"/bin/sh", "-c", "read value"},
		Workdir: t.TempDir(), KeepStdinOpen: true, YieldTime: time.Millisecond,
		Postprocessor: postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
	}
	const attempts = 12
	results := make(chan error, attempts)
	var starts sync.WaitGroup
	for range attempts {
		starts.Go(func() {
			_, err := manager.Start(context.Background(), req)
			results <- err
		})
	}
	starts.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
			continue
		}
		var limit *ConcurrentShellLimitError
		if !errors.As(err, &limit) || limit.Limit != 2 {
			t.Fatalf("unexpected rejection: %v", err)
		}
	}
	if accepted != 2 {
		t.Fatalf("accepted %d shells, want 2", accepted)
	}
}

func TestManagerFailedStartReleasesCapacity(t *testing.T) {
	manager := newShellTestManager(t, time.Millisecond, WithMaxConcurrent(1))
	req := ExecRequest{
		Command: []string{"/does-not-exist"}, Workdir: t.TempDir(),
		Postprocessor: postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
	}
	if _, err := manager.Start(context.Background(), req); err == nil {
		t.Fatal("nonexistent command succeeded")
	}
	req.Command = []string{"/bin/sh", "-c", "exit 0"}
	if _, err := manager.Start(context.Background(), req); err != nil {
		t.Fatalf("start after failed start: %v", err)
	}
}

func TestExecCommandConcurrentLimitIsRecoverableToolError(t *testing.T) {
	manager := newShellTestManager(t, time.Millisecond, WithMaxConcurrent(1))
	tool := NewExecCommandToolWithPostprocessor(t.TempDir(), 16_000, 100_000, manager, "session", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	input := map[string]any{"cmd": "read value", "tty": true, "yield_time_ms": 1}
	first := callExecCommand(t, tool, "first", input)
	if first.IsError {
		t.Fatalf("first start failed: %s", first.Output)
	}
	rejected := callExecCommand(t, tool, "second", input)
	if !rejected.IsError || rejected.Terminal {
		t.Fatalf("expected non-terminal tool error: %+v", rejected)
	}
	var output struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rejected.Output, &output); err != nil {
		t.Fatal(err)
	}
	if output.Error == "" {
		t.Fatal("model-visible error is empty")
	}
}
