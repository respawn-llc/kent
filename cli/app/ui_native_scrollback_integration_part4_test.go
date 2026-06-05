package app

import (
	"builder/cli/tui"
	"builder/server/llm"
	"builder/server/runtime"
	"builder/shared/clientui"
	"builder/shared/serverapi"
	"builder/shared/transcript"
	"bytes"
	"context"
	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"io"
	"strings"
	"testing"
	"time"
)

func TestNativeProgramUserFlushDoesNotTriggerTranscriptSyncThatDropsCommentary(t *testing.T) {
	out := &bytes.Buffer{}
	runtimeEvents := make(chan clientui.Event, 16)
	client := &staleTranscriptRuntimeClient{
		page: clientui.TranscriptPage{
			SessionID: "session-1",
			Entries:   []clientui.ChatEntry{{Role: "assistant", Text: "seed", Phase: string(llm.MessagePhaseFinal)}},
		},
	}
	model := newProjectedTestUIModel(
		client,
		runtimeEvents,
		closedAskEvents(),
	)
	program := startNativeProgram(t, model, out)

	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	waitForTestCondition(t, 2*time.Second, "startup replay", func() bool {
		return strings.Contains(normalizedOutput(out.String()), "seed")
	})
	baselineLoadCalls := client.LoadCalls()

	callMeta := transcript.ToolCallMeta{ToolName: "shell", Command: "pwd", CompactText: "pwd", IsShell: true}
	runtimeEvents <- clientui.Event{
		Kind:                       clientui.EventUserMessageFlushed,
		CommittedTranscriptChanged: true,
		StepID:                     "step-1",
		UserMessage:                "say hi",
		TranscriptEntries:          []clientui.ChatEntry{{Role: "user", Text: "say hi"}},
	}
	runtimeEvents <- clientui.Event{Kind: clientui.EventAssistantDelta, StepID: "step-1", AssistantDelta: "working"}

	waitForTestCondition(t, 2*time.Second, "live commentary after user flush", func() bool {
		normalized := normalizedOutput(out.String())
		return containsInOrder(normalized, "seed", "say hi", "working")
	})
	if committed := stripANSIAndTrimRight(model.view.OngoingCommittedSnapshot()); !strings.Contains(committed, "say hi") {
		t.Fatalf("expected committed ongoing surface to retain flushed user row before later runtime events, got %q", committed)
	}
	if currentLoadCalls := client.LoadCalls(); currentLoadCalls != baselineLoadCalls {
		t.Fatalf("expected flushed user message to avoid extra transcript syncs before commentary, baseline=%d current=%d", baselineLoadCalls, currentLoadCalls)
	}

	runtimeEvents <- clientui.Event{Kind: clientui.EventToolCallStarted, StepID: "step-1", TranscriptEntries: []clientui.ChatEntry{{Role: "tool_call", Text: "pwd", ToolCallID: "call_1", ToolCall: transcriptToolCallMetaClient(&callMeta)}}}
	runtimeEvents <- clientui.Event{Kind: clientui.EventToolCallCompleted, StepID: "step-1", TranscriptEntries: []clientui.ChatEntry{{Role: "tool_result_ok", Text: "$ pwd\n/tmp", ToolCallID: "call_1"}}}
	runtimeEvents <- clientui.Event{Kind: clientui.EventAssistantMessage, StepID: "step-1", TranscriptEntries: []clientui.ChatEntry{{Role: "assistant", Text: "done", Phase: string(llm.MessagePhaseFinal)}}}

	waitForTestCondition(t, 2*time.Second, "tool and final after user flush", func() bool {
		normalized := normalizedOutput(out.String())
		return containsInOrder(normalized, "seed", "say hi", "pwd", "done")
	})
	if currentLoadCalls := client.LoadCalls(); currentLoadCalls != baselineLoadCalls {
		t.Fatalf("expected flushed user message to avoid extra transcript syncs, baseline=%d current=%d", baselineLoadCalls, currentLoadCalls)
	}

	program.QuitAndWait(2 * time.Second)
	if normalized := normalizedOutput(out.String()); !containsInOrder(normalized, "seed", "say hi", "pwd", "done") {
		t.Fatalf("expected realtime terminal sequence after user flush, got %q", normalized)
	}
}

