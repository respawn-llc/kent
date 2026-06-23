package app

import (
	"strings"
	"testing"

	"core/cli/tui"
	"core/server/llm"
	"core/shared/clientui"
)

func TestCommittedLocalEntryWhileAssistantStreamingIsDeferredUntilFinalCommit(t *testing.T) {
	m := newProjectedClosedUIModel(nil)
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.transcriptEntries = []tui.TranscriptEntry{{Role: tui.TranscriptRoleUser, Text: "prompt", Committed: true}}
	m.transcriptRevision = 1
	m.transcriptTotalEntries = 1
	m.forwardToView(tui.SetConversationMsg{BaseOffset: 0, TotalEntries: 1, Entries: m.transcriptEntries})

	_, streamCmd := m.handleRuntimeEventBatch([]clientui.Event{{
		Kind:           clientui.EventAssistantDelta,
		StepID:         "step-1",
		AssistantDelta: "final answer",
	}})
	_ = collectCmdMessages(t, streamCmd)

	_, localCmd := m.handleRuntimeEventBatch([]clientui.Event{{
		Kind:                       clientui.EventLocalEntryAdded,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		CommittedEntryStart:        1,
		CommittedEntryStartSet:     true,
		CommittedEntryCount:        2,
		TranscriptRevision:         2,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "system", Text: "local diagnostic"}},
	}})
	localFlush := collectNativeHistoryFlushText(collectCmdMessages(t, localCmd))
	if strings.Contains(localFlush, "local diagnostic") {
		t.Fatalf("local entry reached native scrollback while assistant was streaming: %q", localFlush)
	}
	if got := len(m.transcriptEntries); got != 1 {
		t.Fatalf("transcript entry count while gated = %d, want 1", got)
	}
	if got := len(m.deferredCommittedTail); got != 1 {
		t.Fatalf("deferred tail count = %d, want 1", got)
	}

	_, finalCmd := m.handleRuntimeEventBatch([]clientui.Event{{
		Kind:                       clientui.EventAssistantMessage,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		CommittedEntryStart:        2,
		CommittedEntryStartSet:     true,
		CommittedEntryCount:        3,
		TranscriptRevision:         3,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:  "assistant",
			Text:  "final answer",
			Phase: string(llm.MessagePhaseFinal),
		}},
	}})
	finalFlush := normalizedOutput(collectNativeHistoryFlushText(collectCmdMessages(t, finalCmd)))
	if got := len(m.deferredCommittedTail); got != 0 {
		t.Fatalf("deferred tail count after final commit = %d, want 0", got)
	}
	if got := m.view.OngoingStreamingText(); got != "" {
		t.Fatalf("streaming text after final commit = %q, want empty", got)
	}
	if !containsInOrder(finalFlush, "final answer", "local diagnostic") {
		t.Fatalf("final commit should flush assistant before deferred local entry, got %q", finalFlush)
	}
}

