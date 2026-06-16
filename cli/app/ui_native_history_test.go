package app

import (
	"core/cli/tui"
	"core/server/runtime"
	"core/shared/transcript"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
)

func stripANSIText(v string) string {
	return strings.Join(strings.Fields(xansi.Strip(v)), " ")
}

func stripANSIPreserve(v string) string {
	return xansi.Strip(v)
}

func pendingSpinnerFrameText(frame int) string {
	return pendingToolSpinnerFrame(frame)
}

func pendingSpinnerLine(frame int, text string) string {
	return pendingSpinnerFrameText(frame) + " " + text
}

func collectNativeHistoryFlushText(msgs []tea.Msg) string {
	var out strings.Builder
	for _, msg := range msgs {
		flush, ok := msg.(nativeHistoryFlushMsg)
		if !ok {
			continue
		}
		out.WriteString(stripANSIPreserve(flush.Text))
		out.WriteByte('\n')
	}
	return out.String()
}

func makeStreamingLines(count int) string {
	parts := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		parts = append(parts, fmt.Sprintf("line-%02d", i))
	}
	return strings.Join(parts, "\n")
}

func TestNativeScrollbackStartupReplayIncludesFullTranscript(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{
			{Role: "user", Text: "first message"},
			{Role: "assistant", Text: "last message"},
		}),
	)

	if _, ok := startupCmdMessage[nativeHistoryFlushMsg](m.startupCmds); ok {
		t.Fatal("expected startup native history replay deferred until window size")
	}
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	updated, ok := next.(*uiModel)
	if !ok {
		t.Fatalf("unexpected model type %T", next)
	}
	m = updated
	if cmd == nil {
		t.Fatal("expected native replay command after first window size")
	}
	msg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg after first window size, got %T", cmd())
	}
	plain := stripANSIText(msg.Text)
	if !strings.Contains(plain, "first message") || !strings.Contains(plain, "last message") {
		t.Fatalf("expected startup native replay to include full transcript, got %q", msg.Text)
	}
}

func TestNativeCommittedProjectionCacheRebuildsAfterRevisionBump(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.windowSizeKnown = true
	m.transcriptBaseOffset = 12
	m.transcriptRevision = 20
	entries := []tui.TranscriptEntry{{Role: "assistant", Text: "before"}}

	initial := m.nativeCommittedProjection(entries)
	entries[0].Text = "after"
	sameRevision := m.nativeCommittedProjection(entries)
	if rendered := sameRevision.Render(tui.TranscriptDivider); !strings.Contains(rendered, "before") || strings.Contains(rendered, "after") {
		t.Fatalf("expected same revision to reuse cached native projection, got %q", rendered)
	}

	m.transcriptRevision = 21
	updated := m.nativeCommittedProjection(entries)
	rendered := updated.Render(tui.TranscriptDivider)
	if !strings.Contains(rendered, "after") || strings.Contains(rendered, "before") {
		t.Fatalf("expected revision bump to rebuild native projection, got %q", rendered)
	}
	if len(updated.Blocks) != 1 || updated.Blocks[0].EntryIndex != 12 {
		t.Fatalf("expected native projection to preserve base offset after rebuild, got %#v", updated.Blocks)
	}
	if initial.Render(tui.TranscriptDivider) == rendered {
		t.Fatalf("expected updated projection to differ after revision bump")
	}
}

func TestNativeScrollbackStartupReplayHidesInterruptionInOngoing(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: string(transcript.EntryRoleInterruption), Text: "User interrupted you"}}),
	)

	next, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	updated, ok := next.(*uiModel)
	if !ok {
		t.Fatalf("unexpected model type %T", next)
	}
	m = updated
	if cmd == nil {
		t.Fatal("expected native replay command after first window size")
	}
	msg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg after first window size, got %T", cmd())
	}
	plain := stripANSIText(msg.Text)
	if strings.Contains(plain, "User interrupted you") {
		t.Fatalf("expected native replay to hide model-facing interruption wording, got %q", msg.Text)
	}
	if strings.Contains(plain, "You interrupted") {
		t.Fatalf("expected native replay to hide interruption from ongoing transcript, got %q", msg.Text)
	}
}

