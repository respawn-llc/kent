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

func TestCommittedTranscriptSuffixReturnsNewestSegment(t *testing.T) {
	eng := newRuntimeViewTranscriptSuffixEngine(t, 5)

	suffix := mustRuntimeSuffix(t, eng)

	if suffix.SessionID != eng.SessionID() || suffix.SessionName != eng.SessionName() {
		t.Fatalf("unexpected session metadata: %+v", suffix)
	}
	if got := len(suffix.Entries); got != 5 {
		t.Fatalf("entries = %d, want 5 (whole newest segment)", got)
	}
	if suffix.Entries[0].Text != "reply-000" || suffix.Entries[4].Text != "reply-004" {
		t.Fatalf("unexpected suffix entries: %+v", suffix.Entries)
	}
}

func TestCommittedTranscriptSuffixEmptySession(t *testing.T) {
	eng := newRuntimeViewTranscriptSuffixEngine(t, 0)

	suffix := mustRuntimeSuffix(t, eng)

	if len(suffix.Entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(suffix.Entries))
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
	eng.AppendCommittedEntryWithVisibility("developer_context", "internal note", transcript.EntryVisibilityVerbose)

	suffix := mustRuntimeSuffix(t, eng)

	if got := len(suffix.Entries); got != 1 {
		t.Fatalf("entries = %d, want 1", got)
	}
	entry := suffix.Entries[0]
	if entry.Role != "developer_context" || entry.Text != "internal note" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	if entry.Visibility != clientui.EntryVisibilityVerbose {
		t.Fatalf("visibility = %q, want %q", entry.Visibility, clientui.EntryVisibilityVerbose)
	}
	entry.Text = "mutated"
	second := mustRuntimeSuffix(t, eng)
	if second.Entries[0].Text != "internal note" {
		t.Fatalf("suffix entries were not cloned: %+v", second.Entries[0])
	}
}

func TestCommittedTranscriptSuffixFromSegmentUsesCommittedEntryCountBase(t *testing.T) {
	segment := runtime.TranscriptSegmentPage{
		CommittedEntryCountBase: 40,
		Snapshot: runtime.ChatSnapshot{
			Entries: []runtime.ChatEntry{
				{Role: "assistant", Text: "r1"},
				{Role: "assistant", Text: "r2"},
			},
		},
	}
	suffix := CommittedTranscriptSuffixFromSegment("sid", "name", clientui.ConversationFreshnessEstablished, 7, segment)
	if suffix.StartEntryCount != 40 {
		t.Fatalf("StartEntryCount = %d, want segment base 40", suffix.StartEntryCount)
	}
	if suffix.CommittedEntryCount != 42 || suffix.NextEntryCount != 42 {
		t.Fatalf("CommittedEntryCount/NextEntryCount = %d/%d, want 42/42 (base+len)", suffix.CommittedEntryCount, suffix.NextEntryCount)
	}
}

func mustRuntimeSuffix(t *testing.T, eng *runtime.Engine) clientui.CommittedTranscriptSuffix {
	t.Helper()
	suffix, err := CommittedTranscriptSuffixFromRuntime(eng, clientui.CommittedTranscriptSuffixRequest{})
	if err != nil {
		t.Fatalf("committed transcript suffix: %v", err)
	}
	return suffix
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