func TestCommittedToolStartWhileStreamingDoesNotReleaseDeferredTail(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.transcriptEntries = []tui.TranscriptEntry{{Role: tui.TranscriptRoleUser, Text: "prompt", Committed: true}}
	m.forwardToView(tui.SetConversationMsg{BaseOffset: 0, TotalEntries: 1, Entries: m.transcriptEntries, Ongoing: "streaming"})
	m.sawAssistantDelta = true
	m.deferredCommittedTail = []deferredProjectedTranscriptTail{{
		rangeStart: 1,
		rangeEnd:   2,
		revision:   2,
		entries:    []clientui.ChatEntry{{Role: "user", Text: "queued follow-up"}},
		pending:    []string{"queued follow-up"},
	}}

	cmd := m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventToolCallStarted,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         3,
		CommittedEntryCount:        3,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:       "tool_call",
			Text:       "pwd",
			ToolCallID: "call-1",
			ToolCall:   &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "pwd"},
		}},
	}, true).cmd
	flush := collectNativeHistoryFlushText(collectCmdMessages(t, cmd))
	if strings.Contains(flush, "queued follow-up") {
		t.Fatalf("tool start released deferred row into native scrollback: %q", flush)
	}
	if got := len(m.deferredCommittedTail); got != 1 {
		t.Fatalf("deferred tail count = %d, want 1", got)
	}
	if got := len(m.transcriptEntries); got != 2 {
		t.Fatalf("transcript entry count = %d, want pending tool row appended", got)
	}
	if got := len(committedTranscriptEntriesForApp(m.transcriptEntries)); got != 1 {
		t.Fatalf("committed ongoing prefix length = %d, want unresolved tool hidden from scrollback", got)
	}

	m.forwardToView(tui.ClearOngoingAssistantMsg{})
	m.sawAssistantDelta = false
	m.resetNativeStreamingState()
	_ = collectCmdMessages(t, m.drainDeferredCommittedDeliveryIfUnblocked())
	if got := len(m.deferredCommittedTail); got != 0 {
		t.Fatalf("deferred tail count after drain = %d, want 0", got)
	}
	foundDeferredRow := false
	for _, entry := range m.transcriptEntries {
		if entry.Text == "queued follow-up" {
			foundDeferredRow = true
			break
		}
	}
	if !foundDeferredRow {
		t.Fatalf("deferred row was dropped after live-only tool start, transcript=%+v", m.transcriptEntries)
	}
}

func TestCommittedSuffixResponseWhileAssistantStreamingIsDeferredWithoutHasMoreContinuation(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.runtimeCommittedSuffixToken = 1
	m.forwardToView(tui.SetConversationMsg{Ongoing: "streaming"})
	m.sawAssistantDelta = true

	cmd := m.handleRuntimeCommittedTranscriptSuffixRefreshed(runtimeCommittedTranscriptSuffixRefreshedMsg{
		token: 1,
		req:   clientui.CommittedTranscriptSuffixRequest{AfterEntryCount: 0, Limit: 1},
		suffix: clientui.CommittedTranscriptSuffix{
			SessionID:           "session-1",
			Revision:            2,
			CommittedEntryCount: 2,
			StartEntryCount:     0,
			NextEntryCount:      1,
			HasMore:             true,
			Entries:             []clientui.ChatEntry{{Role: "system", Text: "local diagnostic"}},
		},
	})
	msgs := collectCmdMessages(t, cmd)
	for _, msg := range msgs {
		if _, ok := msg.(runtimeCommittedTranscriptSuffixRefreshedMsg); ok {
			t.Fatalf("gated suffix response requested HasMore continuation: %+v", msg)
		}
	}
	if got := len(m.transcriptEntries); got != 0 {
		t.Fatalf("suffix response mutated transcript while gated: %+v", m.transcriptEntries)
	}
	if !m.deferredCommittedSuffixRefreshSet {
		t.Fatal("expected gated suffix response to schedule deferred suffix refetch")
	}
}

func TestCommittedSuffixRequestWhileAssistantStreamingDefersThroughGate(t *testing.T) {
	client := &runtimeClientWithoutCachedMainView{
		mainView: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{SessionID: "session-1"}},
	}
	m := newProjectedClosedUIModel(client)
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	_, streamCmd := m.handleRuntimeEventBatch([]clientui.Event{{
		Kind:           clientui.EventAssistantDelta,
		StepID:         "step-1",
		AssistantDelta: "streaming",
	}})
	_ = collectCmdMessages(t, streamCmd)
	if !m.ongoingCommittedScrollbackGateActive() {
		t.Fatal("expected assistant stream gate to be active before committed event")
	}

	evt := clientui.Event{
		Kind:                       clientui.EventLocalEntryAdded,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		CommittedEntryStart:        0,
		CommittedEntryStartSet:     true,
		CommittedEntryCount:        1,
		TranscriptRevision:         1,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "system", Text: "local diagnostic"}},
	}
	if shouldDeliverCommittedRuntimeEventFromSuffix(m, evt) {
		t.Fatal("suffix request predicate allowed committed delivery while assistant stream is live")
	}
	_, cmd := m.handleRuntimeEventBatch([]clientui.Event{evt})
	_ = collectCmdMessages(t, cmd)
	if got := len(m.deferredCommittedTail); got != 1 {
		t.Fatalf("deferred tail count = %d, want 1", got)
	}
}