func TestNativeScrollbackStartupReplayContinuesPastEmptyToolResult(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.transcriptEntries = []tui.TranscriptEntry{
		{Role: "user", Text: "before tool"},
		{Role: "tool_call", Text: "apply patch", ToolCallID: "call_patch", ToolCall: &transcript.ToolCallMeta{ToolName: "patch"}},
		{Role: "tool_result_ok", Text: "", ToolCallID: "call_patch"},
		{Role: "assistant", Text: "after empty result"},
	}
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})

	next, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected startup replay command")
	}
	msg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg after first window size, got %T", cmd())
	}
	plain := stripANSIText(msg.Text)
	if !strings.Contains(plain, "after empty result") {
		t.Fatalf("expected startup replay to continue past empty tool result, got %q", msg.Text)
	}
	if strings.Contains(plain, "tool_result_ok") {
		t.Fatalf("did not expect empty tool result entry to render, got %q", msg.Text)
	}
}

func TestNativeScrollbackStartupReplayKeepsPatchSuccessStateAfterEmptyToolResult(t *testing.T) {
	m := newProjectedStaticUIModel(WithUITheme("dark"))
	m.transcriptEntries = []tui.TranscriptEntry{
		{Role: "tool_call", Text: "apply patch", ToolCallID: "call_patch", ToolCall: &transcript.ToolCallMeta{ToolName: "patch", Command: "apply patch"}},
		{Role: "tool_result_ok", Text: "", ToolCallID: "call_patch"},
	}
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})

	_, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd == nil {
		t.Fatal("expected startup replay command")
	}
	msg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg after first window size, got %T", cmd())
	}
	plain := stripANSIPreserve(msg.Text)
	if !strings.Contains(plain, "⇄ apply patch") {
		t.Fatalf("expected patch replay to show tool call text, got %q", plain)
	}
}

func TestNativeScrollbackStartupReplayKeepsPatchErrorSymbol(t *testing.T) {
	m := newProjectedStaticUIModel(WithUITheme("dark"))
	m.transcriptEntries = []tui.TranscriptEntry{
		{
			Role:       "tool_call",
			Text:       "Edited: ./main.go +1 -1",
			ToolCallID: "call_patch",
			ToolCall:   &transcript.ToolCallMeta{ToolName: "patch", PatchSummary: "Edited: ./main.go +1 -1", PatchDetail: "Edited:\n./main.go\n-old\n+new"},
		},
		{Role: "tool_result_error", Text: "Patch failed", ToolCallID: "call_patch"},
	}
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})

	_, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd == nil {
		t.Fatal("expected startup replay command")
	}
	msg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg after first window size, got %T", cmd())
	}
	plain := stripANSIPreserve(msg.Text)
	if !strings.Contains(plain, "⇄ ./main.go +1 -1") || strings.Contains(plain, "Edited:") {
		t.Fatalf("expected patch replay to show error patch symbol and summary, got %q", plain)
	}
}

func TestNativeScrollbackStartupReplayKeepsMultiFilePatchHeaderFullStrength(t *testing.T) {
	m := newProjectedStaticUIModel(WithUITheme("dark"))
	summary := "./cli/app/ui_diff_render_test.go +2 -2\n./cli/app/ui_mode_flow_test.go +1 -1"
	m.transcriptEntries = []tui.TranscriptEntry{
		{
			Role:       "tool_call",
			Text:       summary,
			ToolCallID: "call_patch",
			ToolCall:   &transcript.ToolCallMeta{ToolName: "patch", PatchSummary: summary, PatchDetail: summary},
		},
		{Role: "tool_result_ok", Text: "", ToolCallID: "call_patch"},
	}
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})

	_, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd == nil {
		t.Fatal("expected startup replay command")
	}
	msg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg after first window size, got %T", cmd())
	}
	headerLine := lineContaining(msg.Text, "./cli/app/ui_diff_render_test.go")
	if headerLine == "" {
		t.Fatalf("expected native replay patch summary line, got %q", msg.Text)
	}
	if strings.Contains(headerLine, ";2m") {
		t.Fatalf("expected native replay multi-file patch summary to render full-strength, got %q", headerLine)
	}
}