func TestNativeProgramCommittedToolStartReplacesMatchingTransientToolRowWithoutHydration(t *testing.T) {
	out := &bytes.Buffer{}
	runtimeEvents := make(chan clientui.Event, 16)
	client := &staleTranscriptRuntimeClient{
		page: clientui.TranscriptPage{
			SessionID: "session-1",
			Entries:   []clientui.ChatEntry{{Role: "assistant", Text: "seed", Phase: string(llm.MessagePhaseFinal)}},
		},
	}
	model := newProjectedTestUIModel(client, runtimeEvents, closedAskEvents())
	program := startNativeProgram(t, model, out)

	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	waitForTestCondition(t, 2*time.Second, "startup replay", func() bool {
		return strings.Contains(normalizedOutput(out.String()), "seed")
	})
	baselineLoadCalls := client.LoadCalls()

	callMeta := transcript.ToolCallMeta{ToolName: "shell", Command: "pwd", CompactText: "pwd", IsShell: true}
	runtimeEvents <- clientui.Event{
		Kind:               clientui.EventToolCallStarted,
		StepID:             "step-1",
		TranscriptRevision: 2,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:       "tool_call",
			Text:       "pwd",
			ToolCallID: "call_1",
			ToolCall:   transcriptToolCallMetaClient(&callMeta),
		}},
	}
	waitForTestCondition(t, 2*time.Second, "transient tool row buffered locally", func() bool {
		return len(model.transcriptEntries) == 2 && model.transcriptEntries[1].Transient
	})
	if strings.Contains(stripANSIAndTrimRight(model.view.OngoingCommittedSnapshot()), "pwd") {
		t.Fatalf("expected transient tool row to stay out of committed ongoing surface, got %q", stripANSIAndTrimRight(model.view.OngoingCommittedSnapshot()))
	}

	runtimeEvents <- clientui.Event{
		Kind:                       clientui.EventToolCallStarted,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         2,
		CommittedEntryCount:        2,
		CommittedEntryStart:        1,
		CommittedEntryStartSet:     true,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:       "tool_call",
			Text:       "pwd",
			ToolCallID: "call_1",
			ToolCall:   transcriptToolCallMetaClient(&callMeta),
		}},
	}
	waitForTestCondition(t, 2*time.Second, "committed tool start upgrades transient row in transcript", func() bool {
		return len(model.transcriptEntries) == 2 && model.transcriptEntries[1].Committed && !model.transcriptEntries[1].Transient
	})
	runtimeEvents <- clientui.Event{
		Kind:                       clientui.EventToolCallCompleted,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         3,
		CommittedEntryCount:        3,
		CommittedEntryStart:        2,
		CommittedEntryStartSet:     true,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:       "tool_result_ok",
			Text:       "$ pwd\n/tmp",
			ToolCallID: "call_1",
		}},
	}

	waitForTestCondition(t, 2*time.Second, "committed tool pair visible without hydrate", func() bool {
		committed := stripANSIAndTrimRight(model.view.OngoingCommittedSnapshot())
		return strings.Contains(committed, "pwd")
	})
	if currentLoadCalls := client.LoadCalls(); currentLoadCalls != baselineLoadCalls {
		t.Fatalf("expected committed tool start to avoid transcript hydrate when only replacing a matching transient row, baseline=%d current=%d", baselineLoadCalls, currentLoadCalls)
	}

	program.QuitAndWait(2 * time.Second)
	if committed := stripANSIAndTrimRight(model.view.OngoingCommittedSnapshot()); !strings.Contains(committed, "pwd") {
		t.Fatalf("expected committed tool pair in ongoing committed surface after replacement, got %q", committed)
	}
}