func TestCommittedSuffixFinalizerResponseAppliesWhileGateIsActive(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.runtimeCommittedSuffixToken = 1
	m.forwardToView(tui.SetConversationMsg{Ongoing: "final answer"})
	m.sawAssistantDelta = true
	m.nativeStreamingController = newNativeAssistantStreamController(m.theme, m.nativeReplayRenderWidth())
	m.nativeStreamingController.ApplySource("final answer", m.theme, m.nativeReplayRenderWidth())
	m.nativeStreamingText = "final answer"

	cmd := m.handleRuntimeCommittedTranscriptSuffixRefreshed(runtimeCommittedTranscriptSuffixRefreshedMsg{
		token: 1,
		req:   clientui.CommittedTranscriptSuffixRequest{AfterEntryCount: 0, Limit: 1},
		suffix: clientui.CommittedTranscriptSuffix{
			SessionID:           "session-1",
			Revision:            2,
			CommittedEntryCount: 1,
			StartEntryCount:     0,
			NextEntryCount:      1,
			Entries: []clientui.ChatEntry{{
				Role:  "assistant",
				Text:  "final answer",
				Phase: string(llm.MessagePhaseFinal),
			}},
		},
	})
	flush := normalizedOutput(collectNativeHistoryFlushText(collectCmdMessages(t, cmd)))
	if !strings.Contains(flush, "final answer") {
		t.Fatalf("finalizer suffix response did not reach native scrollback, got %q", flush)
	}
	if got := len(m.transcriptEntries); got != 1 {
		t.Fatalf("transcript entry count = %d, want finalizer applied", got)
	}
	if got := m.view.OngoingStreamingText(); got != "" {
		t.Fatalf("streaming text after finalizer suffix = %q, want empty", got)
	}
	if m.deferredCommittedSuffixRefreshSet {
		t.Fatal("finalizer suffix response should not schedule deferred suffix refetch")
	}
}

func TestCommittedSuffixFinalizerResponseAppliesFromNativeStreamingSourceOnly(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.runtimeCommittedSuffixToken = 1
	m.nativeStreamingController = newNativeAssistantStreamController(m.theme, m.nativeReplayRenderWidth())
	m.nativeStreamingController.ApplySource("final answer", m.theme, m.nativeReplayRenderWidth())

	if !m.ongoingCommittedScrollbackGateActive() {
		t.Fatal("expected native streaming source to activate committed scrollback gate")
	}
	cmd := m.handleRuntimeCommittedTranscriptSuffixRefreshed(runtimeCommittedTranscriptSuffixRefreshedMsg{
		token: 1,
		req:   clientui.CommittedTranscriptSuffixRequest{AfterEntryCount: 0, Limit: 1},
		suffix: clientui.CommittedTranscriptSuffix{
			SessionID:           "session-1",
			Revision:            2,
			CommittedEntryCount: 1,
			StartEntryCount:     0,
			NextEntryCount:      1,
			Entries: []clientui.ChatEntry{{
				Role:  "assistant",
				Text:  "final answer",
				Phase: string(llm.MessagePhaseFinal),
			}},
		},
	})
	flush := normalizedOutput(collectNativeHistoryFlushText(collectCmdMessages(t, cmd)))
	if !strings.Contains(flush, "final answer") {
		t.Fatalf("finalizer suffix response from native source did not flush, got %q", flush)
	}
	if m.deferredCommittedSuffixRefreshSet {
		t.Fatal("native-source finalizer suffix response should not be deferred")
	}
}

