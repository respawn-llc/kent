package app

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"builder/cli/tui"
)

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

func TestEmitForcedNativeProjectionReplayUpdatesRenderedProjection(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.windowSizeKnown = true
	m.nativeProjectionBaseOffset = 7
	projection := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{EntryIndex: 7, EntryEnd: 7, DividerGroup: "assistant", Lines: []string{"hello"}},
	}}

	cmd := m.emitForcedNativeProjectionReplay(projection)
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
	if !reflect.DeepEqual(m.nativeRenderedProjection, projection) {
		t.Fatalf("rendered projection = %+v, want %+v", m.nativeRenderedProjection, projection)
	}
	if m.nativeRenderedBaseOffset != 7 {
		t.Fatalf("rendered base offset = %d, want 7", m.nativeRenderedBaseOffset)
	}
	if m.nativeRenderedSnapshot != projection.Render(tui.TranscriptDivider) {
		t.Fatalf("rendered snapshot = %q, want raw projection snapshot", m.nativeRenderedSnapshot)
	}
}

func TestNativeHistoryFlushBuffersPendingSequencesInOrder(t *testing.T) {
	m := newProjectedStaticUIModel()

	if cmd := m.handleNativeHistoryFlush(nativeHistoryFlushMsg{Text: "third", Sequence: 3}); cmd != nil {
		t.Fatalf("out-of-order flush cmd = %v, want nil", cmd)
	}
	if m.nativeFlushedSequence != 0 || len(m.nativePendingFlushes) != 1 {
		t.Fatalf("after seq3 flushed=%d pending=%d, want 0/1", m.nativeFlushedSequence, len(m.nativePendingFlushes))
	}

	_ = m.handleNativeHistoryFlush(nativeHistoryFlushMsg{Text: "first", Sequence: 1})
	if m.nativeFlushedSequence != 1 || len(m.nativePendingFlushes) != 1 {
		t.Fatalf("after seq1 flushed=%d pending=%d, want 1/1", m.nativeFlushedSequence, len(m.nativePendingFlushes))
	}

	_ = m.handleNativeHistoryFlush(nativeHistoryFlushMsg{Text: "second", Sequence: 2})
	if m.nativeFlushedSequence != 3 {
		t.Fatalf("after seq2 flushed=%d, want pending seq3 drained", m.nativeFlushedSequence)
	}
	if len(m.nativePendingFlushes) != 0 {
		t.Fatalf("pending flushes = %d, want drained", len(m.nativePendingFlushes))
	}
}

func TestNativeHistoryFlushClearBelowPrefixesPrintedText(t *testing.T) {
	m := newProjectedStaticUIModel()

	msgs := collectCmdMessages(t, m.handleNativeHistoryFlush(nativeHistoryFlushMsg{
		Text:             "committed tail",
		ClearBelowBefore: true,
		Sequence:         1,
	}))
	if len(msgs) != 1 {
		t.Fatalf("flush messages = %d, want 1", len(msgs))
	}
	printed := fmt.Sprintf("%+v", msgs[0])
	if !strings.Contains(printed, "\x1b[Jcommitted tail") {
		t.Fatalf("expected clear-below CSI before printed text, got %#v", msgs[0])
	}
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