func TestNativeProgramUserFlushHydratesCommittedGapWhileAssistantStreamIsLive(t *testing.T) {
	out := &bytes.Buffer{}
	runtimeEvents := make(chan clientui.Event, 16)
	client := &staleTranscriptRuntimeClient{
		runtimeControlFakeClient: runtimeControlFakeClient{
			transcript: clientui.TranscriptPage{
				SessionID:    "session-1",
				Revision:     1,
				Entries:      []clientui.ChatEntry{{Role: "user", Text: "seed"}},
				TotalEntries: 1,
			},
		},
		page: clientui.TranscriptPage{
			SessionID:    "session-1",
			Revision:     1,
			Entries:      []clientui.ChatEntry{{Role: "user", Text: "seed"}},
			TotalEntries: 1,
		},
	}
	model := newProjectedTestUIModel(client, runtimeEvents, closedAskEvents())
	program := startNativeProgram(t, model, out)

	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	waitForTestCondition(t, 2*time.Second, "startup replay", func() bool {
		return strings.Contains(normalizedOutput(out.String()), "seed")
	})
	baselineLoadCalls := client.LoadCalls()

	runtimeEvents <- clientui.Event{Kind: clientui.EventRunStateChanged, RunState: &clientui.RunState{Lifecycle: clientui.RunningRunLifecycle(clientui.RunModeTurn)}}
	runtimeEvents <- clientui.Event{Kind: clientui.EventAssistantDelta, StepID: "step-1", AssistantDelta: "foreground done"}
	waitForTestCondition(t, 2*time.Second, "assistant delta visible", func() bool {
		return strings.Contains(normalizedOutput(out.String()), "foreground done")
	})
	client.page = clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     2,
		Entries:      []clientui.ChatEntry{{Role: "user", Text: "seed"}, {Role: "user", Text: "steered message"}},
		TotalEntries: 2,
	}

	runtimeEvents <- clientui.Event{
		Kind:                       clientui.EventUserMessageFlushed,
		CommittedTranscriptChanged: true,
		StepID:                     "step-1",
		TranscriptRevision:         2,
		CommittedEntryCount:        2,
		UserMessage:                "steered message",
		TranscriptEntries:          []clientui.ChatEntry{{Role: "user", Text: "steered message"}},
	}
	userFlushDeadline := time.Now().Add(2 * time.Second)
	userFlushVisible := false
	for time.Now().Before(userFlushDeadline) {
		committed := stripANSIAndTrimRight(model.view.OngoingCommittedSnapshot())
		if containsInOrder(committed, "seed", "steered message") {
			userFlushVisible = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !userFlushVisible {
		t.Fatalf("expected user flush visible after hydrate, committed=%q load_calls=%d output=%q", stripANSIAndTrimRight(model.view.OngoingCommittedSnapshot()), client.LoadCalls(), normalizedOutput(out.String()))
	}
	if got := len(model.deferredCommittedTail); got != 0 {
		t.Fatalf("expected queued user flush path to avoid deferred committed tail, got %d", got)
	}
	if currentLoadCalls := client.LoadCalls(); currentLoadCalls != baselineLoadCalls {
		t.Fatalf("expected contiguous user flush append to avoid transcript hydrate, baseline=%d current=%d", baselineLoadCalls, currentLoadCalls)
	}

	runtimeEvents <- clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		CommittedTranscriptChanged: true,
		StepID:                     "step-1",
		TranscriptRevision:         3,
		CommittedEntryCount:        3,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:  "assistant",
			Text:  "after follow-up",
			Phase: string(llm.MessagePhaseFinal),
		}},
	}

	waitForTestCondition(t, 2*time.Second, "assistant response appends after hydrated user row", func() bool {
		committed := stripANSIAndTrimRight(model.view.OngoingCommittedSnapshot())
		return containsInOrder(committed, "seed", "steered message", "after follow-up")
	})
	if currentLoadCalls := client.LoadCalls(); currentLoadCalls != baselineLoadCalls {
		t.Fatalf("expected assistant response append to keep avoiding transcript hydrate, baseline=%d current=%d", baselineLoadCalls, currentLoadCalls)
	}

	program.QuitAndWait(2 * time.Second)
	if committed := stripANSIAndTrimRight(model.view.OngoingCommittedSnapshot()); !containsInOrder(committed, "seed", "steered message", "after follow-up") {
		t.Fatalf("expected hydrated committed user flush to remain visible in ongoing committed surface, got %q", committed)
	}
}

func TestNativeProgramReviewerTerminalMessageRemainsVisibleWithoutHydration(t *testing.T) {
	out := &bytes.Buffer{}
	runtimeEvents := make(chan clientui.Event, 16)
	client := &staleTranscriptRuntimeClient{
		page: clientui.TranscriptPage{
			SessionID: "session-1",
			Entries:   []clientui.ChatEntry{{Role: "assistant", Text: "seed", Phase: string(llm.MessagePhaseFinal)}},
		},
	}
	model := newProjectedTestUIModel(client, runtimeEvents, closedAskEvents())
	program := startNativeProgram(t, model, out)

	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	waitForTestCondition(t, 2*time.Second, "startup replay", func() bool {
		return strings.Contains(normalizedOutput(out.String()), "seed")
	})
	baselineLoadCalls := client.LoadCalls()

	runtimeEvents <- clientui.Event{
		Kind:                       clientui.EventLocalEntryAdded,
		CommittedTranscriptChanged: true,
		StepID:                     "step-1",
		TranscriptRevision:         2,
		CommittedEntryCount:        2,
		CommittedEntryStart:        1,
		CommittedEntryStartSet:     true,
		TranscriptEntries: []clientui.ChatEntry{{
			Role: "reviewer_status",
			Text: "Supervisor ran: no changes.",
		}},
	}

	waitForTestCondition(t, 2*time.Second, "reviewer terminal message visible without hydrate", func() bool {
		committed := stripANSIAndTrimRight(model.view.OngoingCommittedSnapshot())
		return containsInOrder(committed, "seed", "Supervisor ran: no changes.")
	})
	if currentLoadCalls := client.LoadCalls(); currentLoadCalls != baselineLoadCalls {
		t.Fatalf("expected reviewer terminal message to avoid transcript hydrate when rich committed entries already cover the tail, baseline=%d current=%d", baselineLoadCalls, currentLoadCalls)
	}

	program.QuitAndWait(2 * time.Second)
	if committed := stripANSIAndTrimRight(model.view.OngoingCommittedSnapshot()); !containsInOrder(committed, "seed", "Supervisor ran: no changes.") {
		t.Fatalf("expected reviewer terminal message in ongoing committed surface, got %q", committed)
	}
}