func TestCommittedSuffixFinalizerResponseDoesNotBypassGateWhenActiveStepIsKnown(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.runtimeCommittedSuffixToken = 1
	m.forwardToView(tui.SetConversationMsg{Ongoing: "same final text"})
	m.sawAssistantDelta = true
	m.nativeStreamingStepID = "new-step"
	m.nativeStreamingController = newNativeAssistantStreamController(m.theme, m.nativeReplayRenderWidth())
	m.nativeStreamingController.ApplySource("same final text", m.theme, m.nativeReplayRenderWidth())
	m.nativeStreamingText = "same final text"

	cmd := m.handleRuntimeCommittedTranscriptSuffixRefreshed(runtimeCommittedTranscriptSuffixRefreshedMsg{
		token: 1,
		req:   clientui.CommittedTranscriptSuffixRequest{AfterEntryCount: 0, Limit: 1},
		suffix: clientui.CommittedTranscriptSuffix{
			SessionID:           "session-1",
			Revision:            2,
			CommittedEntryCount: 1,
			StartEntryCount:     0,
			NextEntryCount:      1,
			Entries: []clientui.ChatEntry{{
				Role:  "assistant",
				Text:  "same final text",
				Phase: string(llm.MessagePhaseFinal),
			}},
		},
	})

	flush := normalizedOutput(collectNativeHistoryFlushText(collectCmdMessages(t, cmd)))
	if strings.Contains(flush, "same final text") {
		t.Fatalf("step-ambiguous suffix finalizer reached native scrollback while gate active: %q", flush)
	}
	if got := len(m.transcriptEntries); got != 0 {
		t.Fatalf("transcript entry count = %d, want suffix deferred", got)
	}
	if got := m.view.OngoingStreamingText(); got != "same final text" {
		t.Fatalf("live stream = %q, want preserved", got)
	}
	if !m.deferredCommittedSuffixRefreshSet {
		t.Fatal("step-ambiguous suffix finalizer should schedule deferred suffix refetch")
	}
}

func TestCommittedSuffixGateEvaluatesTrimmedSuffixRows(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.runtimeCommittedSuffixToken = 1
	m.transcriptEntries = []tui.TranscriptEntry{{Role: tui.TranscriptRoleAssistant, Text: "final answer", Committed: true}}
	m.transcriptRevision = 1
	m.transcriptTotalEntries = 1
	m.ongoingCommittedDelivery = newOngoingCommittedDeliveryCursor(1, 1)
	m.forwardToView(tui.SetConversationMsg{
		BaseOffset:   0,
		TotalEntries: 1,
		Entries:      m.transcriptEntries,
		Ongoing:      "final answer",
	})
	m.sawAssistantDelta = true
	m.nativeStreamingController = newNativeAssistantStreamController(m.theme, m.nativeReplayRenderWidth())
	m.nativeStreamingController.ApplySource("final answer", m.theme, m.nativeReplayRenderWidth())
	m.nativeStreamingText = "final answer"

	cmd := m.handleRuntimeCommittedTranscriptSuffixRefreshed(runtimeCommittedTranscriptSuffixRefreshedMsg{
		token: 1,
		req:   clientui.CommittedTranscriptSuffixRequest{AfterEntryCount: 0, Limit: 2},
		suffix: clientui.CommittedTranscriptSuffix{
			SessionID:           "session-1",
			Revision:            2,
			CommittedEntryCount: 2,
			StartEntryCount:     0,
			NextEntryCount:      2,
			Entries: []clientui.ChatEntry{
				{
					Role:  "assistant",
					Text:  "final answer",
					Phase: string(llm.MessagePhaseFinal),
				},
				{Role: "system", Text: "overlapped committed row"},
			},
		},
	})

	flush := normalizedOutput(collectNativeHistoryFlushText(collectCmdMessages(t, cmd)))
	if strings.Contains(flush, "overlapped committed row") {
		t.Fatalf("trimmed non-finalizer suffix row reached native scrollback while gate active: %q", flush)
	}
	if got := len(m.transcriptEntries); got != 1 {
		t.Fatalf("transcript entry count = %d, want overlapped suffix deferred", got)
	}
	if !m.deferredCommittedSuffixRefreshSet {
		t.Fatal("trimmed non-finalizer suffix should schedule deferred suffix refetch")
	}
}

