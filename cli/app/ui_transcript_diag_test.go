package app

import (
	"context"
	"strings"
	"testing"

	"builder/cli/tui"
	"builder/shared/client"
	"builder/shared/clientui"
	"builder/shared/serverapi"
)

type transcriptDiagTestSessionViewClient struct{}

func (transcriptDiagTestSessionViewClient) GetSessionMainView(context.Context, serverapi.SessionMainViewRequest) (serverapi.SessionMainViewResponse, error) {
	return serverapi.SessionMainViewResponse{}, nil
}

func (transcriptDiagTestSessionViewClient) GetSessionTranscriptPage(context.Context, serverapi.SessionTranscriptPageRequest) (serverapi.SessionTranscriptPageResponse, error) {
	return serverapi.SessionTranscriptPageResponse{}, nil
}

func (transcriptDiagTestSessionViewClient) GetRun(context.Context, serverapi.RunGetRequest) (serverapi.RunGetResponse, error) {
	return serverapi.RunGetResponse{}, nil
}

type transcriptDiagTestRuntimeControlClient struct{}

func (transcriptDiagTestRuntimeControlClient) SetSessionName(context.Context, serverapi.RuntimeSetSessionNameRequest) error {
	return nil
}
func (transcriptDiagTestRuntimeControlClient) SetThinkingLevel(context.Context, serverapi.RuntimeSetThinkingLevelRequest) error {
	return nil
}
func (transcriptDiagTestRuntimeControlClient) SetFastModeEnabled(context.Context, serverapi.RuntimeSetFastModeEnabledRequest) (serverapi.RuntimeSetFastModeEnabledResponse, error) {
	return serverapi.RuntimeSetFastModeEnabledResponse{}, nil
}
func (transcriptDiagTestRuntimeControlClient) SetReviewerEnabled(context.Context, serverapi.RuntimeSetReviewerEnabledRequest) (serverapi.RuntimeSetReviewerEnabledResponse, error) {
	return serverapi.RuntimeSetReviewerEnabledResponse{}, nil
}
func (transcriptDiagTestRuntimeControlClient) SetAutoCompactionEnabled(context.Context, serverapi.RuntimeSetAutoCompactionEnabledRequest) (serverapi.RuntimeSetAutoCompactionEnabledResponse, error) {
	return serverapi.RuntimeSetAutoCompactionEnabledResponse{}, nil
}
func (transcriptDiagTestRuntimeControlClient) SetQuestionsEnabled(context.Context, serverapi.RuntimeSetQuestionsEnabledRequest) (serverapi.RuntimeSetQuestionsEnabledResponse, error) {
	return serverapi.RuntimeSetQuestionsEnabledResponse{}, nil
}
func (transcriptDiagTestRuntimeControlClient) AppendLocalEntry(context.Context, serverapi.RuntimeAppendLocalEntryRequest) error {
	return nil
}
func (transcriptDiagTestRuntimeControlClient) ShouldCompactBeforeUserMessage(context.Context, serverapi.RuntimeShouldCompactBeforeUserMessageRequest) (serverapi.RuntimeShouldCompactBeforeUserMessageResponse, error) {
	return serverapi.RuntimeShouldCompactBeforeUserMessageResponse{}, nil
}
func (transcriptDiagTestRuntimeControlClient) SubmitUserMessage(context.Context, serverapi.RuntimeSubmitUserMessageRequest) (serverapi.RuntimeSubmitUserMessageResponse, error) {
	return serverapi.RuntimeSubmitUserMessageResponse{}, nil
}
func (transcriptDiagTestRuntimeControlClient) SubmitUserTurn(context.Context, serverapi.RuntimeSubmitUserTurnRequest) (serverapi.RuntimeSubmitUserTurnResponse, error) {
	return serverapi.RuntimeSubmitUserTurnResponse{}, nil
}
func (transcriptDiagTestRuntimeControlClient) SubmitUserShellCommand(context.Context, serverapi.RuntimeSubmitUserShellCommandRequest) error {
	return nil
}
func (transcriptDiagTestRuntimeControlClient) CompactContext(context.Context, serverapi.RuntimeCompactContextRequest) error {
	return nil
}
func (transcriptDiagTestRuntimeControlClient) CompactContextForPreSubmit(context.Context, serverapi.RuntimeCompactContextForPreSubmitRequest) error {
	return nil
}
func (transcriptDiagTestRuntimeControlClient) HasQueuedUserWork(context.Context, serverapi.RuntimeHasQueuedUserWorkRequest) (serverapi.RuntimeHasQueuedUserWorkResponse, error) {
	return serverapi.RuntimeHasQueuedUserWorkResponse{}, nil
}
func (transcriptDiagTestRuntimeControlClient) SubmitQueuedUserMessages(context.Context, serverapi.RuntimeSubmitQueuedUserMessagesRequest) (serverapi.RuntimeSubmitQueuedUserMessagesResponse, error) {
	return serverapi.RuntimeSubmitQueuedUserMessagesResponse{}, nil
}
func (transcriptDiagTestRuntimeControlClient) Interrupt(context.Context, serverapi.RuntimeInterruptRequest) error {
	return nil
}
func (transcriptDiagTestRuntimeControlClient) QueueUserMessage(context.Context, serverapi.RuntimeQueueUserMessageRequest) (serverapi.RuntimeQueueUserMessageResponse, error) {
	return serverapi.RuntimeQueueUserMessageResponse{}, nil
}
func (transcriptDiagTestRuntimeControlClient) DiscardQueuedUserMessage(context.Context, serverapi.RuntimeDiscardQueuedUserMessageRequest) (serverapi.RuntimeDiscardQueuedUserMessageResponse, error) {
	return serverapi.RuntimeDiscardQueuedUserMessageResponse{}, nil
}
func (transcriptDiagTestRuntimeControlClient) RecordPromptHistory(context.Context, serverapi.RuntimeRecordPromptHistoryRequest) error {
	return nil
}
func (transcriptDiagTestRuntimeControlClient) ShowGoal(context.Context, serverapi.RuntimeGoalShowRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return serverapi.RuntimeGoalShowResponse{}, nil
}
func (transcriptDiagTestRuntimeControlClient) SetGoal(context.Context, serverapi.RuntimeGoalSetRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return serverapi.RuntimeGoalShowResponse{}, nil
}
func (transcriptDiagTestRuntimeControlClient) PauseGoal(context.Context, serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return serverapi.RuntimeGoalShowResponse{}, nil
}
func (transcriptDiagTestRuntimeControlClient) ResumeGoal(context.Context, serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return serverapi.RuntimeGoalShowResponse{}, nil
}
func (transcriptDiagTestRuntimeControlClient) CompleteGoal(context.Context, serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return serverapi.RuntimeGoalShowResponse{}, nil
}
func (transcriptDiagTestRuntimeControlClient) ClearGoal(context.Context, serverapi.RuntimeGoalClearRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return serverapi.RuntimeGoalShowResponse{}, nil
}

