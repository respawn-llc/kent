package app

import (
	"testing"

	"core/cli/tui"
	"core/shared/clientui"
)

func TestCommittedOngoingLocalFrontierUsesUnfilteredStablePrefix(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.transcriptBaseOffset = 10
	m.transcriptEntries = []tui.TranscriptEntry{
		{Role: tui.TranscriptRoleUser, Text: "prompt", Committed: true},
		{Role: "reviewer_status", Text: "Supervisor ran.", Transient: true, Committed: true},
		{Role: tui.TranscriptRoleAssistant, Text: "", Committed: true},
		{Role: tui.TranscriptRoleToolCall, Text: "pending", ToolCallID: "call_a", Transient: true},
	}

	if rendered := len(committedTranscriptEntriesForApp(m.transcriptEntries)); rendered != 2 {
		t.Fatalf("test precondition failed: rendered committed entry count = %d, want committed transient row kept and empty assistant filtered", rendered)
	}
	if got := committedOngoingLocalFrontierEnd(m); got != 13 {
		t.Fatalf("committed ongoing local frontier = %d, want unfiltered stable transcript prefix end", got)
	}
}

func TestAuthoritativeRecentTailHydrateDeliveryUsesUnfilteredStablePrefix(t *testing.T) {
	m := newProjectedStaticUIModel()
	entries := []tui.TranscriptEntry{
		{Role: tui.TranscriptRoleUser, Text: "prompt", Committed: true},
		{Role: "reviewer_status", Text: "Supervisor ran.", Transient: true, Committed: true},
		{Role: tui.TranscriptRoleAssistant, Text: "", Committed: true},
	}

	m.runtimeAdapter().applyAuthoritativeRecentTailPage(clientui.TranscriptPage{
		Offset:       10,
		Revision:     7,
		TotalEntries: 13,
	}, entries, false)

	state := committedDeliveryStateForTest(m)
	if !state.Initialized || state.LastAppliedCommittedEntryCount != 13 {
		t.Fatalf("committed delivery state = %+v, want applied frontier at unfiltered stable prefix end", state)
	}
}
