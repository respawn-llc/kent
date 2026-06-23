package app

import (
	"testing"

	"core/cli/tui"
	"core/shared/clientui"
)

func TestReduceProjectedTranscriptEventSkipsDuplicateToolStart(t *testing.T) {
	state := projectedTranscriptEventState{
		entries: []tui.TranscriptEntry{{
			Role:       tui.TranscriptRoleToolCall,
			Text:       "pwd",
			ToolCallID: "call-1",
			Committed:  true,
		}},
	}
	reduction := reduceProjectedTranscriptEvent(state, clientui.Event{
		Kind: clientui.EventToolCallStarted,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:       "tool_call",
			Text:       "pwd",
			ToolCallID: "call-1",
		}},
	})

	if reduction.decision != projectedTranscriptDecisionSkip || !reduction.duplicateToolStarts {
		t.Fatalf("decision = %+v, want duplicate skip", reduction)
	}
	if reduction.skipReason != "duplicate_tool_call_start" {
		t.Fatalf("skip reason = %q", reduction.skipReason)
	}
}

func TestReduceProjectedTranscriptEventSkipsDuplicateTransientToolStart(t *testing.T) {
	state := projectedTranscriptEventState{
		entries: []tui.TranscriptEntry{{
			Role:       tui.TranscriptRoleToolCall,
			Text:       "pwd",
			ToolCallID: "call-1",
			Transient:  true,
		}},
	}
	reduction := reduceProjectedTranscriptEvent(state, clientui.Event{
		Kind: clientui.EventToolCallStarted,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:       "tool_call",
			Text:       "pwd",
			ToolCallID: "call-1",
		}},
	})

	if reduction.decision != projectedTranscriptDecisionSkip || !reduction.duplicateToolStarts {
		t.Fatalf("decision = %+v, want duplicate transient tool start skip", reduction)
	}
}

func TestReduceProjectedTranscriptEventHydratesCommittedGap(t *testing.T) {
	state := projectedTranscriptEventState{
		baseOffset: 0,
		entries: []tui.TranscriptEntry{{
			Role:      tui.TranscriptRoleUser,
			Text:      "prompt",
			Committed: true,
		}},
		revision: 3,
	}
	reduction := reduceProjectedTranscriptEvent(state, clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		CommittedTranscriptChanged: true,
		CommittedEntryStart:        3,
		CommittedEntryStartSet:     true,
		CommittedEntryCount:        4,
		TranscriptRevision:         4,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "assistant", Text: "answer"}},
	})

	if reduction.decision != projectedTranscriptDecisionHydrate {
		t.Fatalf("decision = %+v, want hydrate", reduction)
	}
	if reduction.plan.divergence != "gap_after_tail" {
		t.Fatalf("divergence = %q", reduction.plan.divergence)
	}
}

func TestReduceProjectedTranscriptEventAppendsCommittedSuffix(t *testing.T) {
	state := projectedTranscriptEventState{
		baseOffset: 0,
		entries: []tui.TranscriptEntry{{
			Role:      tui.TranscriptRoleUser,
			Text:      "prompt",
			Committed: true,
		}},
		revision: 1,
	}
	reduction := reduceProjectedTranscriptEvent(state, clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		CommittedTranscriptChanged: true,
		CommittedEntryStart:        0,
		CommittedEntryStartSet:     true,
		CommittedEntryCount:        2,
		TranscriptRevision:         2,
		TranscriptEntries: []clientui.ChatEntry{
			{Role: "user", Text: "prompt"},
			{Role: "assistant", Text: "answer"},
		},
	})

	if reduction.decision != projectedTranscriptDecisionApply || reduction.plan.mode != projectedTranscriptEntryPlanAppend {
		t.Fatalf("decision = %+v, want append apply", reduction)
	}
	if reduction.plan.rangeStart != 1 || reduction.plan.rangeEnd != 1 {
		t.Fatalf("range = [%d,%d], want [1,1]", reduction.plan.rangeStart, reduction.plan.rangeEnd)
	}
	if len(reduction.plan.entries) != 1 || reduction.plan.entries[0].Text != "answer" {
		t.Fatalf("entries = %+v, want answer suffix", reduction.plan.entries)
	}
	if !reduction.projectedCommitted || reduction.projectedTransient {
		t.Fatalf("commit flags = committed:%t transient:%t", reduction.projectedCommitted, reduction.projectedTransient)
	}
}