var _ client.SessionViewClient = transcriptDiagTestSessionViewClient{}
var _ client.RuntimeControlClient = transcriptDiagTestRuntimeControlClient{}

func TestProjectedRuntimeEventLogsTranscriptDiagnostics(t *testing.T) {
	logger := &testUILogger{}
	m := newProjectedStaticUIModel(
		WithUILogger(logger),
		WithUITranscriptDiagnostics(true),
		WithUISessionID("session-1"),
	)

	_ = m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:           clientui.EventAssistantDelta,
		StepID:         "step-1",
		AssistantDelta: "working",
		TranscriptEntries: []clientui.ChatEntry{{
			Role: "assistant",
			Text: "working",
		}},
	}, true).cmd

	joined := strings.Join(logger.lines, "\n")
	if !strings.Contains(joined, "transcript.diag.client.apply_event") {
		t.Fatalf("expected event diagnostics, got %q", joined)
	}
	if !strings.Contains(joined, "transcript.diag.client.append_entries") {
		t.Fatalf("expected append diagnostics, got %q", joined)
	}
	if !strings.Contains(joined, "session_id=session-1") {
		t.Fatalf("expected session id in diagnostics, got %q", joined)
	}
}

func TestProjectedRuntimeEventLogsTranscriptDiagnosticsInDebugMode(t *testing.T) {
	logger := &testUILogger{}
	m := newProjectedStaticUIModel(
		WithUILogger(logger),
		WithUIDebug(true),
		WithUISessionID("session-1"),
	)

	_ = m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                clientui.EventToolCallStarted,
		StepID:              "step-1",
		TranscriptRevision:  7,
		CommittedEntryCount: 3,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:       "tool_call",
			ToolCallID: "call-1",
			Text:       "shell",
		}},
	}, true).cmd

	joined := strings.Join(logger.lines, "\n")
	if !strings.Contains(joined, "transcript.diag.client.projected_plan") {
		t.Fatalf("expected projected plan diagnostics in debug mode, got %q", joined)
	}
	if !strings.Contains(joined, "event_revision=7") {
		t.Fatalf("expected revision field in debug diagnostics, got %q", joined)
	}
}

