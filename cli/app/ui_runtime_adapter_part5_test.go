package app

import (
	"strings"
	"testing"

	"core/cli/tui"
	"core/server/llm"
	"core/shared/clientui"
	"core/shared/transcript"
)

func TestUserMessageFlushedAlreadyCoveredByAuthoritativeTailDoesNotDuplicateNativeReplay(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "user", Text: "steered message"}}
	m.transcriptBaseOffset = 0
	m.transcriptTotalEntries = 1
	m.transcriptRevision = 10
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})

	cmd := m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventUserMessageFlushed,
		CommittedTranscriptChanged: true,
		UserMessage:                "steered message",
		TranscriptRevision:         10,
		CommittedEntryCount:        1,
		TranscriptEntries: []clientui.ChatEntry{{
			Role: "user",
			Text: "steered message",
		}},
	}, true).cmd
	if len(m.transcriptEntries) != 1 {
		t.Fatalf("expected stale flushed user message to be skipped, got %+v", m.transcriptEntries)
	}
	if cmd != nil {
		if _, ok := cmd().(nativeHistoryFlushMsg); ok {
			t.Fatal("expected no duplicate native replay after authoritative tail already covered the user message")
		}
	}
}

func TestQueuedUserMessageFailedStatusRestoresPendingInjectedInput(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.pendingInjected = []clientui.QueuedUserMessage{{ID: "queue-1", Text: "steered message"}}
	m.injectedQueue = []injectedRuntimeQueueItem{{LocalID: "local-1", ServerID: "queue-1", Text: "steered message", State: injectedRuntimeQueueEnqueued}}

	cmd := m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind: clientui.EventQueuedUserMessageStatus,
		QueuedUserMessageStatus: &clientui.QueuedUserMessageStatusEvent{
			QueueItemID:   "queue-1",
			Status:        clientui.QueuedUserMessageFailed,
			RestoreText:   "steered message",
			FailureReason: clientui.QueuedUserMessageFailureClosing,
		},
	}, true).cmd

	if len(m.pendingInjected) != 0 || len(m.injectedQueue) != 0 {
		t.Fatalf("expected failed queued item removed, pending=%+v queue=%+v", m.pendingInjected, m.injectedQueue)
	}
	if strings.TrimSpace(m.input) != "steered message" {
		t.Fatalf("input = %q, want restored queued text", m.input)
	}
	if m.transientStatus == "" || m.transientStatusKind != uiStatusNoticeError {
		t.Fatalf("expected transient queue failure status, got %q kind=%d", m.transientStatus, m.transientStatusKind)
	}
	if cmd == nil {
		t.Fatal("expected transient status clear command")
	}
}

func TestQueuedUserMessageSubmittedStatusRemovesPendingInjectedInput(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.pendingInjected = []clientui.QueuedUserMessage{{ID: "queue-1", Text: "steered message"}, {ID: "queue-2", Text: "follow-up"}}
	m.injectedQueue = []injectedRuntimeQueueItem{
		{LocalID: "local-1", ServerID: "queue-1", Text: "steered message", State: injectedRuntimeQueueEnqueued},
		{LocalID: "local-2", ServerID: "queue-2", Text: "follow-up", State: injectedRuntimeQueueEnqueued},
	}

	_ = m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind: clientui.EventQueuedUserMessageStatus,
		QueuedUserMessageStatus: &clientui.QueuedUserMessageStatusEvent{
			QueueItemID: "queue-1",
			Status:      clientui.QueuedUserMessageSubmitted,
		},
	}, true).cmd

	if len(m.pendingInjected) != 1 || m.pendingInjected[0].ID != "queue-2" {
		t.Fatalf("pending injected = %+v, want only queue-2", m.pendingInjected)
	}
	if len(m.injectedQueue) != 1 || m.injectedQueue[0].ServerID != "queue-2" {
		t.Fatalf("injected queue = %+v, want only queue-2", m.injectedQueue)
	}
	if m.input != "" {
		t.Fatalf("input = %q, want unchanged empty input", m.input)
	}
}