func TestPatchEditedLabelOmittedInLiveViewAndNativeReplay(t *testing.T) {
	m := newProjectedStaticUIModel(WithUITheme("dark"))
	entries := []tui.TranscriptEntry{
		{
			Role:       "tool_call",
			Text:       "Edited: ./single.go +1 -1",
			ToolCallID: "single",
			ToolCall:   &transcript.ToolCallMeta{ToolName: "patch", PatchSummary: "Edited: ./single.go +1 -1", PatchDetail: "Edited:\n./single.go\n-old\n+new"},
		},
		{Role: "tool_result_ok", ToolCallID: "single"},
		{
			Role:       "tool_call",
			Text:       "Edited:\n./a.go +1\n./b.go -1",
			ToolCallID: "multi",
			ToolCall:   &transcript.ToolCallMeta{ToolName: "patch", PatchSummary: "Edited:\n./a.go +1\n./b.go -1", PatchDetail: "Edited:\n./a.go\n+new\n./b.go\n-old"},
		},
		{Role: "tool_result_ok", ToolCallID: "multi"},
		{
			Role:       "tool_call",
			Text:       "Edited:",
			ToolCallID: "raw",
			ToolCall:   &transcript.ToolCallMeta{ToolName: "patch", PatchSummary: "Edited:", PatchDetail: "Edited:\nnot a structured patch payload"},
		},
		{Role: "tool_result_ok", ToolCallID: "raw"},
	}
	m.transcriptEntries = entries
	m.forwardToView(tui.SetConversationMsg{Entries: entries})

	live := stripANSIAndTrimRight(m.view.OngoingSnapshot())
	if strings.Contains(live, "Edited:") || !strings.Contains(live, "⇄ ./single.go +1 -1") || !strings.Contains(live, "./a.go +1") || !strings.Contains(live, "⇄ Patch") {
		t.Fatalf("expected live patch summaries without Edited label, got %q", live)
	}

	_, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd == nil {
		t.Fatal("expected startup replay command")
	}
	msg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", cmd())
	}
	replay := stripANSIPreserve(msg.Text)
	if strings.Contains(replay, "Edited:") || !strings.Contains(replay, "⇄ ./single.go +1 -1") || !strings.Contains(replay, "./a.go +1") || !strings.Contains(replay, "⇄ Patch") {
		t.Fatalf("expected native replay patch summaries without Edited label, got %q", replay)
	}
}

func TestNativeScrollbackStartupEmptyConversationEmitsBlankScreenSpacer(t *testing.T) {
	m := newProjectedStaticUIModel()

	next, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	updated, ok := next.(*uiModel)
	if !ok {
		t.Fatalf("unexpected model type %T", next)
	}
	m = updated
	if cmd == nil {
		t.Fatal("expected blank spacer command after first window size without transcript")
	}
	msg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg after first empty window size, got %T", cmd())
	}
	if !msg.AllowBlank {
		t.Fatal("expected blank spacer replay to allow whitespace-only flushes")
	}
	if got := strings.Count(msg.Text, "\n"); got != 30 {
		t.Fatalf("expected blank spacer to emit one empty screen worth of lines, got %d newlines", got)
	}
	if !m.nativeHistoryReplayed {
		t.Fatal("expected empty-history startup to mark native scrollback as replayed")
	}
	if m.nativeRenderedSnapshot != "" {
		t.Fatalf("expected empty-history startup to keep rendered history snapshot empty, got %q", m.nativeRenderedSnapshot)
	}
	if cmd := m.syncNativeHistoryFromTranscript(); cmd != nil {
		t.Fatalf("expected empty-history replay to emit spacer only once without resize, got %T", cmd())
	}
}

