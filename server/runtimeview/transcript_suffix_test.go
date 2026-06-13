package runtimeview

import (
	"fmt"
	"testing"

	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	"core/server/tools"
	"core/shared/clientui"
	"core/shared/transcript"
)

func TestCommittedTranscriptSuffixReturnsRowsAfterCursor(t *testing.T) {
	eng := newRuntimeViewTranscriptSuffixEngine(t, 5)

	suffix := CommittedTranscriptSuffixFromRuntime(eng, clientui.CommittedTranscriptSuffixRequest{AfterEntryCount: 2, Limit: 2})

	if suffix.SessionID != eng.SessionID() || suffix.SessionName != eng.SessionName() {
		t.Fatalf("unexpected session metadata: %+v", suffix)
	}
	if suffix.StartEntryCount != 2 {
		t.Fatalf("start entry count = %d, want 2", suffix.StartEntryCount)
	}
	if suffix.NextEntryCount != 4 {
		t.Fatalf("next entry count = %d, want 4", suffix.NextEntryCount)
	}
	if suffix.CommittedEntryCount != 5 {
		t.Fatalf("committed entry count = %d, want 5", suffix.CommittedEntryCount)
	}
	if !suffix.HasMore {
		t.Fatalf("expected has_more for bounded suffix, got %+v", suffix)
	}
	if got := len(suffix.Entries); got != 2 {
		t.Fatalf("entries = %d, want 2", got)
	}
	if suffix.Entries[0].Text != "reply-002" || suffix.Entries[1].Text != "reply-003" {
		t.Fatalf("unexpected suffix entries: %+v", suffix.Entries)
	}
}

func TestCommittedTranscriptSuffixReturnsEmptyAtTail(t *testing.T) {
	eng := newRuntimeViewTranscriptSuffixEngine(t, 3)

	suffix := CommittedTranscriptSuffixFromRuntime(eng, clientui.CommittedTranscriptSuffixRequest{AfterEntryCount: 3, Limit: 10})

	if suffix.StartEntryCount != 3 || suffix.NextEntryCount != 3 {
		t.Fatalf("unexpected empty cursor metadata: %+v", suffix)
	}
	if suffix.CommittedEntryCount != 3 {
		t.Fatalf("committed entry count = %d, want 3", suffix.CommittedEntryCount)
	}
	if suffix.HasMore {
		t.Fatalf("did not expect has_more at tail: %+v", suffix)
	}
	if len(suffix.Entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(suffix.Entries))
	}
}

func TestCommittedTranscriptSuffixHonorsLimit(t *testing.T) {
	eng := newRuntimeViewTranscriptSuffixEngine(t, 8)

	suffix := CommittedTranscriptSuffixFromRuntime(eng, clientui.CommittedTranscriptSuffixRequest{AfterEntryCount: 1, Limit: 3})

	if got := len(suffix.Entries); got != 3 {
		t.Fatalf("entries = %d, want 3", got)
	}
	if suffix.NextEntryCount != 4 {
		t.Fatalf("next entry count = %d, want 4", suffix.NextEntryCount)
	}
	if !suffix.HasMore {
		t.Fatalf("expected has_more when limit cuts suffix: %+v", suffix)
	}
}

func TestCommittedTranscriptSuffixClampsInconsistentBaseOffset(t *testing.T) {
	suffix := CommittedTranscriptSuffixFromCollectedChat(
		"session-1",
		"session",
		clientui.ConversationFreshnessEstablished,
		12,
		clientui.ChatSnapshot{Entries: []clientui.ChatEntry{{Role: "assistant", Text: "stale row"}}},
		1,
		3,
		clientui.CommittedTranscriptSuffixRequest{AfterEntryCount: 0, Limit: 10},
	)

	if suffix.StartEntryCount != 1 || suffix.NextEntryCount != 1 {
		t.Fatalf("expected clamped cursor at committed total, got %+v", suffix)
	}
	if suffix.HasMore {
		t.Fatalf("did not expect has_more after clamping inconsistent cursor: %+v", suffix)
	}
	if len(suffix.Entries) != 0 {
		t.Fatalf("expected no entries beyond committed total, got %+v", suffix.Entries)
	}
}

func TestCommittedTranscriptSuffixPreservesEntryMetadata(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	eng, err := runtime.New(store, projectionFastClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	eng.AppendLocalEntryWithVisibility("developer_context", "internal note", transcript.EntryVisibilityDetailOnly)

	suffix := CommittedTranscriptSuffixFromRuntime(eng, clientui.CommittedTranscriptSuffixRequest{AfterEntryCount: 0, Limit: 1})

	if got := len(suffix.Entries); got != 1 {
		t.Fatalf("entries = %d, want 1", got)
	}
	entry := suffix.Entries[0]
	if entry.Role != "developer_context" || entry.Text != "internal note" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	if entry.Visibility != clientui.EntryVisibilityDetailOnly {
		t.Fatalf("visibility = %q, want %q", entry.Visibility, clientui.EntryVisibilityDetailOnly)
	}
	entry.Text = "mutated"
	second := CommittedTranscriptSuffixFromRuntime(eng, clientui.CommittedTranscriptSuffixRequest{AfterEntryCount: 0, Limit: 1})
	if second.Entries[0].Text != "internal note" {
		t.Fatalf("suffix entries were not cloned: %+v", second.Entries[0])
	}
}

func newRuntimeViewTranscriptSuffixEngine(t *testing.T, count int) *runtime.Engine {
	t.Helper()
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	for i := 0; i < count; i++ {
		if _, _, err := store.AppendEvent("step-1", "message", llm.Message{Role: llm.RoleAssistant, Content: fmt.Sprintf("reply-%03d", i), Phase: llm.MessagePhaseFinal}); err != nil {
			t.Fatalf("append message %d: %v", i, err)
		}
	}
	eng, err := runtime.New(store, projectionFastClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	return eng
}