func TestRuntimeTranscriptPageLogsRejectReason(t *testing.T) {
	logger := &testUILogger{}
	m := newProjectedStaticUIModel(
		WithUILogger(logger),
		WithUITranscriptDiagnostics(true),
		WithUISessionID("session-1"),
	)
	m.transcriptRevision = 10
	m.transcriptLiveDirty = true
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "assistant", Text: "seed"}}
	m.transcriptTotalEntries = 1
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})

	_ = m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{Window: clientui.TranscriptWindowOngoingTail}, clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
	}, clientui.TranscriptRecoveryCauseNone)

	joined := strings.Join(logger.lines, "\n")
	if !strings.Contains(joined, "transcript.diag.client.apply_page_reject") {
		t.Fatalf("expected reject diagnostics, got %q", joined)
	}
	if !strings.Contains(joined, "reason=live_dirty_same_or_older_revision") {
		t.Fatalf("expected reject reason, got %q", joined)
	}
}

func TestDeferredCommittedTailLifecycleLogsDiagnostics(t *testing.T) {
	logger := &testUILogger{}
	m := newProjectedStaticUIModel(
		WithUILogger(logger),
		WithUITranscriptDiagnostics(true),
		WithUISessionID("session-1"),
	)
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.setBusy(true)
	m.pendingInjected = queuedUserMessagesForTest("steered message")
	m.input = "steered message"
	m.lockedInjectText = "steered message"
	m.lockedInjectID = "queue-test-0"
	m.setInputSubmitLocked(true)
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "user", Text: "seed"}}
	m.transcriptRevision = 6
	m.transcriptTotalEntries = 1
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})
	_ = m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventAssistantDelta, StepID: "step-1", AssistantDelta: "foreground done"}, true).cmd

	_ = m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                         clientui.EventUserMessageFlushed,
		StepID:                       "step-1",
		CommittedTranscriptChanged:   true,
		TranscriptRevision:           7,
		CommittedEntryCount:          2,
		UserMessage:                  "steered message",
		UserMessageBatchQueueItemIDs: []string{"queue-test-0"},
		TranscriptEntries:            []clientui.ChatEntry{{Role: "user", Text: "steered message"}},
	}, true).cmd
	_ = m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         8,
		CommittedEntryCount:        3,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "assistant", Text: "foreground done"}},
	}, true).cmd

	m.deferredCommittedTail = []deferredProjectedTranscriptTail{{
		rangeStart: 1,
		rangeEnd:   2,
		revision:   8,
		entries:    []clientui.ChatEntry{{Role: "user", Text: "queued user"}},
		pending:    []string{"queued user"},
	}}
	_ = m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		StepID:                     "step-2",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         9,
		CommittedEntryCount:        5,
		CommittedEntryStart:        4,
		CommittedEntryStartSet:     true,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "assistant", Text: "authoritative tail"}},
	}, true).cmd

	joined := strings.Join(logger.lines, "\n")
	for _, want := range []string{
		"transcript.diag.client.defer_tail",
		"transcript.diag.client.merge_deferred_tail",
		"transcript.diag.client.begin_continuity_recovery",
		"transcript.diag.client.clear_deferred_tail",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %s in diagnostics, got %q", want, joined)
		}
	}
	if !strings.Contains(joined, "reason=invalidate_transient") {
		t.Fatalf("expected clear reason in diagnostics, got %q", joined)
	}
}

