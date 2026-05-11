package app

import (
	"builder/cli/tui"
	"builder/server/llm"
	"builder/server/runtime"
	"builder/server/tools"
	"builder/shared/clientui"
	"builder/shared/toolspec"
	"builder/shared/transcript"
	"builder/shared/transcript/toolcodec"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestNativePendingToolEntriesTrackParallelCommitFrontier(t *testing.T) {
	entries := []tui.TranscriptEntry{
		{Role: "user", Text: "prompt"},
		{Role: "tool_call", Text: "echo a", ToolCallID: "call_a", ToolCall: &transcript.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo a"}},
		{Role: "tool_call", Text: "echo b", ToolCallID: "call_b", ToolCall: &transcript.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo b"}},
		{Role: "tool_result_ok", Text: "out-b", ToolCallID: "call_b"},
	}

	pending := nativePendingToolEntries(entries)
	if len(pending) != 3 {
		t.Fatalf("expected pending tool calls plus matching completed result, got %#v", pending)
	}
	if pending[0].ToolCallID != "call_a" || pending[0].Role != "tool_call" || pending[0].Text != "echo a" {
		t.Fatalf("unexpected first pending tool entry: %#v", pending[0])
	}
	if pending[1].ToolCallID != "call_b" || pending[1].Role != "tool_call" || pending[1].Text != "echo b" {
		t.Fatalf("unexpected second pending tool entry: %#v", pending[1])
	}
	if pending[2].ToolCallID != "call_b" || pending[2].Role != "tool_result_ok" || pending[2].Text != "out-b" {
		t.Fatalf("unexpected pending tool result entry: %#v", pending[2])
	}
}

func TestNativeScrollbackWarningStaysHiddenFromOngoingReplayAndShowsInDetail(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUITheme("dark"),
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "warning", Text: "Heads-up warning text."}}),
	)

	_, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if cmd == nil {
		t.Fatal("expected startup replay command")
	}
	msg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", cmd())
	}
	plain := stripANSIPreserve(msg.Text)
	if strings.Contains(plain, "Heads-up warning text.") {
		t.Fatalf("did not expect warning in ongoing native replay, got %q", plain)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	detail := stripANSIPreserve(next.(*uiModel).View())
	if !strings.Contains(detail, "Heads-up warning text.") {
		t.Fatalf("expected warning in detail view, got %q", detail)
	}
}

func TestNativePendingToolCallStaysLiveUntilResultThenAppendsFinalBlock(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "prompt once"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}

	call := tui.TranscriptEntry{
		Role:       "tool_call",
		Text:       "pwd",
		ToolCallID: "call_1",
		ToolCall:   &transcript.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "pwd"},
	}
	m.transcriptEntries = append(m.transcriptEntries, call)
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})
	m.syncViewport()
	if cmd := m.syncNativeHistoryFromTranscript(); cmd != nil {
		t.Fatalf("expected pending tool call to stay out of committed scrollback, got %T", cmd())
	}
	if strings.Contains(m.nativeRenderedSnapshot, "pwd") {
		t.Fatalf("expected pending tool call absent from committed snapshot, got %q", m.nativeRenderedSnapshot)
	}

	result := tui.TranscriptEntry{Role: "tool_result_ok", Text: "/tmp", ToolCallID: "call_1"}
	m.transcriptEntries = append(m.transcriptEntries, result)
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})
	m.syncViewport()
	cmd := m.syncNativeHistoryFromTranscript()
	if cmd == nil {
		t.Fatal("expected finalized tool block to append to native scrollback")
	}
	msg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", cmd())
	}
	plain := stripANSIText(msg.Text)
	if strings.Contains(plain, "prompt once") {
		t.Fatalf("expected tool completion delta without full replay, got %q", msg.Text)
	}
	if strings.Count(plain, "pwd") != 1 {
		t.Fatalf("expected finalized tool call emitted once, got %q", msg.Text)
	}
	if strings.Contains(plain, "/tmp") {
		t.Fatalf("did not expect native ongoing scrollback to start emitting shell output inline, got %q", msg.Text)
	}
	if cmd := m.syncNativeHistoryFromTranscript(); cmd != nil {
		t.Fatalf("expected no duplicate emission after finalized tool call flush, got %T", cmd())
	}
}