func TestNativeProgramConversationRefreshHydratesCommittedTranscriptWithoutReplayDuplication(t *testing.T) {
	out := &bytes.Buffer{}
	runtimeEvents := make(chan clientui.Event, 8)
	client := &startupTranscriptRuntimeClient{
		view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{SessionID: "session-1", SessionName: "incident triage"}},
		page: clientui.TranscriptPage{
			SessionID:    "session-1",
			SessionName:  "incident triage",
			TotalEntries: 1,
			Entries: []clientui.ChatEntry{{
				Role:  "assistant",
				Text:  "already visible",
				Phase: string(llm.MessagePhaseFinal),
			}},
		},
	}
	model := newProjectedTestUIModel(client, runtimeEvents, closedAskEvents())
	model.startupCmds = nil
	program := startNativeProgram(t, model, out)

	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	waitForTestCondition(t, 2*time.Second, "startup committed transcript", func() bool {
		return strings.Contains(normalizedOutput(out.String()), "already visible")
	})
	baselineLen := out.Len()
	client.page = clientui.TranscriptPage{
		SessionID:    "session-1",
		SessionName:  "incident triage",
		TotalEntries: 2,
		Entries: []clientui.ChatEntry{
			{Role: "assistant", Text: "already visible", Phase: string(llm.MessagePhaseFinal)},
			{Role: "assistant", Text: "restored after reconnect", Phase: string(llm.MessagePhaseFinal)},
		},
	}

	runtimeEvents <- clientui.Event{Kind: clientui.EventConversationUpdated, CommittedTranscriptChanged: true}
	waitForTestCondition(t, 2*time.Second, "recovered committed transcript", func() bool {
		return strings.Contains(normalizedOutput(out.String()), "restored after reconnect")
	})

	program.QuitAndWait(2 * time.Second)

	raw := out.String()
	if strings.Contains(raw[baselineLen:], "\x1b[2J") {
		t.Fatalf("expected reconnect hydration to avoid clearing the session, got %q", raw[baselineLen:])
	}
	normalized := normalizedOutput(raw)
	if strings.Count(normalized, "already visible") != 1 {
		t.Fatalf("expected previously visible committed entry exactly once after hydration, got %d in %q", strings.Count(normalized, "already visible"), normalized)
	}
	if strings.Count(normalized, "restored after reconnect") != 1 {
		t.Fatalf("expected recovered committed entry exactly once, got %d in %q", strings.Count(normalized, "restored after reconnect"), normalized)
	}
	if model.sessionName != "incident triage" {
		t.Fatalf("expected session name preserved across hydration, got %q", model.sessionName)
	}
	if len(client.loadRequests) != 1 {
		t.Fatalf("transcript load calls = %d, want 1", len(client.loadRequests))
	}
}

func TestNativeProgramSlashLocalEntryDoesNotReplayPreviousPrompt(t *testing.T) {
	out := &bytes.Buffer{}
	runtimeEvents := make(chan clientui.Event, 8)
	client := &slashLocalEntryRuntimeClient{
		events: runtimeEvents,
		startupTranscriptRuntimeClient: startupTranscriptRuntimeClient{
			view: clientui.RuntimeMainView{
				Session: clientui.RuntimeSessionView{SessionID: "session-1"},
				Status:  clientui.RuntimeStatus{AutoCompactionEnabled: true},
			},
			page: clientui.TranscriptPage{
				SessionID:    "session-1",
				Revision:     1,
				TotalEntries: 1,
				NextOffset:   1,
				Entries: []clientui.ChatEntry{{
					Role: "user",
					Text: "can u backfill this into the conversation so i can post this on twitter",
				}},
			},
		},
	}
	model := newProjectedTestUIModel(client, runtimeEvents, closedAskEvents())
	model.promptHistoryDraft = "can u backfill this into the conversation so i can post this on twitter"
	model.promptHistoryDraftCursor = -1
	program := startNativeProgram(t, model, out)

	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	waitForTestCondition(t, 2*time.Second, "startup prompt visible once", func() bool {
		return strings.Count(normalizedOutput(out.String()), "can u backfill this into the conversation") == 1
	})
	model.input = "/autocompaction"
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForTestCondition(t, 2*time.Second, "slash command feedback visible", func() bool {
		return strings.Contains(normalizedOutput(out.String()), "Auto-compaction disabled")
	})
	program.QuitAndWait(2 * time.Second)

	normalized := normalizedOutput(out.String())
	if got := strings.Count(normalized, "can u backfill this into the conversation"); got != 1 {
		t.Fatalf("expected previous prompt once after slash feedback, got %d in %q", got, normalized)
	}
	if got := strings.Count(normalized, "Auto-compaction disabled"); got != 1 {
		t.Fatalf("expected slash feedback once, got %d in %q", got, normalized)
	}
}

type slashLocalEntryRuntimeClient struct {
	startupTranscriptRuntimeClient
	events chan<- clientui.Event
}

func (c *slashLocalEntryRuntimeClient) SetAutoCompactionEnabled(enabled bool) (bool, bool, error) {
	c.view.Status.AutoCompactionEnabled = enabled
	return true, enabled, nil
}

