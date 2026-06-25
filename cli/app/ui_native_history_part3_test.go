package app

import (
	"core/cli/app/internal/nativescrollback"
	"core/cli/tui"
	"core/server/llm"
	"core/server/runtime"
	"core/shared/clientui"
	"core/shared/transcript"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestProjectedRuntimeAssistantFinalAfterPromotionMatchesTrimmedCommittedText(t *testing.T) {
	m := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), nil,
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "trim me"}}),
	)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 22, Height: 8})
	m = next.(*uiModel)
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)

	lineCount := 8
	committedText := makeStreamingLines(lineCount)
	streamText := committedText + "   "
	next, firstCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: streamText}),
	}})
	m = next.(*uiModel)
	firstFlush := collectNativeHistoryFlushText(collectCmdMessagesApplyingNativeWriteResults(t, m, firstCmd))
	if !strings.Contains(firstFlush, "line-01") {
		t.Fatalf("expected first promotion to include earliest streaming line, got %q", firstFlush)
	}

	next, finalCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantMessage, Message: llm.Message{Role: llm.RoleAssistant, Content: committedText, Phase: llm.MessagePhaseFinal}}),
	}})
	m = next.(*uiModel)
	finalFlush := collectNativeHistoryFlushText(collectCmdMessagesApplyingNativeWriteResults(t, m, finalCmd))
	if !strings.Contains(finalFlush, fmt.Sprintf("line-%02d", lineCount)) {
		t.Fatalf("expected finalized append to include remaining streaming tail, got %q", finalFlush)
	}
	if got := strings.Count(firstFlush+finalFlush, "line-01"); got != 1 {
		t.Fatalf("expected earliest streaming line emitted exactly once after trimmed commit, got %d in %q%q", got, firstFlush, finalFlush)
	}
	if streamState := m.nativeScrollbackLedger.AssistantStreamState(); strings.TrimSpace(streamState.Source) != "" || streamState.ScheduledStableLines != 0 || m.nativeStreamingDividerFlushed {
		t.Fatalf("expected streaming promotion state reset after trimmed commit, got source=%q flushed=%d divider=%v", streamState.Source, streamState.ScheduledStableLines, m.nativeStreamingDividerFlushed)
	}
}

func TestProjectedRuntimeAssistantFinalRewriteOfPromotedPrefixFailsClosed(t *testing.T) {
	m := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), nil,
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "rewrite?"}}),
	)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	m = next.(*uiModel)
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)

	streamText := "original stable\n\nmutable tail"
	next, streamCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: streamText}),
	}})
	m = next.(*uiModel)
	streamFlush := collectNativeHistoryFlushText(collectCmdMessagesApplyingNativeWriteResults(t, m, streamCmd))
	if !strings.Contains(streamFlush, "original stable") {
		t.Fatalf("expected streaming prefix to become permanent before final rewrite, got %q", streamFlush)
	}

	finalText := "rewritten stable\n\nmutable tail"
	next, finalCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantMessage, Message: llm.Message{Role: llm.RoleAssistant, Content: finalText, Phase: llm.MessagePhaseFinal}}),
	}})
	m = next.(*uiModel)
	if !m.nativeScrollbackInvariantSet {
		t.Fatal("expected committed final rewrite of a permanent streamed prefix to fail closed")
	}
	if m.nativeRenderedProjectionCommitPending() {
		t.Fatal("unsafe final rewrite must not schedule an acked rendered projection commit")
	}
	if flushText := collectNativeHistoryFlushText(collectCmdMessages(t, finalCmd)); strings.TrimSpace(flushText) != "" {
		t.Fatalf("unsafe final rewrite must not append replacement native scrollback text, got %q", flushText)
	}
}

func TestNativeStreamingFinalizerRangeSkipsFilteredEmptyCommittedRows(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 80
	m.termHeight = 8
	m.transcriptEntries = []tui.TranscriptEntry{
		{Role: tui.TranscriptRoleUser, Text: "prompt", Committed: true},
		{Role: tui.TranscriptRoleAssistant, Text: "", Committed: true},
	}
	seedProjection := m.nativeCommittedProjection(committedTranscriptEntriesForApp(m.transcriptEntries))
	setNativeCurrentAndRenderedProjectionForTest(m, seedProjection, m.transcriptBaseOffset, len(committedTranscriptEntriesForApp(m.transcriptEntries)))
	seedNativeAssistantStreamForTest(m, "final answer\n")
	m.nativeScrollbackLedger.ObserveAssistantCommitCandidate(nativescrollback.AssistantCommitCandidate{
		StartEntryCount: 2,
		Entries: []nativescrollback.AssistantCommitEntry{{
			Role: string(tui.TranscriptRoleAssistant),
			Text: "final answer",
		}},
	})
	m.transcriptEntries = append(m.transcriptEntries, tui.TranscriptEntry{
		Role:      tui.TranscriptRoleAssistant,
		Text:      "final answer",
		Committed: true,
	})

	nativeCommittedEntries := committedNativeScrollbackEntriesForApp(m.transcriptEntries)
	if got := len(nativeCommittedEntries.Entries); got != 2 {
		t.Fatalf("rendered committed entries = %d, want user plus final assistant after empty row is filtered", got)
	}
	if got := nativeCommittedEntries.SourceIndexes; len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Fatalf("rendered source indexes = %+v, want [0 2]", got)
	}
	projection := m.nativeCommittedProjection(nativeCommittedEntries.Entries)
	cmd, handled := m.finalizeNativeStreamingCommit(projection, nativeCommittedEntries, len(nativeCommittedEntries.Entries))
	if !handled {
		t.Fatal("expected assistant finalizer to map absolute committed range through filtered native entries")
	}
	flushText := collectNativeHistoryFlushText(collectCmdMessages(t, cmd))
	if !strings.Contains(flushText, "final answer") {
		t.Fatalf("expected final assistant tail to flush, got %q", flushText)
	}
}

func TestProjectedRuntimeAssistantFinalExtendsPromotedStreamWithoutDuplicatePrefix(t *testing.T) {
	m := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), nil,
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "auth status?"}}),
	)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	m = next.(*uiModel)
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)

	streamText := "Yes, currently possible.\n\nCurrent code uses `/status` collector path.\n"
	committedText := streamText + "\nI recommend changing picker auth to local state only.\n"
	next, streamCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, StepID: "step-auth", AssistantDelta: streamText}),
	}})
	m = next.(*uiModel)
	firstFlush := collectNativeHistoryFlushText(collectCmdMessages(t, streamCmd))
	if !strings.Contains(firstFlush, "Yes, currently possible.") {
		t.Fatalf("expected stream prefix to promote before final commit, got %q", firstFlush)
	}

	next, finalCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{
			Kind:                       runtime.EventAssistantMessage,
			StepID:                     "step-auth",
			CommittedTranscriptChanged: true,
			CommittedEntryStart:        1,
			CommittedEntryStartSet:     true,
			CommittedEntryCount:        2,
			Message:                    llm.Message{Role: llm.RoleAssistant, Content: committedText, Phase: llm.MessagePhaseFinal},
		}),
	}})
	m = next.(*uiModel)
	finalFlush := collectNativeHistoryFlushText(collectCmdMessages(t, finalCmd))
	if strings.Contains(finalFlush, "Yes, currently possible.") {
		t.Fatalf("expected final commit to avoid duplicating promoted prefix, got %q", finalFlush)
	}
	if !strings.Contains(finalFlush, "I recommend changing picker auth") {
		t.Fatalf("expected final commit to flush committed tail missing from stream, got %q", finalFlush)
	}
	if got := strings.Count(firstFlush+finalFlush, "Yes, currently possible."); got != 1 {
		t.Fatalf("expected promoted prefix once, got %d in %q%q", got, firstFlush, finalFlush)
	}
	if got := len(splitPlainLines(normalizedOutput(firstFlush + "\n" + finalFlush))); got > 10 {
		t.Fatalf("expected rendered output to stay bounded without duplicated block, got %d lines in %q%q", got, firstFlush, finalFlush)
	}
}