func TestProjectedUserMessageFlushedWithSameTextAndNewCommittedCountAppendsDistinctEntry(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "user", Text: "steered message"}}
	m.transcriptBaseOffset = 0
	m.transcriptTotalEntries = 1
	m.transcriptRevision = 10
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})

	cmd := m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventUserMessageFlushed,
		CommittedTranscriptChanged: true,
		UserMessage:                "steered message",
		TranscriptRevision:         11,
		CommittedEntryCount:        2,
		TranscriptEntries: []clientui.ChatEntry{{
			Role: "user",
			Text: "steered message",
		}},
	}, true).cmd
	if len(m.transcriptEntries) != 2 {
		t.Fatalf("expected repeated same-text user message to append distinctly, got %+v", m.transcriptEntries)
	}
	if cmd == nil {
		t.Fatal("expected native replay command for new committed user message")
	}
	if _, ok := cmd().(nativeHistoryFlushMsg); !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", cmd())
	}
}

func TestProjectedUserMessageFlushedDoesNotScheduleTranscriptRefresh(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true

	cmd := m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventUserMessageFlushed,
		CommittedTranscriptChanged: true,
		UserMessage:                "steered message",
		TranscriptEntries: []clientui.ChatEntry{{
			Role: "user",
			Text: "steered message",
		}},
	}, true).cmd
	msgs := collectCmdMessages(t, cmd)
	for _, msg := range msgs {
		if _, ok := msg.(runtimeTranscriptRefreshedMsg); ok {
			t.Fatalf("did not expect transcript refresh after flushed user message, got %+v", msgs)
		}
	}
	if got := len(m.transcriptEntries); got != 1 {
		t.Fatalf("expected immediate transcript append, got %d entries", got)
	}
	if got := m.transcriptEntries[0].Text; got != "steered message" {
		t.Fatalf("transcript entry text = %q, want steered message", got)
	}
}

func TestProjectedUserMessageFlushedAdvancesQueuedInputWithoutTranscriptRefresh(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.pendingInjected = queuedUserMessagesForTest("steered message", "follow-up")
	m.input = "steered message"
	m.lockedInjectText = "steered message"
	m.lockedInjectID = "queue-test-0"
	m.setInputSubmitLocked(true)

	cmd := m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                         clientui.EventUserMessageFlushed,
		CommittedTranscriptChanged:   true,
		UserMessage:                  "steered message",
		UserMessageBatchQueueItemIDs: []string{"queue-test-0"},
		TranscriptEntries: []clientui.ChatEntry{{
			Role: "user",
			Text: "steered message",
		}},
	}, true).cmd
	msgs := collectCmdMessages(t, cmd)
	for _, msg := range msgs {
		if _, ok := msg.(runtimeTranscriptRefreshedMsg); ok {
			t.Fatalf("did not expect transcript refresh after flushed injected user message, got %+v", msgs)
		}
	}
	if client.recordedPromptHistory != "" {
		t.Fatalf("did not expect queued flush to persist prompt history again, got %q", client.recordedPromptHistory)
	}
	if len(m.pendingInjected) != 1 || m.pendingInjected[0].Text != "follow-up" {
		t.Fatalf("expected pending injected queue advanced, got %+v", m.pendingInjected)
	}
	if m.input != "" {
		t.Fatalf("expected locked input cleared, got %q", m.input)
	}
	if m.isInputSubmitLocked() {
		t.Fatal("expected input submit lock cleared")
	}
}