func (c *slashLocalEntryRuntimeClient) CachedMainView() (clientui.RuntimeMainView, bool) {
	return c.MainView(), true
}

func (c *slashLocalEntryRuntimeClient) AppendLocalEntry(role, text string) error {
	return c.AppendLocalEntryWithNoticeID(role, text, "")
}

func (c *slashLocalEntryRuntimeClient) AppendLocalEntryWithNoticeID(role, text, noticeID string) error {
	entry := clientui.ChatEntry{Role: role, Text: text, NoticeID: noticeID}
	start := c.page.TotalEntries
	c.page.Entries = append(c.page.Entries, entry)
	c.page.TotalEntries++
	c.page.NextOffset = c.page.TotalEntries
	c.page.Revision++
	c.events <- clientui.Event{
		Kind:                       clientui.EventLocalEntryAdded,
		CommittedTranscriptChanged: true,
		TranscriptRevision:         c.page.Revision,
		CommittedEntryCount:        c.page.TotalEntries,
		CommittedEntryStart:        start,
		CommittedEntryStartSet:     true,
		TranscriptEntries:          []clientui.ChatEntry{entry},
	}
	return nil
}

func TestNativeStreamingInterleavedWithStatusRedrawStaysCoherent(t *testing.T) {
	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(
		nil,
		closedProjectedRuntimeEvents(),
		closedAskEvents(),
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "prompt once"}}),
	)
	program := startNativeProgram(t, model, out)
	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 32})
	program.Send(projectedRuntimeEventMsg(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "line1\n"}))
	program.Send(spinnerTickMsg{})
	program.Send(projectedRuntimeEventMsg(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "line2\n"}))
	program.Send(spinnerTickMsg{})
	time.Sleep(40 * time.Millisecond)
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	program.Wait(2 * time.Second)
	raw := out.String()
	plain := xansi.Strip(raw)
	if strings.Count(normalizedOutput(raw), "prompt once") != 1 {
		t.Fatalf("expected prompt once in output, got %d", strings.Count(normalizedOutput(raw), "prompt once"))
	}
	line1Count := strings.Count(normalizedOutput(raw), "line1")
	line2Count := strings.Count(normalizedOutput(raw), "line2")
	if line1Count < 1 || line2Count < 1 || line1Count > 2 || line2Count > 2 {
		t.Fatalf("expected bounded streamed line visibility under redraw pressure, got line1=%d line2=%d output=%q", line1Count, line2Count, normalizedOutput(raw))
	}
	normalized := normalizedOutput(raw)
	if strings.LastIndex(normalized, "line1") > strings.LastIndex(normalized, "line2") {
		t.Fatalf("expected final streamed line order preserved, got %q", normalized)
	}
	for _, line := range strings.Split(plain, "\n") {
		if strings.Count(line, statusStateCircleGlyph+statusLineSpinnerSeparator) > 1 {
			t.Fatalf("expected no duplicated status segment in a single rendered line, got %q", line)
		}
	}
}

func TestQueuedFollowUpWaitsForFinalTranscriptCatchUpBeforeNativeScrollbackAppend(t *testing.T) {
	out := &bytes.Buffer{}
	client := &gatedRefreshRuntimeClient{
		runtimeControlFakeClient: runtimeControlFakeClient{
			sessionView: clientui.RuntimeSessionView{SessionID: "session-1"},
		},
		page: clientui.TranscriptPage{
			SessionID:    "session-1",
			TotalEntries: 1,
			Entries: []clientui.ChatEntry{{
				Role:  "assistant",
				Text:  "final answer",
				Phase: string(llm.MessagePhaseFinal),
			}},
		},
		refreshStarted: make(chan struct{}),
		releaseRefresh: make(chan struct{}),
	}
	model := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	model.startupCmds = nil
	model.setBusy(true)
	model.activity = uiActivityRunning
	model.queued = queuedInputsForTest("follow up")
	model.sawAssistantDelta = true
	model.forwardToView(tui.SetConversationMsg{Ongoing: "working"})

	program := startNativeProgram(t, model, out)

	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	waitForTestCondition(t, 2*time.Second, "live assistant streaming visible", func() bool {
		return strings.Contains(normalizedOutput(out.String()), "working")
	})

	program.Send(submitDoneMsg{message: "ignored by runtime-backed flow"})
	select {
	case <-client.refreshStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for transcript catch-up refresh to start")
	}
	time.Sleep(80 * time.Millisecond)
	if client.submitText != "" {
		t.Fatalf("expected queued follow-up to wait for transcript catch-up, submit=%q", client.submitText)
	}

	close(client.releaseRefresh)
	waitForTestCondition(t, 2*time.Second, "final answer committed before queued follow-up starts", func() bool {
		normalized := normalizedOutput(out.String())
		return strings.Contains(normalized, "final answer") && client.submitText == "follow up"
	})
	if strings.TrimSpace(model.view.OngoingStreamingText()) != "" {
		t.Fatalf("expected live streaming buffer cleared after final catch-up, got %q", model.view.OngoingStreamingText())
	}

	program.Send(runtimeEventMsg{event: clientui.Event{
		Kind:                       clientui.EventUserMessageFlushed,
		CommittedTranscriptChanged: true,
		StepID:                     "step-2",
		UserMessage:                "follow up",
		TranscriptEntries: []clientui.ChatEntry{{
			Role: "user",
			Text: "follow up",
		}},
	}})
	waitForTestCondition(t, 2*time.Second, "queued follow-up appended after final answer", func() bool {
		return containsInOrder(normalizedOutput(out.String()), "final answer", "follow up")
	})

	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	program.Wait(2 * time.Second)

	normalized := normalizedOutput(out.String())
	if !containsInOrder(normalized, "final answer", "follow up") {
		t.Fatalf("expected final answer before queued follow-up in scrollback, got %q", normalized)
	}
	if strings.Count(normalized, "final answer") != 1 {
		t.Fatalf("expected final answer appended exactly once, got %d in %q", strings.Count(normalized, "final answer"), normalized)
	}
}

