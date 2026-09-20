package shell

import (
	"context"
	"core/internal/testharness/postprocessfixture"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestManagerCollectUntilTreatsExitedProcessAsIncompleteUntilOutputFinalized(t *testing.T) {
	entry := &processEntry{
		notify:          make(chan struct{}, 1),
		outputFinalized: make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := (&Manager{}).collectUntil(ctx, entry, time.Now().Add(time.Second))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("collectUntil error = %v, want context canceled while output remains unfinalized", err)
	}
}

func TestManagerSubscribeOutputWaitsForLogFlushNotification(t *testing.T) {
	manager := newBackgroundTestManager(t)
	workspace := t.TempDir()

	result, err := manager.Start(context.Background(), ExecRequest{
		Postprocessor:  postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:        []string{"sh", "-c", "sleep 0.15; printf 'flush-ready\\n'; sleep 1"},
		DisplayCommand: "flush-notify",
		Workdir:        workspace,
		YieldTime:      50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !result.Backgrounded {
		t.Fatalf("expected backgrounded process, got %+v", result)
	}
	defer func() { _ = manager.Kill(result.SessionID) }()

	sub, err := manager.SubscribeOutput(context.Background(), result.SessionID, 0)
	if err != nil {
		t.Fatalf("SubscribeOutput: %v", err)
	}
	defer func() { _ = sub.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	chunk, err := sub.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if !strings.Contains(chunk.Text, "flush-ready") {
		t.Fatalf("expected flushed output, got %+v", chunk)
	}
}