func TestNativeParallelToolCompletionWaitsForStablePrefixBeforeAppend(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "prompt once"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}

	entries := []tui.TranscriptEntry{
		{Role: "user", Text: "prompt once"},
		{Role: "tool_call", Text: "echo a", ToolCallID: "call_a", ToolCall: &transcript.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo a"}},
		{Role: "tool_call", Text: "echo b", ToolCallID: "call_b", ToolCall: &transcript.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo b"}},
		{Role: "tool_result_ok", Text: "out-b", ToolCallID: "call_b"},
	}
	m.transcriptEntries = entries
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})
	m.syncViewport()
	if cmd := m.syncNativeHistoryFromTranscript(); cmd != nil {
		t.Fatalf("expected no committed flush before first pending call resolves, got %T", cmd())
	}

	m.transcriptEntries = append(m.transcriptEntries, tui.TranscriptEntry{Role: "tool_result_ok", Text: "out-a", ToolCallID: "call_a"})
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})
	m.syncViewport()
	cmd := m.syncNativeHistoryFromTranscript()
	if cmd == nil {
		t.Fatal("expected append once the stable prefix advances")
	}
	msg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", cmd())
	}
	plain := stripANSIText(msg.Text)
	if strings.Contains(plain, "prompt once") {
		t.Fatalf("expected delta append without prompt replay, got %q", msg.Text)
	}
	if strings.Count(plain, "echo a") != 1 || strings.Count(plain, "echo b") != 1 {
		t.Fatalf("expected both tool calls appended exactly once in order, got %q", msg.Text)
	}
	if strings.Index(plain, "echo a") > strings.Index(plain, "echo b") {
		t.Fatalf("expected parallel tool append to preserve declaration order, got %q", plain)
	}
	if strings.Contains(plain, "out-a") || strings.Contains(plain, "out-b") {
		t.Fatalf("did not expect shell outputs inline in ongoing scrollback delta, got %q", msg.Text)
	}
	postCommitView := stripANSIPreserve(m.View())
	if strings.Contains(postCommitView, "echo a") || strings.Contains(postCommitView, "echo b") {
		t.Fatalf("expected committed tool rows removed from volatile live region, got %q", postCommitView)
	}
	if cmd := m.syncNativeHistoryFromTranscript(); cmd != nil {
		t.Fatalf("expected no duplicate append after committing stable prefix, got %T", cmd())
	}
}

func TestProjectedRuntimeBatchesPreserveImmediateLiveEventsAndLaterCommittedAppend(t *testing.T) {
	m := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), nil,
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "seed"}}),
	)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	m = next.(*uiModel)
	_ = collectCmdMessages(t, startupCmd)

	callMeta := transcript.ToolCallMeta{ToolName: "shell", Command: "pwd", CompactText: "pwd", IsShell: true}
	firstBatch := []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventUserMessageFlushed, StepID: "step-1", UserMessage: "say hi"}),
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventLocalEntryAdded, StepID: "step-1", CommittedTranscriptChanged: true, CommittedEntryStart: 2, CommittedEntryStartSet: true, CommittedEntryCount: 3, LocalEntry: &runtime.ChatEntry{Role: "reviewer_status", Text: "Supervisor ran: 2 suggestions, applied."}}),
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventReviewerCompleted, StepID: "step-1", Reviewer: &runtime.ReviewerStatus{Outcome: "applied", SuggestionsCount: 2}}),
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventBackgroundUpdated, StepID: "step-1", Background: &runtime.BackgroundShellEvent{Type: "completed", ID: "1000", State: "completed", NoticeText: "Background shell 1000 completed.\nOutput:\nhello", CompactText: "Background shell 1000 completed"}}),
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventToolCallStarted, StepID: "step-1", ToolCall: &llm.ToolCall{ID: "call-1", Name: string(toolspec.ToolExecCommand), Presentation: toolcodec.EncodeToolCallMeta(callMeta)}}),
	}
	next, cmd := m.Update(runtimeEventBatchMsg{events: firstBatch})
	m = next.(*uiModel)
	msgs := collectCmdMessages(t, cmd)
	flushText := strings.Builder{}
	for _, msg := range msgs {
		if flush, ok := msg.(nativeHistoryFlushMsg); ok {
			flushText.WriteString(stripANSIPreserve(flush.Text))
			flushText.WriteString("\n")
		}
	}
	if !containsInOrder(flushText.String(), "say hi", "Supervisor ran", "Background shell 1000 completed") {
		t.Fatalf("expected first batch committed flush to preserve event order, got %q", flushText.String())
	}
	view := stripANSIPreserve(m.View())
	if !strings.Contains(view, "pwd") {
		t.Fatalf("expected pending tool call visible immediately in ongoing mode, got %q", view)
	}

	secondBatch := []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventToolCallCompleted, StepID: "step-1", ToolResult: &tools.Result{CallID: "call-1", Name: toolspec.ToolExecCommand, Output: []byte("/tmp")}}),
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantMessage, StepID: "step-1", Message: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal}}),
	}
	next, cmd = m.Update(runtimeEventBatchMsg{events: secondBatch})
	m = next.(*uiModel)
	msgs = collectCmdMessages(t, cmd)
	flushText.Reset()
	for _, msg := range msgs {
		if flush, ok := msg.(nativeHistoryFlushMsg); ok {
			flushText.WriteString(stripANSIPreserve(flush.Text))
			flushText.WriteString("\n")
		}
	}
	if !containsInOrder(flushText.String(), "pwd", "done") {
		t.Fatalf("expected later committed append after tool completion, got %q", flushText.String())
	}
	view = stripANSIPreserve(m.View())
	if strings.Contains(view, "pwd") {
		t.Fatalf("expected pending tool preview cleared after completion, got %q", view)
	}
}