func TestQueuedFollowUpRemainsHiddenUntilFinalCatchUpThenAppendsOnceInRenderedOngoing(t *testing.T) {
	out := &bytes.Buffer{}
	client := &gatedRefreshRuntimeClient{
		runtimeControlFakeClient: runtimeControlFakeClient{
			sessionView: clientui.RuntimeSessionView{SessionID: "session-1"},
		},
		page: clientui.TranscriptPage{
			SessionID:    "session-1",
			TotalEntries: 1,
			Entries: []clientui.ChatEntry{{
				Role:  "assistant",
				Text:  "final answer",
				Phase: string(llm.MessagePhaseFinal),
			}},
		},
		refreshStarted: make(chan struct{}),
		releaseRefresh: make(chan struct{}),
	}
	model := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	model.startupCmds = nil
	model.setBusy(true)
	model.activity = uiActivityRunning
	model.queued = queuedInputsForTest("follow up")
	model.sawAssistantDelta = true
	model.forwardToView(tui.SetConversationMsg{Ongoing: "working"})

	program := startNativeProgram(t, model, out)

	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	waitForTestCondition(t, 2*time.Second, "initial live assistant visible", func() bool {
		return strings.Contains(normalizedOutput(out.String()), "working")
	})

	program.Send(submitDoneMsg{message: "ignored by runtime-backed flow"})
	select {
	case <-client.refreshStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for transcript catch-up refresh to start")
	}
	time.Sleep(80 * time.Millisecond)
	if strings.Contains(stripANSIAndTrimRight(model.view.OngoingCommittedSnapshot()), "follow up") {
		t.Fatalf("expected queued follow-up to stay out of committed ongoing transcript before final catch-up, got %q", stripANSIAndTrimRight(model.view.OngoingCommittedSnapshot()))
	}

	close(client.releaseRefresh)
	waitForTestCondition(t, 2*time.Second, "final answer visible before queued follow-up", func() bool {
		committed := stripANSIAndTrimRight(model.view.OngoingCommittedSnapshot())
		return strings.Contains(committed, "final answer") && !strings.Contains(committed, "follow up")
	})

	program.Send(runtimeEventMsg{event: clientui.Event{
		Kind:                       clientui.EventUserMessageFlushed,
		CommittedTranscriptChanged: true,
		StepID:                     "step-2",
		UserMessage:                "follow up",
		TranscriptEntries: []clientui.ChatEntry{{
			Role: "user",
			Text: "follow up",
		}},
	}})
	waitForTestCondition(t, 2*time.Second, "queued follow-up appended after final catch-up", func() bool {
		return containsInOrder(normalizedOutput(out.String()), "final answer", "follow up")
	})

	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	program.Wait(2 * time.Second)

	committed := stripANSIAndTrimRight(model.view.OngoingCommittedSnapshot())
	if strings.Count(committed, "final answer") != 1 {
		t.Fatalf("expected final answer exactly once in committed ongoing transcript, got %d in %q", strings.Count(committed, "final answer"), committed)
	}
	if strings.Count(committed, "follow up") != 1 {
		t.Fatalf("expected queued follow-up exactly once in committed ongoing transcript, got %d in %q", strings.Count(committed, "follow up"), committed)
	}
	if !containsInOrder(committed, "final answer", "follow up") {
		t.Fatalf("expected final answer before queued follow-up in rendered committed ongoing transcript, got %q", committed)
	}
}

