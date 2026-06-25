package app

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"core/cli/app/internal/nativescrollback"
	"core/cli/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func collectCmdMessagesApplyingNativeWriteResults(t *testing.T, m *uiModel, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	msgs := collectCmdMessages(t, cmd)
	for idx := 0; idx < len(msgs); idx++ {
		flush, ok := msgs[idx].(nativeHistoryFlushMsg)
		if ok {
			msgs = append(msgs, collectCmdMessages(t, m.handleNativeHistoryFlush(flush))...)
			continue
		}
		writeResult, ok := msgs[idx].(nativeTerminalWriteResultMsg)
		if !ok {
			continue
		}
		msgs = append(msgs, collectCmdMessages(t, m.handleNativeTerminalWriteResult(writeResult.Result))...)
	}
	return msgs
}

func TestNativeProjectionOverlapMatchesRenderedFrontier(t *testing.T) {
	rendered := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{EntryIndex: 0, EntryEnd: 0, DividerGroup: "user", Lines: []string{"one"}},
		{EntryIndex: 1, EntryEnd: 2, DividerGroup: "assistant", Lines: []string{"two"}},
	}}
	current := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{EntryIndex: 0, EntryEnd: 0, DividerGroup: "user", Lines: []string{"one"}},
		{EntryIndex: 1, EntryEnd: 2, DividerGroup: "assistant", Lines: []string{"two"}},
		{EntryIndex: 3, EntryEnd: 3, DividerGroup: "user", Lines: []string{"three"}},
	}}

	frontier, ok := nativeProjectionRenderedFrontier(rendered)
	if !ok || frontier != 2 {
		t.Fatalf("frontier = %d ok=%v, want 2 true", frontier, ok)
	}
	if got := nativeProjectionFirstBlockAfterEntry(current, frontier); got != 2 {
		t.Fatalf("first block after frontier = %d, want 2", got)
	}
	if !nativeProjectionOverlapMatchesRendered(current, rendered, frontier) {
		t.Fatal("expected rendered overlap to match current projection")
	}

	current.Blocks[1].Lines[0] = "mutated"
	if nativeProjectionOverlapMatchesRendered(current, rendered, frontier) {
		t.Fatal("expected mutated overlap to fail")
	}
}

func TestContinuityRecoveryProjectionReplayUpdatesRenderedProjection(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.windowSizeKnown = true
	projection := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{EntryIndex: 7, EntryEnd: 7, DividerGroup: "assistant", Lines: []string{"hello"}},
	}}
	setNativeCurrentProjectionForTest(m, projection, 7, 1)

	cmd := m.emitNonContiguousNativeProjectionRecovery(projection, tui.TranscriptProjection{})
	if cmd == nil {
		t.Fatal("expected replay command")
	}
	msgs := collectCmdMessages(t, cmd)
	if len(msgs) != 2 {
		t.Fatalf("replay messages = %d, want clear plus flush", len(msgs))
	}
	if _, ok := msgs[1].(nativeHistoryFlushMsg); !ok {
		t.Fatalf("second replay msg = %T, want nativeHistoryFlushMsg", msgs[1])
	}
	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, cmd)
	if !reflect.DeepEqual(m.nativeRenderedProjection(), projection) {
		t.Fatalf("rendered projection = %+v, want %+v", m.nativeRenderedProjection(), projection)
	}
	if m.nativeRenderedProjectionBaseOffset() != 7 {
		t.Fatalf("rendered base offset = %d, want 7", m.nativeRenderedProjectionBaseOffset())
	}
	if m.nativeRenderedSnapshot() != projection.Render(tui.TranscriptDivider) {
		t.Fatalf("rendered snapshot = %q, want raw projection snapshot", m.nativeRenderedSnapshot())
	}
}