func TestProjectedCommittedToolAndFinalEventsDoNotScheduleTranscriptRefresh(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "assistant", Text: "seed"}}
	m.transcriptTotalEntries = len(m.transcriptEntries)
	m.transcriptRevision = 10
	m.forwardToView(tui.SetConversationMsg{BaseOffset: 0, TotalEntries: len(m.transcriptEntries), Entries: m.transcriptEntries})
	m.layout().syncViewport()

	callMeta := transcript.ToolCallMeta{ToolName: "shell", Command: "pwd", CompactText: "pwd", IsShell: true}
	events := []clientui.Event{
		{
			Kind:                       clientui.EventUserMessageFlushed,
			CommittedTranscriptChanged: true,
			StepID:                     "step-1",
			TranscriptRevision:         11,
			CommittedEntryCount:        2,
			CommittedEntryStart:        1,
			CommittedEntryStartSet:     true,
			UserMessage:                "say hi",
			TranscriptEntries: []clientui.ChatEntry{{
				Role: "user",
				Text: "say hi",
			}},
		},
		{Kind: clientui.EventAssistantDelta, StepID: "step-1", AssistantDelta: "working"},
		{
			Kind:                       clientui.EventToolCallStarted,
			CommittedTranscriptChanged: true,
			StepID:                     "step-1",
			TranscriptRevision:         12,
			CommittedEntryCount:        3,
			CommittedEntryStart:        2,
			CommittedEntryStartSet:     true,
			TranscriptEntries: []clientui.ChatEntry{{
				Role:       "tool_call",
				Text:       "pwd",
				ToolCallID: "call-1",
				ToolCall:   transcriptToolCallMetaClient(&callMeta),
			}},
		},
		{
			Kind:                       clientui.EventToolCallCompleted,
			CommittedTranscriptChanged: true,
			StepID:                     "step-1",
			TranscriptRevision:         13,
			CommittedEntryCount:        4,
			CommittedEntryStart:        3,
			CommittedEntryStartSet:     true,
			TranscriptEntries: []clientui.ChatEntry{{
				Role:       "tool_result_ok",
				Text:       "$ pwd\n/tmp",
				ToolCallID: "call-1",
			}},
		},
		{
			Kind:                       clientui.EventAssistantMessage,
			CommittedTranscriptChanged: true,
			StepID:                     "step-1",
			TranscriptRevision:         14,
			CommittedEntryCount:        5,
			CommittedEntryStart:        4,
			CommittedEntryStartSet:     true,
			TranscriptEntries: []clientui.ChatEntry{{
				Role:  "assistant",
				Text:  "done",
				Phase: string(llm.MessagePhaseFinal),
			}},
		},
	}

	for _, evt := range events {
		msgs := collectCmdMessages(t, m.runtimeAdapter().applyProjectedRuntimeEvent(evt, true).cmd)
		for _, msg := range msgs {
			if _, ok := msg.(runtimeTranscriptRefreshedMsg); ok {
				t.Fatalf("did not expect committed runtime event to trigger transcript hydration, event=%s msgs=%+v", evt.Kind, msgs)
			}
		}
	}
	loaded := m.view.LoadedTranscriptEntries()
	if got, want := len(loaded), 5; got != want {
		t.Fatalf("loaded transcript entry count = %d, want %d (%+v)", got, want, loaded)
	}
	if got := loaded[0].Text; got != "seed" {
		t.Fatalf("loaded[0].Text = %q, want seed", got)
	}
	if got := loaded[1].Text; got != "say hi" {
		t.Fatalf("loaded[1].Text = %q, want say hi", got)
	}
	if got := loaded[2].Text; got != "pwd" {
		t.Fatalf("loaded[2].Text = %q, want pwd", got)
	}
	if got := loaded[4].Text; got != "done" {
		t.Fatalf("loaded[4].Text = %q, want done", got)
	}
}

func TestProjectedConversationUpdatedMatchingCommittedStateSkipsHydration(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	baseline := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     11,
		Offset:       0,
		TotalEntries: 2,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "seed"}, {Role: "assistant", Text: "committed after", Phase: string(llm.MessagePhaseFinal)}},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, baseline, clientui.TranscriptRecoveryCauseNone); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	cmd := m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventConversationUpdated,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         11,
		CommittedEntryCount:        2,
	}, true).cmd
	for _, msg := range collectCmdMessages(t, cmd) {
		if _, ok := msg.(runtimeTranscriptRefreshedMsg); ok {
			t.Fatalf("did not expect matching committed conversation_updated to trigger hydration, got %+v", msg)
		}
	}
}

