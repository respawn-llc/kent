package app

import (
	"strings"

	"core/cli/tui"
	"core/shared/clientui"
	"core/shared/transcript"
)

type projectedTranscriptEntryPlanMode uint8

const (
	projectedTranscriptEntryPlanSkip projectedTranscriptEntryPlanMode = iota + 1
	projectedTranscriptEntryPlanAppend
	projectedTranscriptEntryPlanReplace
	projectedTranscriptEntryPlanHydrate
)

type projectedTranscriptEntryPlan struct {
	mode       projectedTranscriptEntryPlanMode
	rangeStart int
	rangeEnd   int
	entries    []clientui.ChatEntry
	divergence string
}

type projectedTranscriptDecisionKind uint8

const (
	projectedTranscriptDecisionApply projectedTranscriptDecisionKind = iota + 1
	projectedTranscriptDecisionSkip
	projectedTranscriptDecisionHydrate
	projectedTranscriptDecisionDefer
)

type projectedTranscriptEventState struct {
	entries              []tui.TranscriptEntry
	baseOffset           int
	revision             int64
	hasRuntimeClient     bool
	busy                 bool
	liveAssistantPending bool
}

type projectedTranscriptEventSnapshot struct {
	entries              []tui.TranscriptEntry
	baseOffset           int
	revision             int64
	hasRuntimeClient     bool
	busy                 bool
	liveAssistantPending bool
}

type projectedTranscriptReduction struct {
	decision            projectedTranscriptDecisionKind
	plan                projectedTranscriptEntryPlan
	skipReason          string
	projectedCommitted  bool
	projectedTransient  bool
	hydrationCause      clientui.TranscriptRecoveryCause
	shouldDeferTail     bool
	duplicateToolStarts bool
}

func newProjectedTranscriptEventState(snapshot projectedTranscriptEventSnapshot) projectedTranscriptEventState {
	return projectedTranscriptEventState{
		entries:              append([]tui.TranscriptEntry(nil), snapshot.entries...),
		baseOffset:           snapshot.baseOffset,
		revision:             snapshot.revision,
		hasRuntimeClient:     snapshot.hasRuntimeClient,
		busy:                 snapshot.busy,
		liveAssistantPending: snapshot.liveAssistantPending,
	}
}

func reduceProjectedTranscriptEvent(state projectedTranscriptEventState, evt clientui.Event) projectedTranscriptReduction {
	incoming := cloneChatEntries(evt.TranscriptEntries)
	if shouldSkipProjectedToolCallStart(state, evt) {
		return projectedTranscriptReduction{
			decision:            projectedTranscriptDecisionSkip,
			plan:                projectedTranscriptEntryPlan{mode: projectedTranscriptEntryPlanSkip, entries: incoming},
			skipReason:          "duplicate_tool_call_start",
			duplicateToolStarts: true,
		}
	}
	plan := planProjectedTranscriptEntries(state, evt)
	reduction := projectedTranscriptReduction{
		decision:           projectedTranscriptDecisionApply,
		plan:               plan,
		projectedCommitted: evt.CommittedTranscriptChanged,
		hydrationCause:     evt.RecoveryCause,
	}
	reduction.projectedTransient = state.hasRuntimeClient && evt.Kind != clientui.EventConversationUpdated && !reduction.projectedCommitted
	switch plan.mode {
	case projectedTranscriptEntryPlanSkip:
		reduction.decision = projectedTranscriptDecisionSkip
		reduction.skipReason = "already_hydrated"
	case projectedTranscriptEntryPlanHydrate:
		reduction.decision = projectedTranscriptDecisionHydrate
	case projectedTranscriptEntryPlanAppend:
		if shouldDeferProjectedUserMessageFlushAppend(state, evt) {
			reduction.decision = projectedTranscriptDecisionDefer
			reduction.shouldDeferTail = true
			reduction.skipReason = "deferred_tail"
		}
	}
	return reduction
}