func TestSuffixGatePredicatesSeparateFinalizersFromCommittedRows(t *testing.T) {
	client := &runtimeClientWithoutCachedMainView{
		mainView: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{SessionID: "session-1"}},
	}
	m := newProjectedClosedUIModel(client)
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.forwardToView(tui.SetConversationMsg{Ongoing: "streaming"})
	m.sawAssistantDelta = true

	committedLocal := clientui.Event{
		Kind:                       clientui.EventLocalEntryAdded,
		CommittedTranscriptChanged: true,
		CommittedEntryCount:        1,
		TranscriptRevision:         1,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "system", Text: "local diagnostic"}},
	}
	if shouldDeliverCommittedRuntimeEventFromSuffix(m, committedLocal) {
		t.Fatal("committed local row should not start suffix delivery while stream gate is active")
	}

	streamFinalizer := clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		CommittedTranscriptChanged: true,
		CommittedEntryCount:        1,
		TranscriptRevision:         1,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "assistant", Text: "streaming", Phase: string(llm.MessagePhaseCommentary)}},
	}
	if shouldDeliverCommittedRuntimeEventFromSuffix(m, streamFinalizer) {
		t.Fatal("assistant stream finalizer should use direct finalization while stream gate is active")
	}

	if !m.shouldGateCommittedSuffixResponse(clientui.CommittedTranscriptSuffix{
		Entries: []clientui.ChatEntry{{Role: "system", Text: "local diagnostic"}},
	}) {
		t.Fatal("non-finalizer suffix response should be gated while stream is active")
	}
	if m.shouldGateCommittedSuffixResponse(clientui.CommittedTranscriptSuffix{
		Entries: []clientui.ChatEntry{{Role: "assistant", Text: "streaming", Phase: string(llm.MessagePhaseCommentary)}},
	}) {
		t.Fatal("assistant stream finalizer suffix response should not be gated")
	}
}

func TestAssistantFinalizerWhileStreamingAppliesBeforeDeferredRowsWithoutSuffixRequest(t *testing.T) {
	client := &runtimeClientWithoutCachedMainView{
		mainView: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{
			SessionID: "session-1",
			Transcript: clientui.TranscriptMetadata{
				Revision:            3,
				CommittedEntryCount: 3,
			},
		}},
	}
	m := newProjectedClosedUIModel(client)
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.transcriptEntries = []tui.TranscriptEntry{{Role: tui.TranscriptRoleUser, Text: "prompt", Committed: true}}
	m.transcriptRevision = 1
	m.transcriptTotalEntries = 1
	m.forwardToView(tui.SetConversationMsg{BaseOffset: 0, TotalEntries: 1, Entries: m.transcriptEntries, Ongoing: "final answer"})
	m.sawAssistantDelta = true
	m.deferredCommittedTail = []deferredProjectedTranscriptTail{{
		rangeStart: 1,
		rangeEnd:   2,
		revision:   2,
		entries:    []clientui.ChatEntry{{Role: "system", Text: "local diagnostic"}},
	}}

	_, cmd := m.handleRuntimeEventBatch([]clientui.Event{{
		Kind:                       clientui.EventAssistantMessage,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		CommittedEntryStart:        2,
		CommittedEntryStartSet:     true,
		CommittedEntryCount:        3,
		TranscriptRevision:         3,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:  "assistant",
			Text:  "final answer",
			Phase: string(llm.MessagePhaseFinal),
		}},
	}})

	msgs := collectCmdMessages(t, cmd)
	for _, msg := range msgs {
		if _, ok := msg.(runtimeCommittedTranscriptSuffixRefreshedMsg); ok {
			t.Fatalf("assistant finalizer should not request suffix while stream gate is active, got %+v", msgs)
		}
	}
	flush := normalizedOutput(collectNativeHistoryFlushText(msgs))
	if !containsInOrder(flush, "final answer", "local diagnostic") {
		t.Fatalf("assistant finalizer should flush before deferred row, got %q", flush)
	}
	if got := len(m.deferredCommittedTail); got != 0 {
		t.Fatalf("deferred tail count = %d, want drained after assistant finalizer", got)
	}
	if got := m.view.OngoingStreamingText(); got != "" {
		t.Fatalf("live assistant stream = %q, want cleared after finalizer applies", got)
	}
}

