package runtimewirefixture

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	shelltool "core/server/tools/shell"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
)

func BackgroundCompletionEvent(ctx context.Context, id string, ownerSessionID string, root string) shelltool.Event {
	sourcePath := filepath.Join(root, id+".fixture-output")
	if err := os.WriteFile(sourcePath, []byte("done"), 0o644); err != nil {
		panic(fmt.Sprintf("write background shell fixture output: %v", err))
	}
	releasePath := filepath.Join(root, id+".fixture-release")
	manager, err := shelltool.NewManager(root, shelltool.WithMinimumExecToBgTime(time.Millisecond))
	if err != nil {
		panic(fmt.Sprintf("create background shell manager fixture: %v", err))
	}
	defer func() { _ = manager.Close() }()
	runner, err := postprocess.NewRunner(postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin})
	if err != nil {
		panic(fmt.Sprintf("create background shell postprocessor fixture: %v", err))
	}

	events := make(chan shelltool.Event, 1)
	manager.SetEventHandler(func(event shelltool.Event) bool {
		if event.Type == shelltool.EventCompleted || event.Type == shelltool.EventKilled {
			events <- event
		}
		return true
	})
	result, err := manager.Start(ctx, shelltool.ExecRequest{
		Postprocessor: runner,
		Command: []string{
			"/bin/sh",
			"-c",
			fmt.Sprintf("while [ ! -f %q ]; do sleep 0.01; done; cat %q", releasePath, sourcePath),
		},
		DisplayCommand: "fixture background completion",
		OwnerSessionID: ownerSessionID,
		Workdir:        root,
		YieldTime:      time.Millisecond,
	})
	if err != nil {
		panic(fmt.Sprintf("start background shell fixture: %v", err))
	}
	if !result.MovedToBackground {
		panic("background shell fixture completed before background transition")
	}
	if err := os.WriteFile(releasePath, nil, 0o644); err != nil {
		panic(fmt.Sprintf("release background shell fixture: %v", err))
	}
	select {
	case event := <-events:
		event.Snapshot.ID = id
		return event
	case <-ctx.Done():
		panic(fmt.Sprintf("waiting for background shell fixture completion: %v", ctx.Err()))
	}
}