func planProjectedTranscriptEntries(state projectedTranscriptEventState, evt clientui.Event) projectedTranscriptEntryPlan {
	entries := cloneChatEntries(evt.TranscriptEntries)
	plan := projectedTranscriptEntryPlan{
		mode:       projectedTranscriptEntryPlanAppend,
		rangeStart: len(state.entries),
		rangeEnd:   len(state.entries),
		entries:    entries,
	}
	if len(entries) == 0 || !eventTranscriptEntriesReconcileWithCommittedTail(evt) {
		return plan
	}
	eventStart, eventEnd, ok := projectedTranscriptEventRange(evt, len(entries))
	if !ok {
		plan.divergence = "missing_event_range"
		return plan
	}
	if eventStart < 0 {
		return projectedTranscriptEntryPlan{mode: projectedTranscriptEntryPlanHydrate, divergence: "negative_event_start"}
	}
	currentStart := state.baseOffset
	currentEnd := currentStart + len(state.entries)
	if eventEnd <= currentStart {
		return projectedTranscriptEntryPlan{mode: projectedTranscriptEntryPlanSkip}
	}
	if eventStart < currentStart {
		trimmedPrefixCount := currentStart - eventStart
		if trimmedPrefixCount >= len(entries) {
			return projectedTranscriptEntryPlan{mode: projectedTranscriptEntryPlanSkip}
		}
		entries = cloneChatEntries(entries[trimmedPrefixCount:])
		eventStart = currentStart
		eventEnd = eventStart + len(entries)
	}
	if evt.TranscriptRevision < state.revision {
		if eventEnd > currentEnd {
			return projectedTranscriptEntryPlan{mode: projectedTranscriptEntryPlanHydrate, divergence: "stale_revision_extends_tail"}
		}
		if projectedTranscriptEntriesMatchCurrentRange(state, eventStart, entries, evt.CommittedTranscriptChanged) {
			return projectedTranscriptEntryPlan{mode: projectedTranscriptEntryPlanSkip}
		}
		return projectedTranscriptEntryPlan{mode: projectedTranscriptEntryPlanSkip}
	}
	if eventStart > currentEnd {
		return projectedTranscriptEntryPlan{mode: projectedTranscriptEntryPlanHydrate, divergence: "gap_after_tail"}
	}
	overlapStart := max(eventStart, currentStart)
	overlapEnd := min(eventEnd, currentEnd)
	if projectedTranscriptEntriesMatchCurrentOverlap(state, eventStart, overlapStart, overlapEnd, entries, evt.CommittedTranscriptChanged) {
		if eventEnd <= currentEnd {
			return projectedTranscriptEntryPlan{mode: projectedTranscriptEntryPlanSkip}
		}
		suffixStart := currentEnd - eventStart
		return projectedTranscriptEntryPlan{
			mode:       projectedTranscriptEntryPlanAppend,
			rangeStart: len(state.entries),
			rangeEnd:   len(state.entries),
			entries:    cloneChatEntries(entries[suffixStart:]),
		}
	}
	return projectedTranscriptEntryPlan{
		mode:       projectedTranscriptEntryPlanReplace,
		rangeStart: eventStart - currentStart,
		rangeEnd:   min(eventEnd, currentEnd) - currentStart,
		entries:    entries,
	}
}

func projectedTranscriptEntriesMatchCurrentRange(state projectedTranscriptEventState, eventStart int, entries []clientui.ChatEntry, requireCommitted bool) bool {
	currentStart := state.baseOffset
	currentEnd := currentStart + len(state.entries)
	eventEnd := eventStart + len(entries)
	if eventStart < currentStart || eventEnd > currentEnd {
		return false
	}
	return projectedTranscriptEntriesMatchCurrentOverlap(state, eventStart, eventStart, eventEnd, entries, requireCommitted)
}

func projectedTranscriptEntriesMatchCurrentOverlap(state projectedTranscriptEventState, eventStart int, overlapStart int, overlapEnd int, entries []clientui.ChatEntry, requireCommitted bool) bool {
	if overlapStart >= overlapEnd {
		return true
	}
	currentStart := state.baseOffset
	for absolute := overlapStart; absolute < overlapEnd; absolute++ {
		currentIndex := absolute - currentStart
		incomingIndex := absolute - eventStart
		if requireCommitted && state.entries[currentIndex].Transient && !state.entries[currentIndex].Committed {
			return false
		}
		if !transcript.EntryPayloadEqual(transcriptPayloadFromTUIEntry(state.entries[currentIndex]), transcriptPayloadFromClientEntry(entries[incomingIndex])) {
			return false
		}
	}
	return true
}

func (mode projectedTranscriptEntryPlanMode) label() string {
	switch mode {
	case projectedTranscriptEntryPlanSkip:
		return "skip"
	case projectedTranscriptEntryPlanAppend:
		return "append"
	case projectedTranscriptEntryPlanReplace:
		return "replace"
	case projectedTranscriptEntryPlanHydrate:
		return "hydrate"
	default:
		return "unknown"
	}
}

func shouldSkipProjectedToolCallStart(state projectedTranscriptEventState, evt clientui.Event) bool {
	if evt.Kind != clientui.EventToolCallStarted || len(evt.TranscriptEntries) == 0 {
		return false
	}
	matched := false
	for _, entry := range evt.TranscriptEntries {
		if tui.TranscriptRoleFromWire(entry.Role) != tui.TranscriptRoleToolCall {
			return false
		}
		toolCallID := strings.TrimSpace(entry.ToolCallID)
		if toolCallID == "" {
			return false
		}
		if evt.CommittedTranscriptChanged {
			if !transcriptContainsCommittedToolCallID(state.entries, toolCallID) {
				return false
			}
			matched = true
			continue
		}
		if !transcriptContainsToolCallID(state.entries, toolCallID) {
			return false
		}
		matched = true
	}
	return matched
}

func shouldDeferProjectedUserMessageFlushAppend(state projectedTranscriptEventState, evt clientui.Event) bool {
	if evt.Kind != clientui.EventUserMessageFlushed || len(evt.TranscriptEntries) == 0 {
		return false
	}
	if !state.busy {
		return false
	}
	if !state.liveAssistantPending {
		return false
	}
	for _, entry := range evt.TranscriptEntries {
		if tui.TranscriptRoleFromWire(entry.Role) != tui.TranscriptRoleUser {
			return false
		}
	}
	if !state.hasRuntimeClient {
		return true
	}
	committed := committedTranscriptEntriesForApp(state.entries)
	return len(committed) == 0
}