func TestDeferredTailDrainAdvancesDeliveryCursorOnlyThroughActuallyEmittedPrefix(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.transcriptEntries = []tui.TranscriptEntry{{Role: tui.TranscriptRoleUser, Text: "prompt", Committed: true}}
	m.transcriptRevision = 1
	m.transcriptTotalEntries = 4
	m.forwardToView(tui.SetConversationMsg{BaseOffset: 0, TotalEntries: 4, Entries: m.transcriptEntries})
	m.ongoingCommittedDelivery = newOngoingCommittedDeliveryCursor(1, 1)
	m.deferredCommittedTail = []deferredProjectedTranscriptTail{
		{
			rangeStart: 1,
			rangeEnd:   2,
			revision:   2,
			entries:    []clientui.ChatEntry{{Role: "system", Text: "first deferred"}},
		},
		{
			rangeStart: 3,
			rangeEnd:   4,
			revision:   4,
			entries:    []clientui.ChatEntry{{Role: "system", Text: "after gap"}},
		},
	}

	_ = collectCmdMessages(t, m.drainDeferredCommittedDeliveryIfUnblocked())

	if got := committedTranscriptTailEnd(m); got != 2 {
		t.Fatalf("committed delivery cursor = %d, want 2 because only first deferred row was contiguous/emitted", got)
	}
	if got := len(m.deferredCommittedTail); got != 1 {
		t.Fatalf("deferred tail count = %d, want gapped tail preserved", got)
	}
}

func TestStaleFinalAssistantCommitDoesNotClearNewerLiveStreamOrReleaseDeferredRows(t *testing.T) {
	client := &runtimeClientWithoutCachedMainView{
		mainView: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{
			SessionID: "session-1",
			Transcript: clientui.TranscriptMetadata{
				Revision:            3,
				CommittedEntryCount: 3,
			},
		}},
	}
	m := newProjectedClosedUIModel(client)
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.transcriptEntries = []tui.TranscriptEntry{{Role: tui.TranscriptRoleUser, Text: "prompt", Committed: true}}
	m.transcriptRevision = 1
	m.transcriptTotalEntries = 1
	m.forwardToView(tui.SetConversationMsg{BaseOffset: 0, TotalEntries: 1, Entries: m.transcriptEntries, Ongoing: "newer live stream"})
	m.sawAssistantDelta = true
	m.nativeStreamingStepID = "new-step"
	m.deferredCommittedTail = []deferredProjectedTranscriptTail{{
		rangeStart: 1,
		rangeEnd:   2,
		revision:   2,
		entries:    []clientui.ChatEntry{{Role: "system", Text: "local diagnostic"}},
	}}

	_, cmd := m.handleRuntimeEventBatch([]clientui.Event{{
		Kind:                       clientui.EventAssistantMessage,
		StepID:                     "old-step",
		CommittedTranscriptChanged: true,
		CommittedEntryStart:        2,
		CommittedEntryStartSet:     true,
		CommittedEntryCount:        3,
		TranscriptRevision:         3,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:  "assistant",
			Text:  "newer live stream",
			Phase: string(llm.MessagePhaseFinal),
		}},
	}})

	flush := normalizedOutput(collectNativeHistoryFlushText(collectCmdMessages(t, cmd)))
	if strings.Contains(flush, "local diagnostic") {
		t.Fatalf("stale final assistant commit released deferred row: %q", flush)
	}
	if got := m.view.OngoingStreamingText(); got != "newer live stream" {
		t.Fatalf("live assistant stream = %q, want newer stream preserved", got)
	}
	if got := len(m.deferredCommittedTail); got != 1 {
		t.Fatalf("deferred tail count = %d, want still deferred", got)
	}
}