func TestProjectedRuntimeBatchPreservesQueuedUserFlushBetweenToolCompletionAndAssistantFinal(t *testing.T) {
	m := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), nil,
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "seed"}}),
	)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	m = next.(*uiModel)
	_ = collectCmdMessages(t, startupCmd)

	callMeta := transcript.ToolCallMeta{ToolName: "shell", Command: "pwd", CompactText: "pwd", IsShell: true}
	firstBatch := []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventUserMessageFlushed, StepID: "step-1", UserMessage: "say hi"}),
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventToolCallStarted, StepID: "step-1", ToolCall: &llm.ToolCall{ID: "call-1", Name: string(toolspec.ToolExecCommand), Presentation: toolcodec.EncodeToolCallMeta(callMeta)}}),
	}
	next, cmd := m.Update(runtimeEventBatchMsg{events: firstBatch})
	m = next.(*uiModel)
	_ = collectCmdMessages(t, cmd)

	secondBatch := []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventToolCallCompleted, StepID: "step-1", ToolResult: &tools.Result{CallID: "call-1", Name: toolspec.ToolExecCommand, Output: []byte("/tmp")}}),
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventUserMessageFlushed, StepID: "step-1", UserMessage: "steer now"}),
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantMessage, StepID: "step-1", Message: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal}}),
	}
	next, cmd = m.Update(runtimeEventBatchMsg{events: secondBatch})
	m = next.(*uiModel)
	msgs := collectCmdMessages(t, cmd)
	flushText := strings.Builder{}
	for _, msg := range msgs {
		if flush, ok := msg.(nativeHistoryFlushMsg); ok {
			flushText.WriteString(stripANSIPreserve(flush.Text))
			flushText.WriteString("\n")
		}
	}
	if !containsInOrder(flushText.String(), "pwd", "steer now", "done") {
		t.Fatalf("expected queued user flush preserved between tool completion and assistant final, got %q", flushText.String())
	}
	view := stripANSIPreserve(m.View())
	if strings.Contains(view, "pwd") {
		t.Fatalf("expected pending tool preview cleared after completion, got %q", view)
	}
}

func TestUIInitClearsScreen(t *testing.T) {
	m := newProjectedStaticUIModel()
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected init command")
	}
}

func TestNativeScrollbackReplayIsChunkedForLargeSessions(t *testing.T) {
	lines := make([]string, 0, 10000)
	for i := 0; i < 10000; i++ {
		lines = append(lines, fmt.Sprintf("entry-%d", i))
	}
	rendered := strings.Join(lines, "\n")
	chunks := splitNativeScrollbackChunks(rendered, 4096)
	if len(chunks) < 2 {
		t.Fatalf("expected chunked replay for large history, got %d chunk(s)", len(chunks))
	}
	for idx, chunk := range chunks {
		if len(chunk) > 8192 {
			t.Fatalf("expected bounded chunk size, chunk %d has %d bytes", idx, len(chunk))
		}
	}
}