func TestStreamingFinalRenderedProjectionAdvancesOnlyAfterTerminalAck(t *testing.T) {
	m := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), nil,
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "prompt"}}),
	)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	m = next.(*uiModel)
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)

	streamText := "stable prefix\n"
	next, streamCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, StepID: "step-ack", AssistantDelta: streamText}),
	}})
	m = next.(*uiModel)
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, streamCmd)
	if got := stripANSIText(m.nativeRenderedSnapshot()); strings.Contains(got, "acked tail") {
		t.Fatalf("test setup unexpectedly rendered final tail before final event: %q", got)
	}

	committedText := streamText + "acked tail\n"
	next, finalCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{
			Kind:                       runtime.EventAssistantMessage,
			StepID:                     "step-ack",
			CommittedTranscriptChanged: true,
			CommittedEntryStart:        1,
			CommittedEntryStartSet:     true,
			CommittedEntryCount:        2,
			Message:                    llm.Message{Role: llm.RoleAssistant, Content: committedText, Phase: llm.MessagePhaseFinal},
		}),
	}})
	m = next.(*uiModel)
	if finalCmd == nil {
		t.Fatal("expected final commit to schedule native terminal write")
	}
	if got := stripANSIText(m.nativeRenderedSnapshot()); strings.Contains(got, "acked tail") {
		t.Fatalf("rendered projection advanced before terminal ack: %q", got)
	}

	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, finalCmd)
	if got := stripANSIText(m.nativeRenderedSnapshot()); !strings.Contains(got, "acked tail") {
		t.Fatalf("rendered projection did not advance after terminal ack: %q", got)
	}
}

func TestStreamingFinalPendingResetSurvivesNoOutputRestoreBeforeTerminalAck(t *testing.T) {
	m := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), nil,
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "prompt"}}),
	)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	m = next.(*uiModel)
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)

	streamText := "stable prefix\n"
	next, streamCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, StepID: "step-reset", AssistantDelta: streamText}),
	}})
	m = next.(*uiModel)
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, streamCmd)

	committedText := streamText + "final tail\n"
	next, finalCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{
			Kind:                       runtime.EventAssistantMessage,
			StepID:                     "step-reset",
			CommittedTranscriptChanged: true,
			CommittedEntryStart:        1,
			CommittedEntryStartSet:     true,
			CommittedEntryCount:        2,
			Message:                    llm.Message{Role: llm.RoleAssistant, Content: committedText, Phase: llm.MessagePhaseFinal},
		}),
	}})
	m = next.(*uiModel)
	if finalCmd == nil {
		t.Fatal("expected final commit to schedule native terminal write")
	}
	if !m.nativeRenderedProjectionCommitPending() {
		t.Fatal("expected rendered projection commit to stay pending before terminal ack")
	}

	if restoreCmd := m.emitCurrentNativeScrollbackState(); restoreCmd != nil {
		for _, msg := range collectCmdMessages(t, restoreCmd) {
			if _, ok := msg.(nativeHistoryFlushMsg); ok {
				t.Fatalf("no-output restore before ack must not schedule another native flush, got %+v", msg)
			}
		}
	}

	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, finalCmd)
	if m.nativeRenderedProjectionCommitPending() {
		t.Fatal("rendered projection commit remained pending after terminal ack")
	}
	if streamState := m.nativeScrollbackLedger.AssistantStreamState(); strings.TrimSpace(streamState.Source) != "" || len(m.nativeScrollbackLedger.AssistantStreamLiveLines()) != 0 {
		t.Fatalf("streaming state was not reset after terminal ack: source=%q tail=%+v", streamState.Source, m.nativeScrollbackLedger.AssistantStreamLiveLines())
	}
}

func TestProjectedRuntimeAssistantCommentaryFinalizesStreamingPromotion(t *testing.T) {
	m := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), nil,
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "commentary please"}}),
	)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 32, Height: 6})
	m = next.(*uiModel)
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)

	streamText := "commentary **bold**\nline-02\n"
	next, firstCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: streamText}),
	}})
	m = next.(*uiModel)
	firstFlush := collectNativeHistoryFlushText(collectCmdMessages(t, firstCmd))
	if strings.Contains(firstFlush, "**bold**") || !strings.Contains(firstFlush, "commentary bold") {
		t.Fatalf("expected commentary prefix to promote as styled markdown, got %q", firstFlush)
	}

	next, finalCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantMessage, Message: llm.Message{Role: llm.RoleAssistant, Content: streamText, Phase: llm.MessagePhaseCommentary}}),
	}})
	m = next.(*uiModel)
	finalFlush := collectNativeHistoryFlushText(collectCmdMessages(t, finalCmd))
	if !strings.Contains(finalFlush, "line-02") {
		t.Fatalf("expected commentary finalization to flush un-emitted tail, got %q", finalFlush)
	}
	if got := strings.Count(firstFlush+finalFlush, "commentary bold"); got != 1 {
		t.Fatalf("expected commentary stable prefix emitted once, got %d in %q%q", got, firstFlush, finalFlush)
	}
}

func TestProjectedRuntimeAssistantFinalHoldsMarkdownTableUntilFinalize(t *testing.T) {
	m := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), nil,
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "table please"}}),
	)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 60, Height: 6})
	m = next.(*uiModel)
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)

	streamText := "| Name | Value |\n| --- | --- |\n| alpha | beta |\n"
	next, firstCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: streamText}),
	}})
	m = next.(*uiModel)
	firstFlush := collectNativeHistoryFlushText(collectCmdMessages(t, firstCmd))
	if strings.Contains(firstFlush, "alpha") || strings.Contains(firstFlush, "beta") {
		t.Fatalf("expected active table to stay out of permanent scrollback before final, got %q", firstFlush)
	}

	next, finalCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantMessage, Message: llm.Message{Role: llm.RoleAssistant, Content: streamText, Phase: llm.MessagePhaseFinal}}),
	}})
	m = next.(*uiModel)
	finalFlush := collectNativeHistoryFlushText(collectCmdMessages(t, finalCmd))
	if !strings.Contains(finalFlush, "alpha") || !strings.Contains(finalFlush, "beta") {
		t.Fatalf("expected finalized table to enter permanent scrollback, got %q", finalFlush)
	}
}

func TestProjectedRuntimeAssistantFinalMatchesMarkdownProjectionAfterHeldSetextAndReference(t *testing.T) {
	m := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), nil)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	m = next.(*uiModel)
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)

	fullText := "Heading\n---\nThis is [link][id]\n\n[id]: https://example.com\n"
	var emitted strings.Builder
	for _, delta := range []string{"Heading\n", "---\nThis is [link][id]\n", "\n[id]: https://example.com\n"} {
		next, cmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
			projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: delta}),
		}})
		m = next.(*uiModel)
		emitted.WriteString(collectNativeHistoryFlushText(collectCmdMessages(t, cmd)))
	}

	next, finalCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantMessage, Message: llm.Message{Role: llm.RoleAssistant, Content: fullText, Phase: llm.MessagePhaseFinal}}),
	}})
	m = next.(*uiModel)
	emitted.WriteString(collectNativeHistoryFlushText(collectCmdMessages(t, finalCmd)))

	got := normalizedOutput(emitted.String())
	want := normalizedOutput(joinedPlainProjectionLines(tui.RenderAssistantMarkdownProjection(fullText, m.theme, m.termWidth)))
	if got != want {
		t.Fatalf("native streamed/final output mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestNativeStreamingResizeKeepsHeldSetextAndReferenceFromPromotingStaleRender(t *testing.T) {
	m := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), nil)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 40, Height: 6})
	m = next.(*uiModel)
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)

	next, firstCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "Heading\n"}),
	}})
	m = next.(*uiModel)
	if flush := collectNativeHistoryFlushText(collectCmdMessages(t, firstCmd)); strings.TrimSpace(flush) != "" {
		t.Fatalf("expected held setext/reference content not to promote before resize, got %q", flush)
	}

	next, resizeCmd := m.Update(tea.WindowSizeMsg{Width: 24, Height: 6})
	m = next.(*uiModel)
	_ = collectCmdMessages(t, resizeCmd)

	next, secondCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "---\nThis is [link][id]\n"}),
	}})
	m = next.(*uiModel)
	flush := collectNativeHistoryFlushText(collectCmdMessages(t, secondCmd))
	if !strings.Contains(flush, "## Heading") {
		t.Fatalf("expected resize-held stream to promote setext render, got %q", flush)
	}
	if strings.Contains(flush, "[link][id]") || strings.Contains(flush, "❮ Heading\n") {
		t.Fatalf("expected resize-held stream not to promote stale pre-heading/reference render, got %q", flush)
	}

	next, thirdCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "\n[id]: https://example.com\n"}),
	}})
	m = next.(*uiModel)
	resolvedFlush := collectNativeHistoryFlushText(collectCmdMessages(t, thirdCmd))
	if !strings.Contains(resolvedFlush, "This is link") {
		t.Fatalf("expected resized stream to promote resolved reference render, got %q", resolvedFlush)
	}
	if strings.Contains(resolvedFlush, "[link][id]") {
		t.Fatalf("expected resized stream not to promote stale unresolved reference render, got %q", resolvedFlush)
	}
}