func TestNativeScrollbackEmitsOnlyNewTranscriptLines(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "old line"}}),
	)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.nativeHistoryReplayed = true
	m.nativeFlushedEntryCount = len(m.transcriptEntries)

	if cmd := m.syncNativeHistoryFromTranscript(); cmd != nil {
		t.Fatal("expected no delta command without transcript changes")
	}

	m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: "new line"})
	m.transcriptEntries = append(m.transcriptEntries, tui.TranscriptEntry{Role: "assistant", Text: "new line"})
	cmd := m.syncNativeHistoryFromTranscript()
	if cmd == nil {
		t.Fatal("expected native history delta command after transcript append")
	}
	msg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", cmd())
	}
	plain := stripANSIText(msg.Text)
	if !strings.Contains(plain, "new line") {
		t.Fatalf("expected delta replay to include new line, got %q", msg.Text)
	}
	if strings.Contains(plain, "old line") {
		t.Fatalf("expected delta replay to exclude old history, got %q", msg.Text)
	}
}

func TestNativeScrollbackDoesNotReplaySameSessionNonAppendMutation(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "prompt"}, {Role: "assistant", Text: "old line"}, {Role: "assistant", Text: "tail line"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}

	m.transcriptEntries[1].Text = "mutated line"
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})
	cmd := m.syncNativeHistoryFromTranscript()
	for _, msg := range collectCmdMessages(t, cmd) {
		if _, ok := msg.(nativeHistoryFlushMsg); ok {
			t.Fatalf("did not expect same-session divergence to replay normal-buffer history, got %+v", msg)
		}
	}
	if got := stripANSIText(m.nativeRenderedSnapshot); !strings.Contains(got, "mutated line") || strings.Contains(got, "old line") {
		t.Fatalf("expected rendered baseline rebased without replay, got %q", got)
	}
}

func TestNativeScrollbackRebasesWhenNoSharedPrefixExists(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "old line"}, {Role: "assistant", Text: "tail line"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}

	m.transcriptEntries = []tui.TranscriptEntry{{Role: "user", Text: "fresh root"}, {Role: "assistant", Text: "rewritten tail"}}
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})
	cmd := m.syncNativeHistoryFromTranscript()
	for _, msg := range collectCmdMessages(t, cmd) {
		if _, ok := msg.(nativeHistoryFlushMsg); ok {
			t.Fatalf("did not expect same-session zero-prefix divergence to replay scrollback, got %+v", msg)
		}
	}
	if got := m.nativeRenderedSnapshot; got != m.nativeProjection.Render(tui.TranscriptDivider) {
		t.Fatalf("expected zero-prefix divergence to update rendered snapshot baseline without replay, got %q", got)
	}

	m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: "next answer"})
	m.transcriptEntries = append(m.transcriptEntries, tui.TranscriptEntry{Role: "assistant", Text: "next answer"})
	appendCmd := m.syncNativeHistoryFromTranscript()
	if appendCmd == nil {
		t.Fatal("expected future append to resume after silent zero-prefix rebase")
	}
	appendMsg, ok := appendCmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", appendCmd())
	}
	appendPlain := stripANSIText(appendMsg.Text)
	if !strings.Contains(appendPlain, "next answer") {
		t.Fatalf("expected resumed append to include new assistant turn, got %q", appendPlain)
	}
	if strings.Contains(appendPlain, "fresh root") || strings.Contains(appendPlain, "rewritten tail") {
		t.Fatalf("expected resumed append to exclude already rebuilt history, got %q", appendPlain)
	}
}

func TestNativeScrollbackResizeRebasesFormatterWidth(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "old line"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}
	if m.nativeFormatterWidth != 40 {
		t.Fatalf("expected initial formatter width 40, got %d", m.nativeFormatterWidth)
	}

	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if m.nativeFormatterWidth != 100 {
		t.Fatalf("expected formatter width rebased to 100 after resize, got %d", m.nativeFormatterWidth)
	}

	m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: "new line"})
	m.transcriptEntries = append(m.transcriptEntries, tui.TranscriptEntry{Role: "assistant", Text: "new line"})
	cmd := m.syncNativeHistoryFromTranscript()
	if cmd == nil {
		t.Fatal("expected delta command after append post-resize")
	}
	msg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", cmd())
	}
	plain := stripANSIText(msg.Text)
	if !strings.Contains(plain, "new line") {
		t.Fatalf("expected delta replay to include new entry, got %q", msg.Text)
	}
	if strings.Contains(plain, "old line") {
		t.Fatalf("expected delta replay to exclude previously flushed history, got %q", msg.Text)
	}
}