func TestNativeOngoingShrinksLiveRegionAfterInputShrinkWhenNotStreaming(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.termHeight = 20
	m.windowSizeKnown = true
	m.input = "line 1\nline 2\nline 3"
	m.syncViewport()
	firstPad := m.nativeLiveRegionPad
	first := strings.Split(m.View(), "\n")
	if len(first) != 20 {
		t.Fatalf("expected fresh conversation to fill terminal height before shrink, got %d lines", len(first))
	}
	m.input = ""
	m.syncViewport()
	secondPad := m.nativeLiveRegionPad
	second := strings.Split(m.View(), "\n")
	if len(second) != 20 {
		t.Fatalf("expected fresh conversation to keep filling terminal height after shrink, got %d lines", len(second))
	}
	if secondPad <= firstPad {
		t.Fatalf("expected top padding to grow after input shrink, first=%d second=%d", firstPad, secondPad)
	}
}

func TestNativeOngoingDoesNotRenderBeforeWindowSizeKnown(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.input = "hello"
	got := stripANSIPreserve(m.View())
	if got != "" {
		t.Fatalf("expected no native ongoing render before first window size, got %q", got)
	}
}

func TestNativeOngoingClearsLiveRegionPadWhenStreamingEnds(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.termHeight = 12
	m.windowSizeKnown = true
	m.forwardToView(tui.SetConversationMsg{Ongoing: "line1\nline2"})
	m.syncViewport()
	if !m.nativeStreamingActive {
		t.Fatal("expected streaming active after ongoing stream snapshot")
	}
	m.forwardToView(tui.SetConversationMsg{Ongoing: ""})
	m.syncViewport()
	if m.nativeLiveRegionPad <= 0 {
		t.Fatalf("expected fresh conversation to restore top padding after streaming ends, got %d", m.nativeLiveRegionPad)
	}
	if m.nativeStreamingActive {
		t.Fatal("expected streaming inactive after ongoing clears")
	}
}

func TestNativeDeltaFlushForSingleLineUserMessageHasNoExtraBlankLine(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "seed"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}

	entry := tui.TranscriptEntry{Role: "user", Text: "belissimo.commit"}
	m.forwardToView(tui.AppendTranscriptMsg{Role: entry.Role, Text: entry.Text})
	m.transcriptEntries = append(m.transcriptEntries, entry)
	cmd := m.syncNativeHistoryFromTranscript()
	if cmd == nil {
		t.Fatal("expected native delta flush command")
	}
	msg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", cmd())
	}
	plain := stripANSIPreserve(msg.Text)
	if strings.Contains(plain, "belissimo.commit\n\n") {
		t.Fatalf("expected no extra blank line after user message, got %q", plain)
	}
}

func TestNativeStreamingLinesHiddenWhenNotBusy(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.termHeight = 20
	m.windowSizeKnown = true
	m.forwardToView(tui.SetConversationMsg{Ongoing: "stale stream text"})
	m.setBusy(false)
	view := stripANSIPreserve(m.View())
	if strings.Contains(view, "stale stream text") {
		t.Fatalf("expected stale streaming text hidden when not busy, got %q", view)
	}

	m.setBusy(true)
	view = stripANSIPreserve(m.View())
	if !strings.Contains(view, "stale stream text") {
		t.Fatalf("expected streaming text visible while busy, got %q", view)
	}
}