func TestNativeStreamingToolInterleavingAppendsOnlyUnemittedAssistantTail(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "interleave"}}),
	)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	m = next.(*uiModel)
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)

	streamText := "assistant prefix\nassistant tail\n"
	m.setBusy(true)
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, Ongoing: streamText})
	firstFlush := collectNativeHistoryFlushText(collectCmdMessages(t, m.syncNativeHistoryFromTranscript()))
	if !strings.Contains(firstFlush, "assistant prefix") {
		t.Fatalf("expected assistant prefix promoted before tool row, got %q", firstFlush)
	}

	toolEntries := append([]tui.TranscriptEntry(nil), m.transcriptEntries...)
	toolEntries = append(toolEntries,
		tui.TranscriptEntry{Role: "tool_call", Text: "pwd", ToolCallID: "call_1", ToolCall: &transcript.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "pwd"}},
		tui.TranscriptEntry{Role: "tool_result_ok", Text: "/tmp", ToolCallID: "call_1"},
	)
	m.transcriptEntries = toolEntries
	m.forwardToView(tui.SetConversationMsg{Entries: toolEntries, Ongoing: streamText})
	toolFlush := collectNativeHistoryFlushText(collectCmdMessages(t, m.syncNativeHistoryFromTranscript()))
	if !strings.Contains(toolFlush, "pwd") {
		t.Fatalf("expected tool row to append after promoted assistant prefix, got %q", toolFlush)
	}

	finalEntries := append([]tui.TranscriptEntry(nil), toolEntries...)
	finalEntries = append(finalEntries, tui.TranscriptEntry{Role: "assistant", Text: streamText})
	m.transcriptEntries = finalEntries
	m.forwardToView(tui.SetConversationMsg{Entries: finalEntries, Ongoing: ""})
	finalFlush := collectNativeHistoryFlushText(collectCmdMessages(t, m.syncNativeHistoryFromTranscript()))
	if strings.Contains(finalFlush, "assistant prefix") {
		t.Fatalf("expected final append to avoid duplicating promoted prefix, got %q", finalFlush)
	}
	if !strings.Contains(finalFlush, "assistant tail") {
		t.Fatalf("expected final append to include un-emitted assistant tail, got %q", finalFlush)
	}
}

func TestNativeStreamingFinalBatchWithPrependedToolRowsSkipsStreamedAssistantDuplicate(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "interleave"}}),
	)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	m = next.(*uiModel)
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)

	streamText := "assistant prefix\nassistant tail\n"
	m.setBusy(true)
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, Ongoing: streamText})
	firstFlush := collectNativeHistoryFlushText(collectCmdMessages(t, m.syncNativeHistoryFromTranscript()))
	if !strings.Contains(firstFlush, "assistant prefix") {
		t.Fatalf("expected assistant prefix promoted before final batch, got %q", firstFlush)
	}

	finalEntries := append([]tui.TranscriptEntry(nil), m.transcriptEntries...)
	finalEntries = append(finalEntries,
		tui.TranscriptEntry{Role: "tool_call", Text: "pwd", ToolCallID: "call_1", ToolCall: &transcript.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "pwd"}},
		tui.TranscriptEntry{Role: "tool_result_ok", Text: "/tmp", ToolCallID: "call_1"},
		tui.TranscriptEntry{Role: "assistant", Text: streamText},
	)
	m.transcriptEntries = finalEntries
	m.forwardToView(tui.SetConversationMsg{Entries: finalEntries, Ongoing: ""})
	finalFlush := collectNativeHistoryFlushText(collectCmdMessages(t, m.syncNativeHistoryFromTranscript()))
	if strings.Contains(finalFlush, "assistant prefix") {
		t.Fatalf("expected final batch append to avoid duplicating promoted prefix, got %q", finalFlush)
	}
	if !strings.Contains(finalFlush, "assistant tail") {
		t.Fatalf("expected final batch append to include un-emitted assistant tail, got %q", finalFlush)
	}
	if !strings.Contains(finalFlush, "pwd") {
		t.Fatalf("expected final batch append to include prepended tool row, got %q", finalFlush)
	}
	if got := strings.Count(firstFlush+finalFlush, "assistant prefix"); got != 1 {
		t.Fatalf("expected promoted prefix emitted exactly once, got %d in %q%q", got, firstFlush, finalFlush)
	}
}

func TestProjectedRuntimeFirstAssistantFinalAfterPromotionDoesNotInsertBogusDivider(t *testing.T) {
	m := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), nil)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 22, Height: 8})
	m = next.(*uiModel)
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)

	lineCount := 8
	streamText := makeStreamingLines(lineCount)
	next, firstCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: streamText}),
	}})
	m = next.(*uiModel)
	_ = collectCmdMessages(t, firstCmd)

	next, finalCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantMessage, Message: llm.Message{Role: llm.RoleAssistant, Content: streamText, Phase: llm.MessagePhaseFinal}}),
	}})
	m = next.(*uiModel)
	finalFlush := collectNativeHistoryFlushText(collectCmdMessages(t, finalCmd))
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, finalCmd)
	if strings.Contains(finalFlush, strings.Repeat("─", m.termWidth)) {
		t.Fatalf("expected first assistant promotion commit to avoid bogus divider, got %q", finalFlush)
	}
	if got := strings.TrimSpace(stripANSIPreserve(m.nativeRenderedSnapshot())); strings.HasPrefix(got, tui.TranscriptDivider) {
		t.Fatalf("expected rendered snapshot to avoid leading divider for first assistant reply, got %q", got)
	}
	if got := strings.TrimSpace(stripANSIPreserve(m.nativeCurrentProjection().Render(tui.TranscriptDivider))); strings.HasPrefix(got, tui.TranscriptDivider) {
		t.Fatalf("expected committed projection to avoid leading divider for first assistant reply, got %q", got)
	}
}