func TestProjectedPlainConversationUpdatedNeverHydrates(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	baseline := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     11,
		Offset:       0,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, baseline, clientui.TranscriptRecoveryCauseNone); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	cmd := m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:   clientui.EventConversationUpdated,
		StepID: "step-1",
	}, true).cmd
	for _, msg := range collectCmdMessages(t, cmd) {
		if _, ok := msg.(runtimeTranscriptRefreshedMsg); ok {
			t.Fatalf("did not expect plain conversation_updated to trigger hydration, got %+v", msg)
		}
	}
	if m.runtimeTranscriptBusy {
		t.Fatal("did not expect runtime transcript sync to start for plain conversation_updated")
	}
}

func TestProjectedCommittedConversationUpdatedRequestsHydrationOnlyOnContinuityLoss(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	baseline := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     11,
		Offset:       0,
		TotalEntries: 2,
		Entries: []clientui.ChatEntry{
			{Role: "assistant", Text: "seed"},
			{Role: "assistant", Text: "committed after", Phase: string(llm.MessagePhaseFinal)},
		},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, baseline, clientui.TranscriptRecoveryCauseNone); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	client.transcript = clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     11,
		Offset:       0,
		TotalEntries: 3,
		Entries: []clientui.ChatEntry{
			{Role: "assistant", Text: "seed"},
			{Role: "assistant", Text: "committed after", Phase: string(llm.MessagePhaseFinal)},
			{Role: "reviewer_status", Text: "Supervisor ran: no changes."},
		},
	}

	cmd := m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventConversationUpdated,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         11,
		CommittedEntryCount:        3,
	}, true).cmd
	msgs := collectCmdMessages(t, cmd)
	var refresh runtimeTranscriptRefreshedMsg
	refreshFound := false
	for _, msg := range msgs {
		typed, ok := msg.(runtimeTranscriptRefreshedMsg)
		if !ok {
			continue
		}
		refresh = typed
		refreshFound = true
	}
	if !refreshFound {
		t.Fatalf("expected committed conversation_updated mismatch to request hydration, got %+v", msgs)
	}
	if refresh.syncCause != runtimeTranscriptSyncCauseCommittedConversation {
		t.Fatalf("committed conversation sync cause = %q, want %q", refresh.syncCause, runtimeTranscriptSyncCauseCommittedConversation)
	}
	if refresh.req.Window != clientui.TranscriptWindowOngoingTail {
		t.Fatalf("committed conversation request window = %q, want ongoing_tail", refresh.req.Window)
	}
}

func TestProjectedCommittedGapRequestsExplicitCommittedGapHydration(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	baseline := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, baseline, clientui.TranscriptRecoveryCauseNone); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	client.transcript = clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     11,
		Offset:       0,
		TotalEntries: 3,
		Entries: []clientui.ChatEntry{
			{Role: "assistant", Text: "seed"},
			{Role: "user", Text: "missing gap row"},
			{Role: "assistant", Text: "authoritative tail"},
		},
	}

	cmd := m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         11,
		CommittedEntryCount:        3,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:  "assistant",
			Text:  "authoritative tail",
			Phase: string(llm.MessagePhaseFinal),
		}},
	}, true).cmd
	msgs := collectCmdMessages(t, cmd)
	var refresh runtimeTranscriptRefreshedMsg
	refreshFound := false
	for _, msg := range msgs {
		typed, ok := msg.(runtimeTranscriptRefreshedMsg)
		if !ok {
			continue
		}
		refresh = typed
		refreshFound = true
	}
	if !refreshFound {
		t.Fatalf("expected committed gap to request runtime transcript refresh, got %+v", msgs)
	}
	if refresh.syncCause != runtimeTranscriptSyncCauseCommittedGap {
		t.Fatalf("committed gap sync cause = %q, want %q", refresh.syncCause, runtimeTranscriptSyncCauseCommittedGap)
	}
	if refresh.req.Window != clientui.TranscriptWindowOngoingTail {
		t.Fatalf("committed gap request window = %q, want ongoing_tail", refresh.req.Window)
	}
}