func TestRuntimeCarryQueueLogsAndResumesInOrder(t *testing.T) {
	logger := &testUILogger{}
	m := newProjectedStaticUIModel(
		WithUILogger(logger),
		WithUITranscriptDiagnostics(true),
		WithUISessionID("session-1"),
	)
	m.waitRuntimeEventAfterHydration = true
	m.pendingRuntimeEvents = []clientui.Event{{Kind: clientui.EventAssistantDelta, AssistantDelta: "older"}}
	carry := clientui.Event{Kind: clientui.EventLocalEntryAdded, CommittedTranscriptChanged: true, CommittedEntryStart: 1, CommittedEntryStartSet: true, CommittedEntryCount: 2, TranscriptEntries: []clientui.ChatEntry{{Role: "reviewer_status", Text: "Supervisor ran: no changes."}}}

	next, _ := m.Update(runtimeEventBatchMsg{
		events: []clientui.Event{{Kind: clientui.EventConversationUpdated}},
		carry:  &carry,
	})
	updated := next.(*uiModel)
	if got := len(updated.pendingRuntimeEvents); got != 2 {
		t.Fatalf("expected carry prepended to pending runtime events, got %d", got)
	}
	if updated.pendingRuntimeEvents[0].Kind != clientui.EventLocalEntryAdded {
		t.Fatalf("expected carry first in pending runtime events, got %+v", updated.pendingRuntimeEvents)
	}
	updated.waitRuntimeEventAfterHydration = false

	resumeCmd := updated.waitRuntimeEventCmd()
	if resumeCmd == nil {
		t.Fatal("expected pending runtime event resume command")
	}
	batch, ok := resumeCmd().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected runtimeEventBatchMsg, got %T", resumeCmd())
	}
	if len(batch.events) != 1 || batch.events[0].Kind != clientui.EventLocalEntryAdded {
		t.Fatalf("expected carry resumed before older pending event, got %+v", batch.events)
	}

	joined := strings.Join(logger.lines, "\n")
	if !strings.Contains(joined, "transcript.diag.client.runtime_batch_carry") {
		t.Fatalf("expected carry diagnostics, got %q", joined)
	}
	if !strings.Contains(joined, "transcript.diag.client.wait_runtime_event_resume_pending") {
		t.Fatalf("expected resume diagnostics, got %q", joined)
	}
	if !strings.Contains(joined, "kind=local_entry_added") {
		t.Fatalf("expected resumed carry kind in diagnostics, got %q", joined)
	}
}

func TestRuntimeClientTranscriptDiagnosticsEnablePaths(t *testing.T) {
	t.Setenv("BUILDER_TRANSCRIPT_DIAGNOSTICS", "")
	client := newUIRuntimeClientWithReads("session-1", transcriptDiagTestSessionViewClient{}, transcriptDiagTestRuntimeControlClient{}).(*sessionRuntimeClient)
	if client.transcriptDiagnosticsEnabled() {
		t.Fatal("expected transcript diagnostics disabled by default")
	}
	client.SetTranscriptDiagnosticsEnabled(true)
	if !client.transcriptDiagnosticsEnabled() {
		t.Fatal("expected explicit runtime-client diagnostics enable to take effect")
	}
	client.SetTranscriptDiagnosticsEnabled(false)
	if client.transcriptDiagnosticsEnabled() {
		t.Fatal("expected runtime-client diagnostics disable to take effect when env is unset")
	}
	t.Setenv("BUILDER_TRANSCRIPT_DIAGNOSTICS", "1")
	if !client.transcriptDiagnosticsEnabled() {
		t.Fatal("expected transcript diagnostics env to enable runtime-client diagnostics")
	}
}