func TestProjectedRuntimeAssistantFinalAfterPromotionDefersNormalScrollbackAppendUntilReturnFromDetail(t *testing.T) {
	m := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), nil)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 22, Height: 8})
	m = next.(*uiModel)
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)

	lineCount := 8
	streamText := makeStreamingLines(lineCount)
	next, promotionCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: streamText}),
	}})
	m = next.(*uiModel)
	promotionFlush := collectNativeHistoryFlushText(collectCmdMessagesApplyingNativeWriteResults(t, m, promotionCmd))
	if !strings.Contains(promotionFlush, "line-01") {
		t.Fatalf("expected initial promotion before detail mode, got %q", promotionFlush)
	}

	_ = m.toggleTranscriptModeWithNativeReplay(false)
	if m.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected detail mode, got %q", m.view.Mode())
	}

	next, finalCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantMessage, Message: llm.Message{Role: llm.RoleAssistant, Content: streamText, Phase: llm.MessagePhaseFinal}}),
	}})
	m = next.(*uiModel)
	finalMsgs := collectCmdMessages(t, finalCmd)
	if flushText := collectNativeHistoryFlushText(finalMsgs); strings.TrimSpace(flushText) != "" {
		t.Fatalf("expected no normal-buffer append while detail mode active, got %q", flushText)
	}
	if strings.TrimSpace(m.view.OngoingStreamingText()) != "" {
		t.Fatalf("expected live streaming buffer cleared after commit in detail mode, got %q", m.view.OngoingStreamingText())
	}
	if strings.TrimSpace(m.nativeScrollbackLedger.AssistantStreamState().Source) == "" {
		t.Fatal("expected deferred promotion state preserved until return to ongoing")
	}

	returnCmd := m.toggleTranscriptModeWithNativeReplay(true)
	if m.view.Mode() != tui.ModeOngoing {
		t.Fatalf("expected ongoing mode after return, got %q", m.view.Mode())
	}
	returnFlush := collectNativeHistoryFlushText(collectCmdMessagesApplyingNativeWriteResults(t, m, returnCmd))
	if strings.Contains(returnFlush, "line-01") {
		t.Fatalf("expected return append to avoid duplicating already promoted prefix, got %q", returnFlush)
	}
	if !strings.Contains(returnFlush, fmt.Sprintf("line-%02d", lineCount)) {
		t.Fatalf("expected return append to flush deferred tail, got %q", returnFlush)
	}
	if streamState := m.nativeScrollbackLedger.AssistantStreamState(); strings.TrimSpace(streamState.Source) != "" || streamState.ScheduledStableLines != 0 || m.nativeStreamingDividerFlushed {
		t.Fatalf("expected deferred promotion state cleared after return append, got source=%q flushed=%d divider=%v", streamState.Source, streamState.ScheduledStableLines, m.nativeStreamingDividerFlushed)
	}
}

func TestNativeStreamingResizeKeepsPermanentPromotionWidthFrozen(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "try again"}}),
	)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 22, Height: 8})
	m = next.(*uiModel)
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)

	m.setBusy(true)
	lineCount := 8
	streamText := makeStreamingLines(lineCount)
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, Ongoing: streamText})
	m.layout().syncViewport()

	firstCmd := m.syncNativeHistoryFromTranscript()
	firstFlush := collectNativeHistoryFlushText(collectCmdMessages(t, firstCmd))
	if !strings.Contains(firstFlush, "line-01") {
		t.Fatalf("expected initial promotion to include earliest streaming line, got %q", firstFlush)
	}

	next, resizeCmd := m.Update(tea.WindowSizeMsg{Width: 16, Height: 8})
	m = next.(*uiModel)
	_ = resizeCmd
	if got := m.nativeScrollbackLedger.AssistantStreamState().Width; got != 22 {
		t.Fatalf("expected permanent stream ledger width to stay frozen at 22 after resize, got %d", got)
	}

	resizedCount := lineCount + 1
	resizedStream := makeStreamingLines(resizedCount)
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, Ongoing: resizedStream})
	m.layout().syncViewport()

	secondCmd := m.syncNativeHistoryFromTranscript()
	secondFlush := collectNativeHistoryFlushText(collectCmdMessages(t, secondCmd))
	if got := m.nativeScrollbackLedger.AssistantStreamState().Width; got != 22 {
		t.Fatalf("expected stream tracking width to stay frozen at 22 after resize, got %d", got)
	}
	if got := strings.Count(firstFlush+secondFlush, "line-01"); got != 1 {
		t.Fatalf("expected resized stream not to duplicate earliest line, got %d in %q%q", got, firstFlush, secondFlush)
	}
	view := stripANSIPreserve(m.View())
	if !strings.Contains(view, fmt.Sprintf("line-%02d", resizedCount)) {
		t.Fatalf("expected live region to keep latest resized tail, got %q", view)
	}
}

func TestNativeStreamingLinesRenderAssistantMarkdown(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "try again"}}),
	)
	m.termWidth = 100
	m.termHeight = 24
	m.windowSizeKnown = true
	m.setBusy(true)
	m.sawAssistantDelta = true
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, Ongoing: "**hello**\n`world`"})
	m.layout().syncViewport()

	raw := m.View()
	plain := stripANSIPreserve(raw)
	if strings.Contains(plain, "**hello**") || strings.Contains(plain, "`world`") {
		t.Fatalf("expected markdown markers rendered in live region while streaming, got %q", plain)
	}
	if !strings.Contains(plain, "❮ hello") || !strings.Contains(plain, "world") {
		t.Fatalf("expected markdown-rendered assistant text in live region, got %q", plain)
	}
	if !strings.Contains(raw, "\x1b[") {
		t.Fatalf("expected live region to preserve markdown styling escapes, got raw=%q", raw)
	}
}

func TestNativeStreamingLinesPrefixOnlyFirstWrappedChunk(t *testing.T) {
	rendered := renderNativeStreamingAssistantLines(
		"This streaming line is intentionally long enough to wrap in the ongoing live region.",
		"dark",
		20,
	)
	if len(rendered) < 2 {
		t.Fatalf("expected wrapped streaming output, got %q", rendered)
	}
	if !strings.HasPrefix(rendered[0], "❮ ") {
		t.Fatalf("expected first wrapped chunk to keep assistant prefix, got %q", rendered[0])
	}
	for idx := 1; idx < len(rendered); idx++ {
		if !strings.HasPrefix(rendered[idx], "  ") {
			t.Fatalf("expected wrapped continuation to stay indented, got %q", rendered[idx])
		}
		if strings.HasPrefix(rendered[idx], "❮ ") {
			t.Fatalf("expected assistant prefix only on first wrapped chunk, got %q", rendered[idx])
		}
	}
}

func TestNativeDeltaFlushDoesNotInsertBlankBeforeDivider(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "try again"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}

	entry := tui.TranscriptEntry{Role: "assistant", Text: "Second Stream Check"}
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
	if strings.HasPrefix(plain, "\n") {
		t.Fatalf("expected no leading blank line in delta flush, got %q", plain)
	}
	if strings.Contains(plain, "\n\n❮") {
		t.Fatalf("expected no blank line between divider and assistant line, got %q", plain)
	}
}

func TestNativePostCommitRedrawStableWithoutExtraBlankBeforeDivider(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "try again"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}

	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "Second Stream Check"})
	preCommitView := stripANSIPreserve(m.View())
	if !strings.Contains(preCommitView, "❮ Second Stream Check") {
		t.Fatalf("expected live streaming assistant line before commit, got %q", preCommitView)
	}

	cmd := m.runtimeAdapter().applyChatSnapshot(runtime.ChatSnapshot{
		Entries:   []runtime.ChatEntry{{Role: "user", Text: "try again"}, {Role: "assistant", Text: "Second Stream Check"}},
		Streaming: "",
	})
	if cmd == nil {
		t.Fatal("expected native history flush command on commit snapshot")
	}
	flushMsg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", cmd())
	}
	flushPlain := stripANSIPreserve(flushMsg.Text)
	if strings.HasPrefix(flushPlain, "\n") {
		t.Fatalf("expected no leading blank line in commit delta flush, got %q", flushPlain)
	}
	if strings.Contains(flushPlain, "\n\n❮") {
		t.Fatalf("expected no blank line before assistant line in commit delta flush, got %q", flushPlain)
	}

	postCommitView := stripANSIPreserve(m.View())
	nextView := stripANSIPreserve(m.View())
	if postCommitView != nextView {
		t.Fatalf("expected stable post-commit live region across redraws\nfirst=%q\nsecond=%q", postCommitView, nextView)
	}
	if strings.Contains(postCommitView, "Second Stream Check") {
		t.Fatalf("expected live streaming lane to be cleared after commit, got %q", postCommitView)
	}
}

func TestNativeStreamingDividerPersistsInTightViewport(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "prompt"}}),
	)
	m.termWidth = 40
	m.termHeight = 6
	m.windowSizeKnown = true
	m.setBusy(true)
	m.sawAssistantDelta = true
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, Ongoing: "line1\nline2\nline3"})
	m.layout().syncViewport()

	plain := stripANSIPreserve(m.View())
	if !strings.Contains(plain, strings.Repeat("─", m.termWidth)) {
		t.Fatalf("expected divider to remain visible in tight viewport, got %q", plain)
	}
	if !strings.Contains(plain, "❮ line1") {
		t.Fatalf("expected first streamed line to remain visible in tight viewport, got %q", plain)
	}
}

