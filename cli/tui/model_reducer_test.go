package tui

import (
	"testing"

	"builder/shared/transcript"
)

func TestReduceAppendTranscriptMsgReportsMutationFlagsAndNormalizesEntry(t *testing.T) {
	m := NewModel()
	var result modelUpdateResult
	hint := &transcript.ToolCallMeta{ToolName: "shell", Command: "pwd"}

	m.reduceAppendTranscriptMsg(AppendTranscriptMsg{Role: "  ", Text: "hello", ToolCallID: " call_1 ", ToolCall: hint}, &result)

	if !result.autoFollowOngoing || !result.ongoingChanged || !result.detailChanged {
		t.Fatalf("expected append transcript reducer to mark transcript refresh flags, got %+v", result)
	}
	if len(m.transcriptInput.Entries) != 1 {
		t.Fatalf("expected one transcript entry, got %d", len(m.transcriptInput.Entries))
	}
	entry := m.transcriptInput.Entries[0]
	if entry.Role != "unknown" {
		t.Fatalf("expected empty role to normalize to unknown, got %q", entry.Role)
	}
	if entry.ToolCallID != "call_1" {
		t.Fatalf("expected trimmed tool call id, got %q", entry.ToolCallID)
	}
	if entry.ToolCall == nil || entry.ToolCall == hint {
		t.Fatalf("expected cloned tool call metadata, got %#v", entry.ToolCall)
	}
}

func TestReduceAppendTranscriptMsgAdvancesTotalEntriesFromTailWindow(t *testing.T) {
	m := NewModel()
	m.transcriptInput.BaseOffset = 250
	m.transcriptInput.TotalEntries = 252
	m.transcriptInput.Entries = []TranscriptEntry{{Role: "assistant", Text: "existing"}, {Role: "assistant", Text: "tail"}}
	var result modelUpdateResult

	m.reduceAppendTranscriptMsg(AppendTranscriptMsg{Role: "assistant", Text: "new tail"}, &result)

	if got, want := m.transcriptInput.TotalEntries, 253; got != want {
		t.Fatalf("transcriptTotalEntries = %d, want %d", got, want)
	}
}

func TestReduceCommitAssistantMsgAdvancesTotalEntriesFromTailWindow(t *testing.T) {
	m := NewModel()
	m.transcriptInput.BaseOffset = 250
	m.transcriptInput.TotalEntries = 252
	m.transcriptInput.Entries = []TranscriptEntry{{Role: "assistant", Text: "existing"}, {Role: "assistant", Text: "tail"}}
	m.transcriptInput.Ongoing = "new tail"
	var result modelUpdateResult

	m.reduceCommitAssistantMsg(&result)

	if got, want := m.transcriptInput.TotalEntries, 253; got != want {
		t.Fatalf("transcriptTotalEntries = %d, want %d", got, want)
	}
	if !result.autoFollowOngoing || !result.ongoingBaseChanged || !result.ongoingChanged || !result.detailChanged {
		t.Fatalf("expected commit assistant reducer to mark transcript refresh flags, got %+v", result)
	}
}

func TestReduceSetConversationMsgNormalizesEntriesAndClearsInvalidSelection(t *testing.T) {
	m := NewModel()
	m.selectedTranscriptEntry = 5
	m.selectedTranscriptActive = true
	var result modelUpdateResult

	m.reduceSetConversationMsg(SetConversationMsg{
		Entries:      []TranscriptEntry{{Role: "assistant", Text: "a", ToolCallID: " call_a ", ToolCall: &transcript.ToolCallMeta{ToolName: "shell"}}},
		Ongoing:      "stream",
		OngoingError: "  err  ",
	}, &result)

	if !result.autoFollowOngoing || !result.ongoingChanged || !result.detailChanged {
		t.Fatalf("expected set conversation reducer to mark transcript refresh flags, got %+v", result)
	}
	if len(m.transcriptInput.Entries) != 1 {
		t.Fatalf("expected conversation to replace transcript entries, got %d", len(m.transcriptInput.Entries))
	}
	if m.transcriptInput.Entries[0].ToolCallID != "call_a" {
		t.Fatalf("expected trimmed tool call id, got %q", m.transcriptInput.Entries[0].ToolCallID)
	}
	if m.transcriptInput.Ongoing != "stream" {
		t.Fatalf("expected ongoing text replacement, got %q", m.transcriptInput.Ongoing)
	}
	if m.ongoingError != "err" {
		t.Fatalf("expected trimmed ongoing error, got %q", m.ongoingError)
	}
	if m.selectedTranscriptActive {
		t.Fatal("expected invalid transcript selection to be cleared")
	}
}

func TestApplyUpdateResultAutoFollowsOngoingAtBottom(t *testing.T) {
	m := NewModel(WithPreviewLines(2))
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "one"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "two"})
	m = updateModel(t, m, AppendTranscriptMsg{Role: "assistant", Text: "three"})
	if got, want := m.ongoingScroll, m.maxOngoingScroll(); got != want {
		t.Fatalf("expected setup at bottom, got %d want %d", got, want)
	}

	m.transcriptInput.Ongoing = ""
	m.applyUpdateResult(modelUpdateResult{autoFollowOngoing: true, ongoingChanged: true}, true)

	if got, want := m.ongoingScroll, m.maxOngoingScroll(); got != want {
		t.Fatalf("expected auto follow to keep ongoing at bottom, got %d want %d", got, want)
	}
}

func TestReduceSetViewportSizeMsgNoopWhenSizeUnchanged(t *testing.T) {
	m := NewModel()
	m.viewportLines = 20
	m.viewportWidth = 80
	revisionBefore := m.transcriptInput.Revision

	next, _ := m.Update(SetViewportSizeMsg{Lines: 20, Width: 80})
	updated := next.(Model)

	if updated.viewportLines != 20 || updated.viewportWidth != 80 {
		t.Fatalf("expected viewport to remain unchanged, got lines=%d width=%d", updated.viewportLines, updated.viewportWidth)
	}
	if updated.transcriptInput.Revision != revisionBefore {
		t.Fatalf("expected unchanged viewport update to keep canonical state revision stable, got %d want %d", updated.transcriptInput.Revision, revisionBefore)
	}
}

func TestReduceStreamAssistantMsgAdvancesProjectionRevision(t *testing.T) {
	m := NewModel()
	before := m.transcriptInput.Revision

	var first modelUpdateResult
	m.reduceStreamAssistantMsg(StreamAssistantMsg{Delta: "a"}, &first)
	if m.transcriptInput.Revision <= before {
		t.Fatalf("expected streaming delta to advance projection revision, before=%d after=%d", before, m.transcriptInput.Revision)
	}
}