func TestDeferredTailDrainPreservesLiveOnlyToolStartAfterEarlierDeferredRow(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.transcriptEntries = []tui.TranscriptEntry{{Role: tui.TranscriptRoleUser, Text: "prompt", Committed: true}}
	m.forwardToView(tui.SetConversationMsg{BaseOffset: 0, TotalEntries: 1, Entries: m.transcriptEntries, Ongoing: "streaming"})
	m.sawAssistantDelta = true
	m.deferredCommittedTail = []deferredProjectedTranscriptTail{{
		rangeStart: 1,
		rangeEnd:   2,
		revision:   2,
		entries:    []clientui.ChatEntry{{Role: "user", Text: "queued follow-up"}},
		pending:    []string{"queued follow-up"},
	}}

	_ = m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventToolCallStarted,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         3,
		CommittedEntryCount:        3,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:       "tool_call",
			Text:       "pwd",
			ToolCallID: "call-1",
			ToolCall:   &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "pwd"},
		}},
	}, true).cmd

	m.forwardToView(tui.ClearOngoingAssistantMsg{})
	m.sawAssistantDelta = false
	m.resetNativeStreamingState()
	_ = collectCmdMessages(t, m.drainDeferredCommittedDeliveryIfUnblocked())

	foundTool := false
	for _, entry := range m.transcriptEntries {
		if entry.Role == tui.TranscriptRoleToolCall && entry.ToolCallID == "call-1" {
			foundTool = true
			break
		}
	}
	if !foundTool {
		t.Fatalf("live-only tool start was dropped after draining earlier deferred row, transcript=%+v", m.transcriptEntries)
	}
}

func TestRunIdleClearsAwaitingNativeStreamingCommitAndDrainsDeferredTail(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.nativeStreamingAwaitingCommit = true
	m.nativeStreamingController = newNativeAssistantStreamController(m.theme, m.nativeReplayRenderWidth())
	m.nativeStreamingController.ApplySource("orphaned stream", m.theme, m.nativeReplayRenderWidth())
	m.nativeStreamingText = "orphaned stream"
	m.deferredCommittedTail = []deferredProjectedTranscriptTail{{
		rangeStart: 0,
		rangeEnd:   1,
		revision:   1,
		entries:    []clientui.ChatEntry{{Role: "system", Text: "deferred after orphaned stream"}},
	}}

	_, cmd := m.handleRuntimeEventBatch([]clientui.Event{{
		Kind:     clientui.EventRunStateChanged,
		RunState: &clientui.RunState{Lifecycle: clientui.IdleRunLifecycle()},
	}})
	_ = collectCmdMessages(t, cmd)

	if m.nativeStreamingAwaitingCommit {
		t.Fatal("expected idle run state to clear awaiting native streaming commit marker")
	}
	if got := strings.TrimSpace(m.nativeStreamingController.source); got != "" {
		t.Fatalf("native streaming source = %q, want cleared", got)
	}
	if got := len(m.deferredCommittedTail); got != 0 {
		t.Fatalf("deferred tail count = %d, want drained after idle recovery", got)
	}
	if got := len(m.transcriptEntries); got != 1 || m.transcriptEntries[0].Text != "deferred after orphaned stream" {
		t.Fatalf("transcript entries after idle recovery = %+v", m.transcriptEntries)
	}
}