func TestNativeHistoryFlushWaitsForTargetSequenceBeforeRearmingRuntimeEvents(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.pendingRuntimeEvents = []clientui.Event{{Kind: clientui.EventConversationUpdated}}
	m.waitRuntimeEventAfterFlushSequence = 2

	firstCmd := m.handleNativeHistoryFlush(nativeHistoryFlushMsg{Text: "first", Sequence: 1})
	if m.waitRuntimeEventAfterFlushSequence != 2 {
		t.Fatalf("expected runtime-event wait to remain armed for sequence 2, got %d", m.waitRuntimeEventAfterFlushSequence)
	}
	if got := len(m.pendingRuntimeEvents); got != 1 {
		t.Fatalf("expected pending runtime events preserved before target flush, got %d", got)
	}
	for _, msg := range collectCmdMessagesApplyingNativeWriteResults(t, m, firstCmd) {
		if _, ok := msg.(runtimeEventBatchMsg); ok {
			t.Fatalf("did not expect runtime rearm before target flush, got %T", msg)
		}
	}

	secondCmd := m.handleNativeHistoryFlush(nativeHistoryFlushMsg{Text: "second", Sequence: 2})
	if secondCmd == nil {
		t.Fatal("expected target flush to rearm runtime events")
	}
	var rearmed runtimeEventBatchMsg
	foundRearm := false
	for _, msg := range collectCmdMessagesApplyingNativeWriteResults(t, m, secondCmd) {
		batch, ok := msg.(runtimeEventBatchMsg)
		if !ok {
			continue
		}
		rearmed = batch
		foundRearm = true
	}
	if !foundRearm {
		t.Fatal("expected runtime event batch after target flush")
	}
	if got := len(rearmed.events); got != 1 {
		t.Fatalf("expected exactly one rearmed pending runtime event, got %d", got)
	}
	if got := rearmed.events[0].Kind; got != clientui.EventConversationUpdated {
		t.Fatalf("rearmed event kind = %q, want %q", got, clientui.EventConversationUpdated)
	}
	if m.waitRuntimeEventAfterFlushSequence != 0 {
		t.Fatalf("expected runtime-event wait cleared after target flush, got %d", m.waitRuntimeEventAfterFlushSequence)
	}
	if got := len(m.pendingRuntimeEvents); got != 0 {
		t.Fatalf("expected pending runtime events drained after target flush, got %d", got)
	}
}

func TestNativeStreamingStableTailRemainsLiveUntilNativeFlushAck(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "prompt"}}),
	)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	m = next.(*uiModel)
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)
	m.nativeScrollbackLedger.DiscardScheduled()

	m.setBusy(true)
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, Ongoing: "line1\nline2\n"})
	streamCmd := m.syncNativeHistoryFromTranscript()
	streamFlush, ok := streamCmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected streaming stable flush, got %T", streamCmd())
	}
	if got := joinedPlainProjectionLines(m.nativeScrollbackLedger.AssistantStreamLiveLines()); !strings.Contains(got, "line1") || !strings.Contains(got, "line2") {
		t.Fatalf("expected scheduled stable line to remain live before flush ack, got %q", got)
	}

	laterCmd := m.emitNativeRenderedTextWithOptions("later", false)
	laterFlush, ok := laterCmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected later flush, got %T", laterCmd())
	}
	if cmd := m.handleNativeHistoryFlush(laterFlush); cmd != nil {
		t.Fatalf("expected out-of-order later flush to buffer without commands, got %T", cmd())
	}
	if got := joinedPlainProjectionLines(m.nativeScrollbackLedger.AssistantStreamLiveLines()); !strings.Contains(got, "line1") || !strings.Contains(got, "line2") {
		t.Fatalf("expected out-of-order flush to leave scheduled stable line live before ack, got %q", got)
	}

	flushCmd := m.handleNativeHistoryFlush(streamFlush)
	for _, msg := range collectCmdMessagesApplyingNativeWriteResults(t, m, flushCmd) {
		if _, ok := msg.(runtimeEventBatchMsg); ok {
			t.Fatalf("runtime events resumed before stable native flush ack updated live tail")
		}
	}
	if got := joinedPlainProjectionLines(m.nativeScrollbackLedger.AssistantStreamLiveLines()); strings.Contains(got, "line1") || !strings.Contains(got, "line2") {
		t.Fatalf("expected terminal ack to remove only acked stable line from live tail, got %q", got)
	}
}

func TestRuntimeBatchNativeFlushArmsFlushFenceBeforeRearmingRuntimeEvents(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.pendingRuntimeEvents = []clientui.Event{{Kind: clientui.EventConversationUpdated}}

	next, cmd := m.handleRuntimeEventBatch([]clientui.Event{{
		Kind:                       clientui.EventUserMessageFlushed,
		UserMessage:                "prompt",
		CommittedTranscriptChanged: true,
		CommittedEntryCount:        1,
		CommittedEntryStart:        0,
		CommittedEntryStartSet:     true,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "user", Text: "prompt"}},
	}})
	m = next

	if m.waitRuntimeEventAfterFlushSequence == 0 {
		t.Fatal("expected runtime-event wait to be fenced behind native flush sequence")
	}
	if m.waitRuntimeEventAfterFlushSequence != m.nativeLastScheduledFlushSequence() {
		t.Fatalf("wait flush sequence = %d, want native sequence %d", m.waitRuntimeEventAfterFlushSequence, m.nativeLastScheduledFlushSequence())
	}
	foundFlush := false
	for _, msg := range collectCmdMessages(t, cmd) {
		switch msg.(type) {
		case nativeHistoryFlushMsg:
			foundFlush = true
		case runtimeEventBatchMsg:
			t.Fatalf("runtime event rearmed before native flush ack: %+v", msg)
		}
	}
	if !foundFlush {
		t.Fatal("expected runtime batch to emit native history flush")
	}
	if got := len(m.pendingRuntimeEvents); got != 1 {
		t.Fatalf("pending runtime events drained before flush ack: %d", got)
	}
}

func TestRuntimeBatchHydrationWithNativeFlushWaitsForFlushAndHydration(t *testing.T) {
	client := &refreshingRuntimeClient{transcripts: []clientui.TranscriptPage{{
		SessionID:    "session-1",
		Revision:     2,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "user", Text: "prompt"}},
	}}}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.pendingRuntimeEvents = []clientui.Event{{Kind: clientui.EventLocalEntryAdded}}

	next, cmd := m.handleRuntimeEventBatch([]clientui.Event{
		{
			Kind:                       clientui.EventUserMessageFlushed,
			UserMessage:                "prompt",
			CommittedTranscriptChanged: true,
			TranscriptRevision:         1,
			CommittedEntryCount:        1,
			CommittedEntryStart:        0,
			CommittedEntryStartSet:     true,
			TranscriptEntries:          []clientui.ChatEntry{{Role: "user", Text: "prompt"}},
		},
		{
			Kind:                       clientui.EventConversationUpdated,
			CommittedTranscriptChanged: true,
			TranscriptRevision:         2,
			CommittedEntryCount:        2,
		},
	})
	m = next

	if !m.waitRuntimeEventAfterHydration {
		t.Fatal("expected hydration fence to remain armed")
	}
	if m.waitRuntimeEventAfterFlushSequence == 0 {
		t.Fatal("expected native flush fence to remain armed")
	}
	foundFlush := false
	foundHydration := false
	for _, msg := range collectCmdMessages(t, cmd) {
		switch msg.(type) {
		case nativeHistoryFlushMsg:
			foundFlush = true
		case runtimeTranscriptRefreshedMsg:
			foundHydration = true
		case runtimeEventBatchMsg:
			t.Fatalf("runtime event rearmed before flush+hydration completed: %+v", msg)
		}
	}
	if !foundFlush {
		t.Fatal("expected native flush command")
	}
	if !foundHydration {
		t.Fatal("expected hydration command")
	}
	if got := len(m.pendingRuntimeEvents); got != 1 {
		t.Fatalf("pending runtime events drained before fences cleared: %d", got)
	}
}