func TestNativeResizeReplayDebouncedToLatestResize(t *testing.T) {
	previousDebounce := nativeResizeReplayDebounce
	previousNow := nativeResizeReplayNow
	nativeResizeReplayDebounce = time.Millisecond
	now := time.Unix(1, 0)
	nativeResizeReplayNow = func() time.Time { return now }
	t.Cleanup(func() {
		nativeResizeReplayDebounce = previousDebounce
		nativeResizeReplayNow = previousNow
	})

	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "old line"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}

	next, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected debounced resize replay command")
	}
	firstToken := m.nativeResizeReplayToken

	next, cmd = m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	m = next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected debounced resize replay command for later resize")
	}
	secondToken := m.nativeResizeReplayToken
	if secondToken <= firstToken {
		t.Fatalf("expected resize replay token to advance, first=%d second=%d", firstToken, secondToken)
	}

	next, staleCmd := m.Update(nativeResizeReplayMsg{token: firstToken})
	m = next.(*uiModel)
	if staleCmd != nil {
		t.Fatalf("expected stale resize replay token ignored, got %T", staleCmd)
	}

	now = now.Add(500 * time.Microsecond)
	next, replayCmd := m.Update(nativeResizeReplayMsg{token: secondToken})
	m = next.(*uiModel)
	if replayCmd == nil {
		t.Fatal("expected latest resize replay token to stay deferred until quiet period elapses")
	}
	deferredMsgs := collectCmdMessages(t, replayCmd)
	if len(deferredMsgs) != 1 {
		t.Fatalf("expected deferred resize replay to reschedule exactly one timer, got %d message(s)", len(deferredMsgs))
	}
	deferred, ok := deferredMsgs[0].(nativeResizeReplayMsg)
	if !ok {
		t.Fatalf("expected deferred nativeResizeReplayMsg, got %T", deferredMsgs[0])
	}
	if deferred.token != secondToken {
		t.Fatalf("expected deferred resize replay token %d, got %d", secondToken, deferred.token)
	}

	now = now.Add(500 * time.Microsecond)
	next, replayCmd = m.Update(nativeResizeReplayMsg{token: secondToken})
	m = next.(*uiModel)
	if replayCmd == nil {
		t.Fatal("expected latest resize replay token to trigger full history replay after quiet period")
	}
	msgs := collectCmdMessages(t, replayCmd)
	if len(msgs) != 2 {
		t.Fatalf("expected clear-screen plus native history flush for resize replay, got %d message(s)", len(msgs))
	}
	flush, ok := msgs[1].(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg as second replay message, got %T", msgs[1])
	}
	if !strings.Contains(stripANSIText(flush.Text), "old line") {
		t.Fatalf("expected full resize replay to include existing transcript, got %q", flush.Text)
	}
	if m.nativeFormatterWidth != 100 {
		t.Fatalf("expected formatter width rebased to latest resize width 100, got %d", m.nativeFormatterWidth)
	}
}

func TestNativeHeightOnlyResizeDoesNotScheduleFullReplay(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "seed"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}

	next, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(*uiModel)
	if cmd != nil {
		t.Fatalf("expected height-only resize to avoid full native replay scheduling, got %T", cmd)
	}
	if m.nativeResizeReplayToken != 0 {
		t.Fatalf("expected height-only resize to avoid changing replay token, got %d", m.nativeResizeReplayToken)
	}
}

func TestNativeResizeReplayInvalidatedAcrossModeSwitch(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "seed"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}

	next, resizeCmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = next.(*uiModel)
	if resizeCmd == nil {
		t.Fatal("expected debounced resize replay command")
	}
	staleToken := m.nativeResizeReplayToken

	_ = m.toggleTranscriptModeWithNativeReplay(false)
	if m.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected detail mode, got %q", m.view.Mode())
	}
	_ = m.toggleTranscriptModeWithNativeReplay(false)
	if m.view.Mode() != tui.ModeOngoing {
		t.Fatalf("expected ongoing mode, got %q", m.view.Mode())
	}
	if m.nativeResizeReplayToken == staleToken {
		t.Fatalf("expected mode switch to invalidate stale resize replay token %d", staleToken)
	}

	next, staleCmd := m.Update(nativeResizeReplayMsg{token: staleToken})
	m = next.(*uiModel)
	if staleCmd != nil {
		t.Fatalf("expected stale resize replay ignored after mode switch, got %T", staleCmd)
	}
}