func TestApplyRuntimeTranscriptPageSpillsHydratedStreamingOverflowWithoutPriorAssistantDelta(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "try again"}}),
	)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 22, Height: 8})
	m = next.(*uiModel)
	_ = collectCmdMessages(t, startupCmd)

	m.setBusy(true)
	lineCount := m.nativeStreamingAssistantLiveBudget(m.termWidth) + 3
	streamText := makeStreamingLines(lineCount)
	cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     1,
		TotalEntries: 1,
		Entries: []clientui.ChatEntry{{
			Role: "user",
			Text: "try again",
		}},
		Ongoing: streamText,
	})
	if cmd == nil {
		t.Fatal("expected runtime transcript page apply to spill hydrated streaming overflow")
	}
	flushText := collectNativeHistoryFlushText(collectCmdMessages(t, cmd))
	if !strings.Contains(flushText, "line-01") {
		t.Fatalf("expected spilled hydrate flush to include earliest streaming line, got %q", flushText)
	}
	view := stripANSIPreserve(m.View())
	if strings.Contains(view, "line-01") {
		t.Fatalf("expected spilled prefix removed from live region, got %q", view)
	}
	if !strings.Contains(view, fmt.Sprintf("line-%02d", lineCount)) {
		t.Fatalf("expected live region to keep latest streaming tail, got %q", view)
	}
	if m.nativeStreamingFlushedLineCount <= 0 {
		t.Fatalf("expected flushed streaming line count to advance, got %d", m.nativeStreamingFlushedLineCount)
	}
	if m.sawAssistantDelta {
		t.Fatal("expected hydrate spill to work without synthesizing assistant delta flag")
	}
}

func TestProjectedRuntimeAssistantDeltaSpillsOverflowIntoScrollback(t *testing.T) {
	m := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), nil,
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "try again"}}),
	)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 22, Height: 8})
	m = next.(*uiModel)
	_ = collectCmdMessages(t, startupCmd)

	lineCount := m.nativeStreamingAssistantLiveBudget(m.termWidth) + 3
	streamText := makeStreamingLines(lineCount)
	next, cmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: streamText}),
	}})
	m = next.(*uiModel)
	flushText := collectNativeHistoryFlushText(collectCmdMessages(t, cmd))
	if !strings.Contains(flushText, "line-01") {
		t.Fatalf("expected runtime delta batch to spill earliest streaming line, got %q", flushText)
	}
	view := stripANSIPreserve(m.View())
	if strings.Contains(view, "line-01") {
		t.Fatalf("expected runtime spill to trim live prefix, got %q", view)
	}
	if !strings.Contains(view, fmt.Sprintf("line-%02d", lineCount)) {
		t.Fatalf("expected runtime spill to preserve latest tail, got %q", view)
	}
}

func TestProjectedRuntimeAssistantFinalAfterSpillDoesNotDuplicateEarlierStreamingLines(t *testing.T) {
	m := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), nil,
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "try again"}}),
	)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 22, Height: 8})
	m = next.(*uiModel)
	_ = collectCmdMessages(t, startupCmd)

	lineCount := m.nativeStreamingAssistantLiveBudget(m.termWidth) + 3
	streamText := makeStreamingLines(lineCount)
	next, firstCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: streamText}),
	}})
	m = next.(*uiModel)
	firstFlush := collectNativeHistoryFlushText(collectCmdMessages(t, firstCmd))
	if !strings.Contains(firstFlush, "line-01") {
		t.Fatalf("expected first spill to include earliest streaming line, got %q", firstFlush)
	}

	next, finalCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantMessage, Message: llm.Message{Role: llm.RoleAssistant, Content: streamText, Phase: llm.MessagePhaseFinal}}),
	}})
	m = next.(*uiModel)
	finalFlush := collectNativeHistoryFlushText(collectCmdMessages(t, finalCmd))
	if strings.Contains(finalFlush, "line-01") {
		t.Fatalf("expected finalized append to skip already spilled prefix, got %q", finalFlush)
	}
	if !strings.Contains(finalFlush, fmt.Sprintf("line-%02d", lineCount)) {
		t.Fatalf("expected finalized append to include remaining streaming tail, got %q", finalFlush)
	}
	if got := strings.Count(firstFlush+finalFlush, "line-01"); got != 1 {
		t.Fatalf("expected earliest streaming line emitted exactly once, got %d in %q%q", got, firstFlush, finalFlush)
	}
	if strings.TrimSpace(m.view.OngoingStreamingText()) != "" {
		t.Fatalf("expected live streaming buffer cleared after commit, got %q", m.view.OngoingStreamingText())
	}
	if m.nativeStreamingText != "" || m.nativeStreamingFlushedLineCount != 0 || m.nativeStreamingDividerFlushed {
		t.Fatalf("expected streaming spill state reset after commit, got text=%q flushed=%d divider=%v", m.nativeStreamingText, m.nativeStreamingFlushedLineCount, m.nativeStreamingDividerFlushed)
	}
}