func TestNativeHistoryReplayDefersWhileDetailAndFlushesOnReturn(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "seed"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}
	startupMsg, ok := startupCmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", startupCmd())
	}
	if !strings.Contains(stripANSIPreserve(startupMsg.Text), "seed") {
		t.Fatalf("expected startup replay to include seed, got %q", startupMsg.Text)
	}

	m.forwardToView(tui.ToggleModeMsg{})
	if m.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected detail mode, got %q", m.view.Mode())
	}

	steered := tui.TranscriptEntry{Role: "user", Text: "steered message"}
	m.transcriptEntries = append(m.transcriptEntries, steered)
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})
	if cmd := m.syncNativeHistoryFromTranscript(); cmd != nil {
		t.Fatalf("expected native replay to stay deferred while detail is active, got %T", cmd())
	}
	if strings.Contains(m.nativeRenderedSnapshot(), "steered message") {
		t.Fatalf("expected rendered normal-buffer snapshot to remain stale while detail is active, got %q", m.nativeRenderedSnapshot())
	}

	m.forwardToView(tui.ToggleModeMsg{})
	if m.view.Mode() != tui.ModeOngoing {
		t.Fatalf("expected ongoing mode, got %q", m.view.Mode())
	}
	cmd := m.emitCurrentNativeHistorySnapshot(nativeHistoryReplayPermitNone)
	if cmd == nil {
		t.Fatal("expected deferred native replay when returning to ongoing")
	}
	flushMsg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", cmd())
	}
	plain := stripANSIPreserve(flushMsg.Text)
	if !strings.Contains(plain, "steered message") {
		t.Fatalf("expected deferred replay to include steered message, got %q", plain)
	}
}

func TestNativeHistorySnapshotDoesNotReplaySameSessionRewriteInOngoingMode(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.windowSizeKnown = true
	initial := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", Lines: []string{"❯ commit/push"}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ before"}},
	}}
	setNativeCurrentAndRenderedProjectionForTest(m, initial, 0, len(initial.Blocks))
	updated := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", Lines: []string{"❯ commit/push"}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ after"}},
	}}
	setNativeCurrentProjectionForTest(m, updated, 0, len(updated.Blocks))

	cmd := m.emitCurrentNativeHistorySnapshot(nativeHistoryReplayPermitNone)
	for _, msg := range collectCmdMessages(t, cmd) {
		if _, ok := msg.(nativeHistoryFlushMsg); ok {
			t.Fatalf("did not expect same-session rewrite to replay committed scrollback, got %+v", msg)
		}
	}
	if got := m.nativeRenderedSnapshot(); got == m.nativeCurrentProjection().Render(tui.TranscriptDivider) {
		t.Fatalf("expected rendered snapshot to remain unchanged after invariant, got %q", got)
	}
	if !m.nativeScrollbackInvariantSet {
		t.Fatal("expected same-session rewrite to record native scrollback invariant")
	}
}

func TestNativeHistorySnapshotAppendsVisibleSuffixAfterHiddenRewriteWithoutReplay(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.windowSizeKnown = true
	previous := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", EntryIndex: 0, EntryEnd: 0, Lines: []string{"❯ before compaction"}},
		{Role: "assistant", DividerGroup: "assistant", EntryIndex: 1, EntryEnd: 1, Lines: []string{"❮ existing answer"}},
	}}
	current := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "compaction_notice", DividerGroup: "notice", EntryIndex: 3, EntryEnd: 3, Lines: []string{"context compacted for the 1st time"}},
	}}
	setNativeCurrentProjectionForTest(m, current, 0, len(current.Blocks))
	setNativeRenderedProjectionForTest(m, previous, 0)

	cmd := m.emitCurrentNativeHistorySnapshot(nativeHistoryReplayPermitNone)
	if cmd == nil {
		t.Fatal("expected hidden rewrite to append visible suffix")
	}
	msg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", cmd())
	}
	plain := stripANSIText(msg.Text)
	if !strings.Contains(plain, "context compacted for the 1st time") {
		t.Fatalf("expected visible suffix append to include compaction notice, got %q", plain)
	}
	if strings.Contains(plain, "before compaction") || strings.Contains(plain, "existing answer") {
		t.Fatalf("expected hidden rewrite append to avoid replaying prior visible history, got %q", plain)
	}
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, cmd)
	if got := m.nativeRenderedSnapshot(); got != m.nativeCurrentProjection().Render(tui.TranscriptDivider) {
		t.Fatalf("expected rendered snapshot rebased to current projection, got %q", got)
	}
}

func TestNativeHistorySnapshotDoesNotAppendSuffixWhenVisibleRewriteTouchesRenderedFrontier(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.windowSizeKnown = true
	previous := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", EntryIndex: 0, EntryEnd: 0, Lines: []string{"❯ commit/push"}},
		{Role: "assistant", DividerGroup: "assistant", EntryIndex: 1, EntryEnd: 1, Lines: []string{"❮ before"}},
	}}
	current := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", EntryIndex: 0, EntryEnd: 0, Lines: []string{"❯ commit/push"}},
		{Role: "assistant", DividerGroup: "assistant", EntryIndex: 1, EntryEnd: 1, Lines: []string{"❮ after"}},
		{Role: "compaction_notice", DividerGroup: "notice", EntryIndex: 3, EntryEnd: 3, Lines: []string{"context compacted for the 1st time"}},
	}}
	setNativeCurrentProjectionForTest(m, current, 0, len(current.Blocks))
	setNativeRenderedProjectionForTest(m, previous, 0)

	cmd := m.emitCurrentNativeHistorySnapshot(nativeHistoryReplayPermitNone)
	for _, msg := range collectCmdMessages(t, cmd) {
		if _, ok := msg.(nativeHistoryFlushMsg); ok {
			t.Fatalf("did not expect visible rewrite to append stale suffix after divergence, got %+v", msg)
		}
	}
}

func TestNativeScrollbackBlocksAssistantFlushesAfterSameSessionDivergence(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "commit/push"}, {Role: "assistant", Text: "before"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)

	m.transcriptEntries[1].Text = "after"
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})
	recoveryCmd := m.syncNativeHistoryFromTranscript()
	for _, msg := range collectCmdMessages(t, recoveryCmd) {
		if _, ok := msg.(nativeHistoryFlushMsg); ok {
			t.Fatalf("did not expect same-session rewrite to replay committed history, got %+v", msg)
		}
	}
	if plain := stripANSIText(m.nativeRenderedSnapshot()); !strings.Contains(plain, "commit/push") || !strings.Contains(plain, "before") || strings.Contains(plain, "after") {
		t.Fatalf("expected same-session rewrite to keep rendered baseline unchanged, got %q", plain)
	}
	if !m.nativeScrollbackInvariantSet {
		t.Fatal("expected same-session rewrite to record native scrollback invariant")
	}

	m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: "next answer"})
	m.transcriptEntries = append(m.transcriptEntries, tui.TranscriptEntry{Role: "assistant", Text: "next answer"})
	appendCmd := m.syncNativeHistoryFromTranscript()
	if appendCmd != nil {
		t.Fatalf("expected append blocked after invariant violation, got %T", appendCmd)
	}
}

func TestNativeScrollbackEmptyCommittedTailAfterRenderedHistoryRecordsInvariant(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.windowSizeKnown = true
	rendered := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", Lines: []string{"❯ commit/push"}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ before"}},
	}}
	setNativeCurrentAndRenderedProjectionForTest(m, rendered, 0, len(rendered.Blocks))

	cmd := m.syncNativeHistoryFromTranscript()
	for _, msg := range collectCmdMessages(t, cmd) {
		if _, ok := msg.(nativeHistoryFlushMsg); ok {
			t.Fatalf("did not expect empty same-session tail to replay or erase native history, got %+v", msg)
		}
	}
	if got := stripANSIText(m.nativeRenderedSnapshot()); !strings.Contains(got, "commit/push") || !strings.Contains(got, "before") {
		t.Fatalf("expected rendered baseline preserved after empty-tail invariant, got %q", got)
	}
	if !m.nativeScrollbackInvariantSet {
		t.Fatal("expected empty same-session tail to record native scrollback invariant")
	}
}