func TestProjectedUserMessageFlushedRequestsHydrationForCommittedGapWhileAssistantStreamIsLive(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.setBusy(true)
	m.pendingInjected = queuedUserMessagesForTest("steered message")
	m.input = "steered message"
	m.lockedInjectText = "steered message"
	m.lockedInjectID = "queue-test-0"
	m.setInputSubmitLocked(true)
	client.transcript = clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     7,
		TotalEntries: 2,
		Entries: []clientui.ChatEntry{
			{Role: "assistant", Text: "foreground done"},
			{Role: "user", Text: "steered message"},
		},
	}
	_ = m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventAssistantDelta, AssistantDelta: "foreground done"}, true).cmd

	cmd := m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                         clientui.EventUserMessageFlushed,
		CommittedTranscriptChanged:   true,
		TranscriptRevision:           7,
		CommittedEntryCount:          2,
		UserMessage:                  "steered message",
		UserMessageBatchQueueItemIDs: []string{"queue-test-0"},
		TranscriptEntries: []clientui.ChatEntry{{
			Role: "user",
			Text: "steered message",
		}},
	}, true).cmd
	msgs := collectCmdMessages(t, cmd)
	refresh, ok := msgs[0].(runtimeTranscriptRefreshedMsg)
	if !ok {
		t.Fatalf("expected committed gap hydration after queued user flush, got %+v", msgs)
	}
	if refresh.syncCause != runtimeTranscriptSyncCauseCommittedGap {
		t.Fatalf("sync cause = %q, want committed_gap", refresh.syncCause)
	}
	if len(m.transcriptEntries) != 0 {
		t.Fatalf("expected live user append to wait for authoritative hydrate when assistant commit is missing, got %+v", m.transcriptEntries)
	}
	if got := len(m.deferredCommittedTail); got != 0 {
		t.Fatalf("expected queued user flush to stop using deferred committed tail, got %d", got)
	}
	if got := m.view.OngoingStreamingText(); got != "" {
		t.Fatalf("expected committed gap hydration to clear stale live assistant text, got %q", got)
	}
	if client.recordedPromptHistory != "" {
		t.Fatalf("did not expect queued flush to persist prompt history again, got %q", client.recordedPromptHistory)
	}
	if len(m.pendingInjected) != 0 {
		t.Fatalf("expected pending injected queue consumed even while hydrate is pending, got %+v", m.pendingInjected)
	}
	queuedPane := strings.TrimSpace(stripANSIAndTrimRight(strings.Join(m.layout().renderQueuedMessagesPane(80), "\n")))
	if queuedPane != "" {
		t.Fatalf("expected flushed user message to leave queued pane once prompt history advances, got %q", queuedPane)
	}
	if m.isInputSubmitLocked() {
		t.Fatal("expected input submit lock cleared")
	}
	if m.input != "" {
		t.Fatalf("expected cleared input after deferred flushed user message, got %q", m.input)
	}
	if m.sawAssistantDelta {
		t.Fatal("expected committed gap hydration to clear assistant delta flag")
	}
}

func TestDeferredCommittedUserFlushRequestsTranscriptRefreshWhenRunEndsWithoutCatchUp(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.deferredCommittedTail = []deferredProjectedTranscriptTail{{
		rangeStart: 1,
		rangeEnd:   2,
		revision:   7,
		entries:    []clientui.ChatEntry{{Role: "user", Text: "steered message"}},
	}}

	cmd := m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:     clientui.EventRunStateChanged,
		RunState: &clientui.RunState{Lifecycle: clientui.IdleRunLifecycle()},
	}, true).cmd
	msgs := collectCmdMessages(t, cmd)
	refreshFound := false
	for _, msg := range msgs {
		if _, ok := msg.(runtimeTranscriptRefreshedMsg); ok {
			refreshFound = true
		}
	}
	if !refreshFound {
		t.Fatalf("expected deferred committed tail to request transcript refresh when run ends without catch-up, got %+v", msgs)
	}
	if got := len(m.deferredCommittedTail); got != 1 {
		t.Fatalf("expected deferred committed tail retained until hydration applies, got %d", got)
	}
}
