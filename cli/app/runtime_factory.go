package app

import (
	"fmt"
	"sort"

	"builder/shared/client"
	"builder/shared/clientui"
	"builder/shared/config"
)

type runtimeWiring struct {
	turnQueueHook         turnQueueHook
	terminalFocus         *terminalFocusState
	runtimeEvents         <-chan clientui.Event
	askEvents             <-chan askEvent
	runtimeClient         clientui.RuntimeClient
	promptControl         client.PromptControlClient
	runtimeControls       client.RuntimeControlClient
	worktrees             client.WorktreeClient
	processControls       client.ProcessControlClient
	processOutput         client.ProcessOutputClient
	processViews          client.ProcessViewClient
	approvalViews         client.ApprovalViewClient
	askViews              client.AskViewClient
	sessionActivity       client.SessionActivityClient
	sessionViews          client.SessionViewClient
	hasOtherSessions      bool
	hasOtherSessionsKnown bool
	promptHistory         []string
}

func configSourceLines(src config.SourceReport) []string {
	keys := make([]string, 0, len(src.Sources))
	for k := range src.Sources {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("%s=%s", k, src.Sources[k]))
	}
	return lines
}