func TestNativeStreamingContractViewportDuringStreamCommittedReplayOnFinish(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "prompt once"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}
	if len(m.transcriptEntries) != 1 {
		t.Fatalf("expected one committed transcript entry at start, got %d", len(m.transcriptEntries))
	}

	next, _ := m.Update(projectedRuntimeEventMsg(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "stream line"}))
	updated, ok := next.(*uiModel)
	if !ok {
		t.Fatalf("unexpected model type %T", next)
	}
	m = updated
	if len(m.transcriptEntries) != 1 {
		t.Fatalf("expected streaming not to append committed transcript yet, got %d entries", len(m.transcriptEntries))
	}
	if !strings.Contains(stripANSIPreserve(m.View()), "stream line") {
		t.Fatalf("expected ongoing viewport to show streaming text")
	}

	m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: "stream line\nfinal line"})
	m.transcriptEntries = append(m.transcriptEntries, tui.TranscriptEntry{Role: "assistant", Text: "stream line\nfinal line"})
	commitCmd := m.syncNativeHistoryFromTranscript()
	if commitCmd == nil {
		t.Fatal("expected native replay delta after committed assistant append")
	}
	flush, ok := commitCmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", commitCmd())
	}
	plain := stripANSIText(flush.Text)
	if strings.Count(plain, "stream line") != 1 || strings.Count(plain, "final line") != 1 {
		t.Fatalf("expected committed assistant text appended exactly once on finish, got %q", flush.Text)
	}
}

func TestNativeScrollbackShrinkRebasesWithoutReemittingHistory(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "line one"}, {Role: "assistant", Text: "line two"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}

	m.transcriptEntries = m.transcriptEntries[:1]
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})
	cmd := m.syncNativeHistoryFromTranscript()
	if cmd != nil {
		t.Fatalf("expected no replay emission after transcript shrink, got %T", cmd())
	}
}

func TestNativeScrollbackRepeatedConversationRefreshDoesNotDuplicateUserPrompt(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "prompt once"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}
	startupMsg, ok := startupCmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", startupCmd())
	}
	combined := startupMsg.Text + "\n"

	for i := 0; i < 12; i++ {
		m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})
		if cmd := m.syncNativeHistoryFromTranscript(); cmd != nil {
			t.Fatalf("expected no replay emission on repeated conversation refresh #%d, got %T", i, cmd())
		}
	}

	m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: "tail"})
	m.transcriptEntries = append(m.transcriptEntries, tui.TranscriptEntry{Role: "assistant", Text: "tail"})
	cmd := m.syncNativeHistoryFromTranscript()
	if cmd == nil {
		t.Fatal("expected tail delta command")
	}
	msg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", cmd())
	}
	combined += msg.Text
	plain := stripANSIText(combined)
	if count := strings.Count(plain, "prompt once"); count != 1 {
		t.Fatalf("expected prompt emitted once across repeated refreshes, got %d occurrences", count)
	}
}