func TestNativeHistoryFlushBuffersPendingSequencesInOrder(t *testing.T) {
	m := newProjectedStaticUIModel()
	first := nativeHistoryFlushForTest(t, m, "first", nativescrollback.FlushOptions{})
	second := nativeHistoryFlushForTest(t, m, "second", nativescrollback.FlushOptions{})
	third := nativeHistoryFlushForTest(t, m, "third", nativescrollback.FlushOptions{})

	if cmd := m.handleNativeHistoryFlush(third); cmd != nil {
		t.Fatalf("out-of-order flush cmd = %v, want nil", cmd)
	}
	if m.nativeAckedFlushSequence() != 0 || m.nativeScrollbackLedger.PendingCount() != 1 {
		t.Fatalf("after seq3 flushed=%d pending=%d, want 0/1", m.nativeAckedFlushSequence(), m.nativeScrollbackLedger.PendingCount())
	}

	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, m.handleNativeHistoryFlush(first))
	if m.nativeAckedFlushSequence() != 1 || m.nativeScrollbackLedger.PendingCount() != 1 {
		t.Fatalf("after seq1 ack flushed=%d pending=%d, want 1/1", m.nativeAckedFlushSequence(), m.nativeScrollbackLedger.PendingCount())
	}

	_ = collectCmdMessagesApplyingNativeWriteResults(t, m, m.handleNativeHistoryFlush(second))
	if m.nativeAckedFlushSequence() != 3 {
		t.Fatalf("after seq2 flushed=%d, want pending seq3 drained", m.nativeAckedFlushSequence())
	}
	if m.nativeScrollbackLedger.PendingCount() != 0 {
		t.Fatalf("pending flushes = %d, want drained", m.nativeScrollbackLedger.PendingCount())
	}
}

func TestNativeHistoryFlushClearBelowPrefixesPrintedText(t *testing.T) {
	m := newProjectedStaticUIModel()

	flush := nativeHistoryFlushForTest(t, m, "committed tail", nativescrollback.FlushOptions{ClearBelowBefore: true})
	msgs := collectCmdMessages(t, m.handleNativeHistoryFlush(flush))
	printed := ""
	for _, msg := range msgs {
		if _, ok := msg.(nativeTerminalWriteResultMsg); ok {
			continue
		}
		printed += fmt.Sprintf("%+v", msg)
	}
	if !strings.Contains(printed, "\x1b[Jcommitted tail") {
		t.Fatalf("expected clear-below CSI before printed text, got %#v", msgs[0])
	}
}

func nativeHistoryFlushForTest(t *testing.T, m *uiModel, text string, opts nativescrollback.FlushOptions) nativeHistoryFlushMsg {
	t.Helper()
	flush, ok := m.nativeScrollbackLedger.Enqueue(text, opts)
	if !ok {
		t.Fatalf("expected native flush for %q", text)
	}
	return nativeHistoryFlushMsg{Flush: flush}
}

func TestSplitNativeScrollbackChunksKeepsLineBoundaries(t *testing.T) {
	chunks := splitNativeScrollbackChunks("aaa\nbbb\nccc", 7)
	want := []string{"aaa\nbbb", "ccc"}
	if !reflect.DeepEqual(chunks, want) {
		t.Fatalf("chunks = %+v, want %+v", chunks, want)
	}
	if got := splitNativeScrollbackChunks(" \n\t", 7); got != nil {
		t.Fatalf("blank chunks = %+v, want nil", got)
	}
}

func TestSplitNativeScrollbackChunksSplitsLongUnbrokenLinesWithinFrameLimit(t *testing.T) {
	longLine := strings.Repeat("x", nativescrollback.TerminalWriteMaxPayload+7)
	chunks := splitNativeScrollbackChunks(longLine, nativescrollback.TerminalWriteMaxPayload)
	if len(chunks) != 2 {
		t.Fatalf("chunk count = %d, want 2", len(chunks))
	}
	for idx, chunk := range chunks {
		if len(chunk) > nativescrollback.TerminalWriteMaxPayload {
			t.Fatalf("chunk %d len = %d, exceeds terminal payload limit", idx, len(chunk))
		}
	}
	if got := strings.Join(chunks, ""); got != longLine {
		t.Fatalf("rejoined long line changed: len=%d want=%d", len(got), len(longLine))
	}
}