func TestNativeScrollbackCommittedCountRegressionRecordsInvariantWithoutRebase(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.windowSizeKnown = true
	rendered := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", Lines: []string{"❯ commit/push"}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ before"}},
	}}
	setNativeCurrentAndRenderedProjectionForTest(m, rendered, 0, len(rendered.Blocks))
	m.transcriptEntries = []tui.TranscriptEntry{{Role: tui.TranscriptRoleUser, Text: "commit/push"}}

	cmd := m.syncNativeHistoryFromTranscript()
	for _, msg := range collectCmdMessages(t, cmd) {
		if _, ok := msg.(nativeHistoryFlushMsg); ok {
			t.Fatalf("did not expect committed-count regression to replay native history, got %+v", msg)
		}
	}
	if got := stripANSIText(m.nativeCurrentProjection().Render(tui.TranscriptDivider)); !strings.Contains(got, "before") {
		t.Fatalf("expected native current projection not to silently rebase to shorter transcript, got %q", got)
	}
	if !m.nativeScrollbackInvariantSet {
		t.Fatal("expected committed-count regression to record native scrollback invariant")
	}
}

func TestNativeScrollbackEmptyContinuityRecoveryClearsThroughTypedPermit(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.termHeight = 10
	m.windowSizeKnown = true
	rendered := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", Lines: []string{"❯ commit/push"}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ before"}},
	}}
	setNativeCurrentAndRenderedProjectionForTest(m, rendered, 0, len(rendered.Blocks))
	m.nativeHistoryReplayPermit = nativeHistoryReplayPermitContinuityRecovery

	cmd := m.syncNativeHistoryFromTranscript()
	msgs := collectCmdMessages(t, cmd)
	foundClear := false
	for _, msg := range msgs {
		if strings.Contains(fmt.Sprintf("%T", msg), "clearScreenMsg") {
			foundClear = true
			break
		}
	}
	if !foundClear {
		t.Fatalf("expected typed continuity recovery for empty transcript to clear native scrollback, got messages=%v", msgs)
	}
	if m.nativeScrollbackInvariantSet {
		t.Fatalf("did not expect empty continuity recovery to record invariant: %+v", m.nativeScrollbackInvariant)
	}
}

func TestNativeDetailExitRecordsInvariantWhenDetailChangedState(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "seed"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)

	enterCmd := m.toggleTranscriptModeWithNativeReplay(false)
	if m.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected detail mode, got %q", m.view.Mode())
	}
	_ = collectCmdMessages(t, enterCmd)

	cmd := m.runtimeAdapter().applyChatSnapshot(runtime.ChatSnapshot{
		Entries: []runtime.ChatEntry{{Role: "user", Text: "fresh root"}, {Role: "assistant", Text: "rewritten tail"}},
	})
	if cmd != nil {
		t.Fatalf("expected replay to stay deferred while detail is active, got %T", cmd())
	}

	leaveCmd := m.toggleTranscriptModeWithNativeReplay(true)
	if m.view.Mode() != tui.ModeOngoing {
		t.Fatalf("expected ongoing mode, got %q", m.view.Mode())
	}
	for _, msg := range collectCmdMessages(t, leaveCmd) {
		if _, ok := msg.(nativeHistoryFlushMsg); ok {
			t.Fatalf("expected detail exit to avoid native replay, got %+v", msg)
		}
	}
	plain := stripANSIText(m.nativeRenderedSnapshot())
	if !strings.Contains(plain, "seed") || strings.Contains(plain, "fresh root") || strings.Contains(plain, "rewritten tail") {
		t.Fatalf("expected detail exit divergence to keep previous rendered baseline, got %q", plain)
	}
	if !m.nativeScrollbackInvariantSet {
		t.Fatal("expected detail exit divergence to record native scrollback invariant")
	}

	m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: "next answer"})
	m.transcriptEntries = append(m.transcriptEntries, tui.TranscriptEntry{Role: "assistant", Text: "next answer"})
	appendCmd := m.syncNativeHistoryFromTranscript()
	if appendCmd != nil {
		t.Fatalf("expected future append blocked after invariant violation, got %T", appendCmd)
	}
}

func TestDefaultDetailTranscriptHydrationSyncsNativeProjectionWithoutTailReplacement(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "seed"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)

	enterCmd := m.toggleTranscriptModeWithNativeReplay(false)
	if m.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected detail mode, got %q", m.view.Mode())
	}
	_ = collectCmdMessages(t, enterCmd)

	cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, clientui.TranscriptPage{
		SessionID:      "session-1",
		Revision:       2,
		Offset:         0,
		TotalEntries:   2,
		NextOffset:     2,
		HasMore:        false,
		Entries:        []clientui.ChatEntry{{Role: "user", Text: "fresh root"}, {Role: "assistant", Text: "rewritten tail"}},
		Streaming:      "",
		StreamingError: "",
	}, clientui.TranscriptRecoveryCauseNone)
	for _, msg := range collectCmdMessages(t, cmd) {
		if flush, ok := msg.(nativeHistoryFlushMsg); ok {
			t.Fatalf("did not expect detail hydration to emit native flush before mode exit, got %+v", flush)
		}
	}

	nativePlain := stripANSIText(m.nativeCurrentProjection().Render(tui.TranscriptDivider))
	if !strings.Contains(nativePlain, "fresh root") || !strings.Contains(nativePlain, "rewritten tail") || strings.Contains(nativePlain, "seed") {
		t.Fatalf("expected default detail hydration to sync native projection, got %q", nativePlain)
	}
	detailPlain := stripANSIAndTrimRight(m.view.View())
	if !strings.Contains(detailPlain, "fresh root") || !strings.Contains(detailPlain, "rewritten tail") {
		t.Fatalf("expected default detail hydration to merge detail page, got %q", detailPlain)
	}
}

func TestNativeDetailRepeatedTogglesKeepAppendBlockedAfterInvariant(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "seed"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)

	_ = collectCmdMessages(t, m.toggleTranscriptModeWithNativeReplay(false))
	cmd := m.runtimeAdapter().applyChatSnapshot(runtime.ChatSnapshot{
		Entries: []runtime.ChatEntry{{Role: "user", Text: "fresh root"}, {Role: "assistant", Text: "rewritten tail"}},
	})
	if cmd != nil {
		t.Fatalf("expected detail-mode hydrate repair to stay deferred, got %T", cmd())
	}
	if leaveCmd := m.toggleTranscriptModeWithNativeReplay(true); leaveCmd != nil {
		for _, msg := range collectCmdMessages(t, leaveCmd) {
			if _, ok := msg.(nativeHistoryFlushMsg); ok {
				t.Fatalf("expected first detail exit to avoid native replay, got %+v", msg)
			}
		}
	}

	for i := 0; i < 2; i++ {
		_ = collectCmdMessages(t, m.toggleTranscriptModeWithNativeReplay(false))
		if leaveCmd := m.toggleTranscriptModeWithNativeReplay(true); leaveCmd != nil {
			for _, msg := range collectCmdMessages(t, leaveCmd) {
				if _, ok := msg.(nativeHistoryFlushMsg); ok {
					t.Fatalf("expected repeated detail exit %d to avoid native replay, got %+v", i, msg)
				}
			}
		}
	}

	m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: "echo hi"})
	m.transcriptEntries = append(m.transcriptEntries, tui.TranscriptEntry{Role: "assistant", Text: "echo hi"})
	appendCmd := m.syncNativeHistoryFromTranscript()
	if appendCmd != nil {
		t.Fatalf("expected next append blocked after invariant violation, got %T", appendCmd)
	}
	if !m.nativeScrollbackInvariantSet {
		t.Fatal("expected repeated detail toggles to preserve native scrollback invariant")
	}
}

func TestNativeScrollbackRecordsInvariantInDebugModeOnSameSessionDivergence(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIDebug(true),
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "commit/push"}, {Role: "assistant", Text: "before"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}

	m.transcriptEntries[1].Text = "after"
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})
	_ = m.syncNativeHistoryFromTranscript()

	if !m.nativeScrollbackInvariantSet {
		t.Fatal("expected debug-mode divergence to record native scrollback invariant")
	}
	if !strings.Contains(m.nativeScrollbackInvariant.Error(), "same-session committed transcript divergence") {
		t.Fatalf("unexpected invariant: %+v", m.nativeScrollbackInvariant)
	}
}