func TestRuntimeContinuityRecoveryReplaysOngoingScrollbackAndLaterAssistantAppend(t *testing.T) {
	out := &bytes.Buffer{}
	runtimeEvents := make(chan clientui.Event, 16)
	client := &runtimeControlFakeClient{
		sessionView: clientui.RuntimeSessionView{SessionID: "session-1"},
		transcript: clientui.TranscriptPage{
			SessionID:    "session-1",
			Revision:     1,
			TotalEntries: 2,
			Entries: []clientui.ChatEntry{
				{Role: "user", Text: "commit/push"},
				{Role: "assistant", Text: "before"},
			},
		},
	}
	model := newProjectedTestUIModel(
		client,
		runtimeEvents,
		closedAskEvents(),
	)
	model.startupCmds = nil
	model.runtimeTranscriptBusy = true
	model.runtimeTranscriptToken = 1
	model.setBusy(true)
	model.activity = uiActivityRunning
	model.sawAssistantDelta = true
	model.forwardToView(tui.SetConversationMsg{Entries: model.transcriptEntries, Ongoing: "working"})

	program := startNativeProgram(t, model, out)

	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	waitForTestCondition(t, 2*time.Second, "initial ongoing output visible", func() bool {
		normalized := normalizedOutput(out.String())
		return strings.Contains(normalized, "before") && strings.Contains(normalized, "working")
	})

	program.Send(runtimeTranscriptRefreshedMsg{token: 1, recoveryCause: clientui.TranscriptRecoveryCauseStreamGap, transcript: clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     2,
		TotalEntries: 2,
		Entries: []clientui.ChatEntry{
			{Role: "user", Text: "commit/push"},
			{Role: "assistant", Text: "after"},
		},
	}})
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(normalizedOutput(out.String()), "after") {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for continuity recovery replay appended to ongoing scrollback output=%q transcript=%+v native_projection=%+v native_rendered_projection=%+v native_snapshot=%q busy=%t runtime_busy=%t token=%d ongoing=%q", normalizedOutput(out.String()), model.transcriptEntries, model.nativeProjection, model.nativeRenderedProjection, model.nativeRenderedSnapshot, model.isBusy(), model.runtimeTranscriptBusy, model.runtimeTranscriptToken, stripANSIAndTrimRight(model.view.OngoingSnapshot()))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := len(model.transcriptEntries); got != 2 {
		t.Fatalf("expected continuity recovery hydrate to replace transcript tail, got %d entries", got)
	}
	if got := model.transcriptEntries[1].Text; got != "after" {
		t.Fatalf("expected authoritative assistant tail after continuity recovery, got %q", got)
	}
	if strings.TrimSpace(model.view.OngoingStreamingText()) != "" {
		t.Fatalf("expected continuity recovery hydrate to clear stale streaming text, got %q", model.view.OngoingStreamingText())
	}

	program.Send(runtimeEventMsg{event: clientui.Event{
		Kind:                       clientui.EventConversationUpdated,
		StepID:                     "step-2",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         3,
		CommittedEntryCount:        3,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:  "assistant",
			Text:  "next answer",
			Phase: string(llm.MessagePhaseFinal),
		}},
	}})
	waitForTestCondition(t, 2*time.Second, "later assistant append resumes after continuity recovery", func() bool {
		return containsInOrder(normalizedOutput(out.String()), "after", "next answer")
	})

	program.QuitAndWait(2 * time.Second)

	normalized := normalizedOutput(out.String())
	if !containsInOrder(normalized, "before", "after", "next answer") {
		t.Fatalf("expected ongoing scrollback to show initial stale tail, recovered authoritative tail, then later assistant append, got %q", normalized)
	}
	if strings.Count(normalized, "next answer") != 1 {
		t.Fatalf("expected later assistant append exactly once, got %d in %q", strings.Count(normalized, "next answer"), normalized)
	}

	next, detailCmd := model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	model = next.(*uiModel)
	_ = collectCmdMessages(t, detailCmd)
	if model.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected detail mode after toggle, got %q", model.view.Mode())
	}
	detail := stripANSIAndTrimRight(model.View())
	if !strings.Contains(detail, "next answer") {
		t.Fatalf("expected detail mode to reflect authoritative transcript tail, got %q", detail)
	}
	foundAuthoritativeTail := false
	for _, entry := range model.transcriptEntries {
		if strings.Contains(entry.Text, "after") {
			foundAuthoritativeTail = true
			break
		}
	}
	if !foundAuthoritativeTail {
		t.Fatalf("expected detail transcript state to include authoritative tail, got %+v", model.transcriptEntries)
	}
	if strings.Contains(detail, "before") {
		t.Fatalf("expected detail mode to exclude stale assistant tail after continuity recovery, got %q", detail)
	}
}