func TestNativeScrollbackIncrementalFlushConcatenationMatchesFullSnapshot(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "line 1"}}),
	)
	_, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if startupCmd == nil {
		t.Fatal("expected startup replay command")
	}
	startupMsg, ok := startupCmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", startupCmd())
	}
	combined := startupMsg.Text + "\n"

	appendEntry := func(text string) {
		t.Helper()
		m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: text})
		m.transcriptEntries = append(m.transcriptEntries, tui.TranscriptEntry{Role: "assistant", Text: text})
		cmd := m.syncNativeHistoryFromTranscript()
		if cmd == nil {
			t.Fatalf("expected replay command after append %q", text)
		}
		msg, ok := cmd().(nativeHistoryFlushMsg)
		if !ok {
			t.Fatalf("expected nativeHistoryFlushMsg, got %T", cmd())
		}
		combined += msg.Text + "\n"
	}

	appendEntry("line 2\n\n```yaml\nroot:\n  key: value\n```")
	appendEntry("line 3 with `code`")

	combined = strings.TrimSuffix(combined, "\n")
	expected := renderStyledNativeProjectionLines(tui.ProjectCommittedOngoingTranscript(m.transcriptEntries, m.theme, m.nativeFormatterWidth).Lines(tui.TranscriptDivider), m.theme, m.nativeFormatterWidth)
	if combined != expected {
		t.Fatalf("expected concatenated incremental flush output to match full snapshot\ncombined=%q\nexpected=%q", combined, expected)
	}
}

func TestNativeScrollbackFlowIntegration(t *testing.T) {
	entries := make([]UITranscriptEntry, 0, 120)
	for i := 1; i <= 120; i++ {
		entries = append(entries, UITranscriptEntry{Role: "assistant", Text: fmt.Sprintf("message %d", i)})
	}
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript(entries),
	)
	nextModel, startupCmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	updatedModel, ok := nextModel.(*uiModel)
	if !ok {
		t.Fatalf("unexpected model type %T", nextModel)
	}
	m = updatedModel

	if startupCmd == nil {
		t.Fatal("expected startup replay command after initial window size")
	}
	startupMsg, ok := startupCmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg at startup, got %T", startupCmd())
	}
	startupPlain := stripANSIText(startupMsg.Text)
	if !strings.Contains(startupPlain, "message 1") || !strings.Contains(startupPlain, "message 120") {
		t.Fatalf("expected startup replay to contain earliest and latest entries")
	}
	if _, cmd := m.Update(startupMsg); cmd == nil {
		t.Fatal("expected non-nil command for startup flush")
	}

	modeBefore := m.view.Mode()
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected detail mode after toggle, got %q", m.view.Mode())
	}
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.view.Mode() != modeBefore {
		t.Fatalf("expected ongoing mode after second toggle, got %q", m.view.Mode())
	}

	start := m.view.OngoingScroll()
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if got := m.view.OngoingScroll(); got != start {
		t.Fatalf("expected pgup not to mutate ongoing transcript state, got %d from %d", got, start)
	}

	m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: "message 121"})
	m.transcriptEntries = append(m.transcriptEntries, tui.TranscriptEntry{Role: "assistant", Text: "message 121"})
	deltaCmd := m.syncNativeHistoryFromTranscript()
	if deltaCmd == nil {
		t.Fatal("expected replay delta command after new message")
	}
	deltaMsg, ok := deltaCmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg delta, got %T", deltaCmd())
	}
	deltaPlain := stripANSIText(deltaMsg.Text)
	if !strings.Contains(deltaPlain, "message 121") {
		t.Fatalf("expected delta replay to contain only new tail content, got %q", deltaMsg.Text)
	}
	if _, cmd := m.Update(deltaMsg); cmd == nil {
		t.Fatal("expected non-nil command for delta flush")
	}
}

func TestRenderNativeScrollbackEntriesPreservesMeaningfulWhitespace(t *testing.T) {
	text := "  \tline one\n\tline two\n"
	out := renderStyledNativeProjectionLines(tui.ProjectCommittedOngoingTranscript([]tui.TranscriptEntry{{Role: "assistant", Text: text}}, "dark", 120).Lines(tui.TranscriptDivider), "dark", 120)
	plain := stripANSIPreserve(out)
	if !strings.Contains(plain, "line one") {
		t.Fatalf("expected first line content preserved, got %q", out)
	}
	if !strings.Contains(plain, "line two") {
		t.Fatalf("expected second line content preserved, got %q", out)
	}
}

func TestNativeScrollbackSnapshotPreservesCodeBlockIndentation(t *testing.T) {
	text := "```yaml\nroot:\n  key: value\n```"
	out := renderStyledNativeProjectionLines(tui.ProjectCommittedOngoingTranscript([]tui.TranscriptEntry{{Role: "assistant", Text: text}}, "dark", 100).Lines(tui.TranscriptDivider), "dark", 100)
	plain := stripANSIPreserve(out)
	if !strings.Contains(plain, "root:") || !strings.Contains(plain, "  key: value") {
		t.Fatalf("expected yaml indentation preserved in formatted snapshot, got %q", out)
	}
}