func TestReduceProjectedTranscriptEventDefersUserFlushWhileLiveAssistantPending(t *testing.T) {
	state := projectedTranscriptEventState{
		hasRuntimeClient:     true,
		busy:                 true,
		liveAssistantPending: true,
	}
	reduction := reduceProjectedTranscriptEvent(state, clientui.Event{
		Kind:                       clientui.EventUserMessageFlushed,
		CommittedTranscriptChanged: true,
		CommittedEntryStart:        0,
		CommittedEntryStartSet:     true,
		CommittedEntryCount:        1,
		TranscriptRevision:         1,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "user", Text: "follow up"}},
	})

	if reduction.decision != projectedTranscriptDecisionDefer || !reduction.shouldDeferTail {
		t.Fatalf("decision = %+v, want defer", reduction)
	}
	if reduction.skipReason != "deferred_tail" {
		t.Fatalf("skip reason = %q", reduction.skipReason)
	}
}

func TestReduceProjectedTranscriptEventDefersCommittedRowsWhileLiveAssistantPending(t *testing.T) {
	state := projectedTranscriptEventState{
		entries: []tui.TranscriptEntry{{
			Role:      tui.TranscriptRoleAssistant,
			Text:      "streaming",
			Committed: true,
		}},
		baseOffset:           0,
		revision:             1,
		hasRuntimeClient:     true,
		liveAssistantPending: true,
	}
	cases := []struct {
		name    string
		kind    clientui.EventKind
		entries []clientui.ChatEntry
	}{
		{
			name:    "local entry",
			kind:    clientui.EventLocalEntryAdded,
			entries: []clientui.ChatEntry{{Role: "system", Text: "local diagnostic"}},
		},
		{
			name:    "tool completion",
			kind:    clientui.EventToolCallCompleted,
			entries: []clientui.ChatEntry{{Role: "tool_result_ok", Text: "done", ToolCallID: "call-1"}},
		},
		{
			name:    "cache warning",
			kind:    clientui.EventCacheWarning,
			entries: []clientui.ChatEntry{{Role: "system", Text: "cache warning"}},
		},
		{
			name:    "conversation update with entries",
			kind:    clientui.EventConversationUpdated,
			entries: []clientui.ChatEntry{{Role: "user", Text: "authoritative tail"}},
		},
	}
	for idx, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reduction := reduceProjectedTranscriptEvent(state, clientui.Event{
				Kind:                       tc.kind,
				CommittedTranscriptChanged: true,
				CommittedEntryStart:        1 + idx,
				CommittedEntryStartSet:     true,
				CommittedEntryCount:        2 + idx,
				TranscriptRevision:         int64(2 + idx),
				TranscriptEntries:          tc.entries,
			})

			if reduction.decision != projectedTranscriptDecisionDefer || !reduction.shouldDeferTail {
				t.Fatalf("decision = %+v, want defer", reduction)
			}
			if reduction.skipReason != "deferred_tail" {
				t.Fatalf("skip reason = %q", reduction.skipReason)
			}
		})
	}
}

func TestReduceProjectedTranscriptEventAllowsFinalAssistantCommitToFinalizeLiveStream(t *testing.T) {
	state := projectedTranscriptEventState{
		liveAssistantPending: true,
		liveAssistantText:    "final",
	}
	reduction := reduceProjectedTranscriptEvent(state, clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		CommittedTranscriptChanged: true,
		CommittedEntryStart:        0,
		CommittedEntryStartSet:     true,
		CommittedEntryCount:        1,
		TranscriptRevision:         1,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:  "assistant",
			Text:  "final",
			Phase: string(clientui.MessagePhaseFinal),
		}},
	})

	if reduction.decision != projectedTranscriptDecisionApply {
		t.Fatalf("decision = %+v, want final assistant commit to apply", reduction)
	}
}

func TestReduceProjectedTranscriptEventAllowsLiveOnlyUnresolvedToolStartWhileLiveAssistantPending(t *testing.T) {
	state := projectedTranscriptEventState{
		entries: []tui.TranscriptEntry{{
			Role:      tui.TranscriptRoleAssistant,
			Text:      "streaming",
			Committed: true,
		}},
		liveAssistantPending: true,
	}
	reduction := reduceProjectedTranscriptEvent(state, clientui.Event{
		Kind:                       clientui.EventToolCallStarted,
		CommittedTranscriptChanged: true,
		CommittedEntryStart:        1,
		CommittedEntryStartSet:     true,
		CommittedEntryCount:        2,
		TranscriptRevision:         2,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:       "tool_call",
			Text:       "pwd",
			ToolCallID: "call-1",
			ToolCall:   &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "pwd"},
		}},
	})

	if reduction.decision != projectedTranscriptDecisionApply {
		t.Fatalf("decision = %+v, want live-only tool start to apply", reduction)
	}
}
