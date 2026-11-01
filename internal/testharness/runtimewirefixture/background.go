package runtimewirefixture

import (
	"context"
	"fmt"
	"time"

	shelltool "core/server/tools/shell"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
)

func BackgroundCompletionEvent(ctx context.Context, id string, ownerSessionID string, root string) shelltool.Event {
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
			"read -r release; echo done",
		},
		DisplayCommand: "fixture background completion",
		OwnerSessionID: ownerSessionID,
		Workdir:        root,
		YieldTime:      time.Millisecond,
		KeepStdinOpen:  true,
	})
	if err != nil {
		panic(fmt.Sprintf("start background shell fixture: %v", err))
	}
	if !result.MovedToBackground {
		panic("background shell fixture completed before background transition")
	}
	if _, err := manager.WriteStdin(ctx, shelltool.WriteRequest{
		SessionID: result.SessionID, Input: "\n", YieldTime: time.Millisecond, MaxOutputChars: 1_000,
	}); err != nil {
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