func TestRenderNativeScrollbackSnapshotPreservesToolCallFormatting(t *testing.T) {
	out := renderStyledNativeProjectionLines(tui.ProjectCommittedOngoingTranscript([]tui.TranscriptEntry{
		{
			Role: "tool_call",
			Text: `{"command":"echo hi"}`,
			ToolCall: &transcript.ToolCallMeta{
				ToolName: "shell",
				IsShell:  true,
				Command:  "echo hi",
			},
		},
		{Role: "tool_result_ok", Text: "hi"},
	}, "dark", 100).Lines(tui.TranscriptDivider), "dark", 100)
	plain := stripANSIText(out)
	if !strings.Contains(plain, "echo hi") {
		t.Fatalf("expected tool call command preserved, got %q", out)
	}
	if !strings.Contains(plain, "hi") {
		t.Fatalf("expected tool result preserved, got %q", out)
	}
}

func TestStyleNativeReplayDividersKeepsRawRuleLikeLinesAsContent(t *testing.T) {
	out := styleNativeReplayDividers("───\nbody", "dark", 10)
	lines := strings.Split(stripANSIPreserve(out), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected two lines, got %q", out)
	}
	if lines[0] != "───" {
		t.Fatalf("expected raw divider-like content preserved, got %q", lines[0])
	}
}

func TestRenderNativeScrollbackSnapshotPreservesAskQuestionStructuredAnswerText(t *testing.T) {
	out := renderStyledNativeProjectionLines(tui.ProjectCommittedOngoingTranscript([]tui.TranscriptEntry{
		{Role: "tool_call", Text: "Choose scope?", ToolCallID: "call_ask", ToolCall: &transcript.ToolCallMeta{ToolName: "ask_question", Question: "Choose scope?", Suggestions: []string{"full", "Fast only"}, RecommendedOptionIndex: 1}},
		{Role: "tool_result_ok", Text: "ask result summary", ToolCallID: "call_ask"},
	}, "dark", 100).Lines(tui.TranscriptDivider), "dark", 100)
	plain := stripANSIText(out)
	if !strings.Contains(plain, "Choose scope?") {
		t.Fatalf("expected ask question preserved, got %q", out)
	}
	if !strings.Contains(plain, "ask result summary") {
		t.Fatalf("expected ask result text preserved, got %q", out)
	}
	if strings.Contains(plain, "full") || strings.Contains(plain, "Fast only") {
		t.Fatalf("expected native ongoing snapshot to omit ask suggestions, got %q", out)
	}
}

func TestNativeCommittedEntriesStopsAtFirstUnresolvedToolCall(t *testing.T) {
	entries := []tui.TranscriptEntry{
		{Role: "user", Text: "prompt"},
		{Role: "tool_call", Text: "echo a", ToolCallID: "call_a", ToolCall: &transcript.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo a"}},
		{Role: "tool_call", Text: "echo b", ToolCallID: "call_b", ToolCall: &transcript.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo b"}},
		{Role: "tool_result_ok", Text: "out-b", ToolCallID: "call_b"},
	}

	committed := committedTranscriptEntriesForApp(entries)
	if len(committed) != 1 || committed[0].Text != "prompt" {
		t.Fatalf("expected only stable prefix committed, got %#v", committed)
	}
	pending := tui.PendingOngoingEntries(entries)
	if len(pending) != 3 {
		t.Fatalf("expected unresolved tool tail to stay pending, got %d entries", len(pending))
	}

	entries = append(entries, tui.TranscriptEntry{Role: "tool_result_ok", Text: "out-a", ToolCallID: "call_a"})
	committed = committedTranscriptEntriesForApp(entries)
	if len(committed) != len(entries) {
		t.Fatalf("expected full transcript committed once first unresolved call completes, got %d of %d entries", len(committed), len(entries))
	}
}