func TestNativeOngoingScrollbackContinuesAfterTransientActivityResubscribeTimeout(t *testing.T) {
	useFastStreamResubscribeDelays(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initial := &stubSessionActivitySubscription{steps: []stubSessionActivityStep{
		{evt: clientui.Event{
			Sequence:                   1,
			Kind:                       clientui.EventAssistantMessage,
			CommittedTranscriptChanged: true,
			TranscriptRevision:         1,
			CommittedEntryCount:        1,
			CommittedEntryStartSet:     true,
			TranscriptEntries:          []clientui.ChatEntry{{Role: "assistant", Text: "before disconnect", Phase: string(llm.MessagePhaseFinal)}},
		}},
		{err: io.EOF},
	}}
	recovered := &stubSessionActivitySubscription{steps: []stubSessionActivityStep{{evt: clientui.Event{
		Sequence:                   2,
		Kind:                       clientui.EventAssistantMessage,
		CommittedTranscriptChanged: true,
		TranscriptRevision:         2,
		CommittedEntryCount:        2,
		CommittedEntryStart:        1,
		CommittedEntryStartSet:     true,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "assistant", Text: "after reconnect", Phase: string(llm.MessagePhaseFinal)}},
	}}}}
	subscribeCalls := 0
	runtimeEvents, stopRuntimeEvents := startSessionActivityEvents(ctx, initial, func(context.Context, uint64) (serverapi.SessionActivitySubscription, error) {
		subscribeCalls++
		if subscribeCalls == 1 {
			return nil, context.DeadlineExceeded
		}
		return recovered, nil
	}, func() bool { return false }, nil)
	defer stopRuntimeEvents()

	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(&runtimeControlFakeClient{sessionView: clientui.RuntimeSessionView{SessionID: "session-1"}}, runtimeEvents, closedAskEvents())
	program := startNativeProgram(t, model, out)

	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	waitForTestCondition(t, 2*time.Second, "initial activity emitted", func() bool {
		return strings.Contains(normalizedOutput(out.String()), "before disconnect")
	})
	waitForTestCondition(t, 2*time.Second, "activity after reconnect emitted", func() bool {
		return containsInOrder(normalizedOutput(out.String()), "before disconnect", "after reconnect")
	})

	program.QuitAndWait(2 * time.Second)
	if subscribeCalls != 2 {
		t.Fatalf("subscribe calls = %d, want 2", subscribeCalls)
	}
	if got := strings.Count(normalizedOutput(out.String()), "after reconnect"); got != 1 {
		t.Fatalf("after reconnect emitted %d times in %q", got, normalizedOutput(out.String()))
	}
}

func TestRuntimeAuthoritativeHydrateRepairsOngoingScrollbackWithoutContinuityLoss(t *testing.T) {
	out := &bytes.Buffer{}
	runtimeEvents := make(chan clientui.Event, 16)
	client := &runtimeControlFakeClient{
		sessionView: clientui.RuntimeSessionView{SessionID: "session-1"},
		transcript: clientui.TranscriptPage{
			SessionID:    "session-1",
			Revision:     1,
			TotalEntries: 2,
			Entries: []clientui.ChatEntry{
				{Role: "user", Text: "commit/push"},
				{Role: "assistant", Text: "before"},
			},
		},
	}
	model := newProjectedTestUIModel(client, runtimeEvents, closedAskEvents())
	model.startupCmds = nil
	model.runtimeTranscriptBusy = true
	model.runtimeTranscriptToken = 1
	model.setBusy(true)
	model.activity = uiActivityRunning
	model.sawAssistantDelta = true
	model.forwardToView(tui.SetConversationMsg{Entries: model.transcriptEntries, Ongoing: "working"})

	program := startNativeProgram(t, model, out)

	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	waitForTestCondition(t, 2*time.Second, "initial ongoing output visible", func() bool {
		normalized := normalizedOutput(out.String())
		return strings.Contains(normalized, "before") && strings.Contains(normalized, "working")
	})

	program.Send(runtimeTranscriptRefreshedMsg{token: 1, transcript: clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     2,
		TotalEntries: 2,
		Entries: []clientui.ChatEntry{
			{Role: "user", Text: "commit/push"},
			{Role: "assistant", Text: "after"},
		},
	}})
	baselineLen := out.Len()
	waitForTestCondition(t, 2*time.Second, "authoritative hydrate divergence rebased", func() bool {
		got := stripANSIAndTrimRight(model.nativeRenderedSnapshot)
		return strings.Contains(got, "after") && !strings.Contains(got, "before")
	})

	program.QuitAndWait(2 * time.Second)

	if strings.Contains(out.String()[baselineLen:], "\x1b[2J") {
		t.Fatalf("did not expect ordinary authoritative hydrate divergence to clear/replay ongoing scrollback, got %q", out.String()[baselineLen:])
	}
	if got := stripANSIAndTrimRight(model.nativeRenderedSnapshot); !strings.Contains(got, "after") || strings.Contains(got, "before") {
		t.Fatalf("expected authoritative hydrate divergence to rebase internal rendered snapshot, got %q", got)
	}
	if normalized := normalizedOutput(out.String()); !strings.Contains(normalized, "before") {
		t.Fatalf("expected previously emitted ongoing scrollback to remain intact, got %q", normalized)
	}
}