func TestNativeHistorySnapshotReplaysDuringContinuityRecovery(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.windowSizeKnown = true
	initial := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", Lines: []string{"❯ commit/push"}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ before"}},
	}}
	setNativeCurrentAndRenderedProjectionForTest(m, initial, 0, len(initial.Blocks))
	m.nativeHistoryReplayPermit = nativeHistoryReplayPermitContinuityRecovery
	updated := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", Lines: []string{"❯ commit/push"}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ after"}},
	}}
	setNativeCurrentProjectionForTest(m, updated, 0, len(updated.Blocks))

	cmd := m.emitCurrentNativeHistorySnapshot(nativeHistoryReplayPermitContinuityRecovery)
	if cmd == nil {
		t.Fatal("expected continuity recovery to replay committed scrollback")
	}
	msgs := collectCmdMessages(t, cmd)
	if len(msgs) != 2 {
		t.Fatalf("expected clear-screen plus native history flush during continuity recovery, got %d message(s)", len(msgs))
	}
	msg, ok := msgs[1].(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg as replay payload, got %T", msgs[1])
	}
	plain := stripANSIPreserve(msg.Text)
	if !strings.Contains(plain, "commit/push") || !strings.Contains(plain, "after") || strings.Contains(plain, "before") {
		t.Fatalf("expected continuity recovery replay to emit authoritative transcript, got %q", plain)
	}
}

func TestNativeHistorySnapshotAuthoritativeHydrateRecordsInvariantWithoutReplay(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.windowSizeKnown = true
	initial := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", Lines: []string{"❯ commit/push"}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ before"}},
	}}
	setNativeCurrentAndRenderedProjectionForTest(m, initial, 0, len(initial.Blocks))
	updated := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", Lines: []string{"❯ commit/push"}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ after"}},
	}}
	setNativeCurrentProjectionForTest(m, updated, 0, len(updated.Blocks))

	cmd := m.emitCurrentNativeHistorySnapshot(nativeHistoryReplayPermitNone)
	msgs := collectCmdMessages(t, cmd)
	for _, msg := range msgs {
		if _, ok := msg.(nativeHistoryFlushMsg); ok {
			t.Fatalf("did not expect authoritative hydrate divergence to replay normal-buffer history, got %+v", msgs)
		}
	}
	if got := stripANSIPreserve(m.nativeRenderedSnapshot()); !strings.Contains(got, "commit/push") || !strings.Contains(got, "before") || strings.Contains(got, "after") {
		t.Fatalf("expected authoritative hydrate divergence to keep rendered snapshot unchanged, got %q", got)
	}
	if !m.nativeScrollbackInvariantSet {
		t.Fatal("expected authoritative hydrate divergence to record native scrollback invariant")
	}
}

func TestNativeHistorySnapshotAuthoritativeHydrateRecordsInvariantInDebugMode(t *testing.T) {
	m := newProjectedStaticUIModel(WithUIDebug(true))
	m.termWidth = 80
	m.windowSizeKnown = true
	initial := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", Lines: []string{"❯ commit/push"}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ before"}},
	}}
	setNativeCurrentAndRenderedProjectionForTest(m, initial, 0, len(initial.Blocks))
	updated := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", Lines: []string{"❯ commit/push"}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ after"}},
	}}
	setNativeCurrentProjectionForTest(m, updated, 0, len(updated.Blocks))

	cmd := m.emitCurrentNativeHistorySnapshot(nativeHistoryReplayPermitNone)
	msgs := collectCmdMessages(t, cmd)
	for _, msg := range msgs {
		if _, ok := msg.(nativeHistoryFlushMsg); ok {
			t.Fatalf("did not expect debug authoritative hydrate divergence to replay normal-buffer history, got %+v", msgs)
		}
	}
	if got := stripANSIPreserve(m.nativeRenderedSnapshot()); !strings.Contains(got, "commit/push") || !strings.Contains(got, "before") || strings.Contains(got, "after") {
		t.Fatalf("expected debug authoritative hydrate divergence to keep rendered snapshot unchanged, got %q", got)
	}
	if !m.nativeScrollbackInvariantSet {
		t.Fatal("expected debug authoritative hydrate divergence to record native scrollback invariant")
	}
}

func TestNativeHistorySnapshotAppendsAcrossSlidingTailWindowWithoutReplay(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.windowSizeKnown = true
	current := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "cache_warning", DividerGroup: "warning", Lines: []string{"⚠ Cache miss: postfix-compatible supervisor cache reuse disappeared, -79k tokens"}},
		{Role: "reviewer_suggestions", DividerGroup: "reviewer", Lines: []string{"§ Supervisor suggested:", "1. Add verification notes."}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ previous answer"}},
		{Role: "user", DividerGroup: "user", Lines: []string{"❯ did you fix the actual transcript bugs, or only reporting/observability?"}},
	}}
	rendered := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "system", DividerGroup: "user", Lines: []string{"❯ earlier question"}},
		{Role: "cache_warning", DividerGroup: "warning", Lines: []string{"⚠ Cache miss: postfix-compatible supervisor cache reuse disappeared, -79k tokens"}},
		{Role: "reviewer_suggestions", DividerGroup: "reviewer", Lines: []string{"§ Supervisor suggested:", "1. Add verification notes."}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ previous answer"}},
	}}
	setNativeCurrentProjectionForTest(m, current, 101, 105)
	setNativeRenderedProjectionForTest(m, rendered, 100)

	cmd := m.emitCurrentNativeHistorySnapshot(nativeHistoryReplayPermitNone)
	if cmd == nil {
		t.Fatal("expected sliding tail window to append only the new suffix")
	}
	msg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg for sliding tail append, got %T", cmd())
	}
	plain := stripANSIPreserve(msg.Text)
	if !strings.Contains(plain, "did you fix the actual transcript bugs") {
		t.Fatalf("expected sliding tail append to emit newest visible block, got %q", plain)
	}
	if strings.Contains(plain, "Cache miss") || strings.Contains(plain, "Supervisor suggested") {
		t.Fatalf("expected sliding tail append to avoid re-emitting overlapped committed rows, got %q", plain)
	}
}

func TestModeRestoreDivergenceRecordsInvariantWithoutReplay(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "seed"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, startupCmd)

	enterCmd := m.toggleTranscriptModeWithNativeReplay(false)
	if m.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected detail mode, got %q", m.view.Mode())
	}
	_ = collectCmdMessages(t, enterCmd)

	cmd := m.runtimeAdapter().applyChatSnapshot(runtime.ChatSnapshot{
		Entries: []runtime.ChatEntry{{Role: "user", Text: "fresh root"}, {Role: "assistant", Text: "rewritten tail"}},
	})
	if cmd != nil {
		t.Fatalf("expected hydrate repair replay to stay deferred while detail is active, got %T", cmd())
	}

	leaveCmd := m.toggleTranscriptModeWithNativeReplay(true)
	if m.view.Mode() != tui.ModeOngoing {
		t.Fatalf("expected ongoing mode, got %q", m.view.Mode())
	}
	for _, msg := range collectCmdMessages(t, leaveCmd) {
		if _, ok := msg.(nativeHistoryFlushMsg); ok {
			t.Fatalf("expected mode-restore to avoid native replay, got %+v", msg)
		}
	}
	plain := stripANSIText(m.nativeRenderedSnapshot())
	if !strings.Contains(plain, "seed") || strings.Contains(plain, "fresh root") || strings.Contains(plain, "rewritten tail") {
		t.Fatalf("expected mode-restore divergence to keep previous rendered baseline, got %q", plain)
	}
	if !m.nativeScrollbackInvariantSet {
		t.Fatal("expected mode-restore divergence to record native scrollback invariant")
	}
	if m.transientStatusKind != uiStatusNoticeError {
		t.Fatalf("expected invariant to mirror an error status, got status=%q kind=%d", m.transientStatus, m.transientStatusKind)
	}
}
