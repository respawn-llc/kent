package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewLazyDoesNotPersistUntilFirstWrite(t *testing.T) {
	root := t.TempDir()
	store, err := NewLazy(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("new lazy store: %v", err)
	}
	if _, err := os.Stat(store.Dir()); !os.IsNotExist(err) {
		t.Fatalf("expected no session dir before first write, stat err=%v", err)
	}

	if _, err := store.AppendEvent("step1", "message", map[string]any{"a": 1}); err != nil {
		t.Fatalf("append event: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.Dir(), sessionFile)); err != nil {
		t.Fatalf("expected session metadata after first write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.Dir(), eventsFile)); err != nil {
		t.Fatalf("expected events file after first write: %v", err)
	}
}

func TestNewLazyReadEventsBeforePersistReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	store, err := NewLazy(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("new lazy store: %v", err)
	}
	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events len = %d, want 0", len(events))
	}
}

func TestBackfillLockedContextBudgetWithoutLockedContractDoesNotPersistLazyStore(t *testing.T) {
	root := t.TempDir()
	store, err := NewLazy(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("new lazy store: %v", err)
	}

	if err := store.BackfillLockedContextBudget(1000, 50); err != nil {
		t.Fatalf("BackfillLockedContextBudget: %v", err)
	}
	if _, err := os.Stat(store.Dir()); !os.IsNotExist(err) {
		t.Fatalf("expected no session dir after no-op backfill, stat err=%v", err)
	}
}

func TestAppendEventMonotonicSequence(t *testing.T) {
	root := t.TempDir()
	store, err := Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	e1, err := store.AppendEvent("step1", "message", map[string]any{"a": 1})
	if err != nil {
		t.Fatalf("append event1: %v", err)
	}
	e2, err := store.AppendEvent("step1", "message", map[string]any{"b": 2})
	if err != nil {
		t.Fatalf("append event2: %v", err)
	}

	if e1.Seq != 1 || e2.Seq != 2 {
		t.Fatalf("unexpected sequence values: %d, %d", e1.Seq, e2.Seq)
	}

	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
	if events[0].Seq != 1 || events[1].Seq != 2 {
		t.Fatalf("persisted sequence mismatch: %+v", events)
	}
}

func TestReadPromptHistoryFallsBackToVisibleUserMessages(t *testing.T) {
	root := t.TempDir()
	store, err := Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if _, err := store.AppendEvent("s1", "message", map[string]any{"role": "user", "content": "first\nline"}); err != nil {
		t.Fatalf("append first user message: %v", err)
	}
	if _, err := store.AppendEvent("s1", "message", map[string]any{"role": "assistant", "content": "ignored"}); err != nil {
		t.Fatalf("append assistant message: %v", err)
	}
	if _, err := store.AppendEvent("s2", "message", map[string]any{"role": "user", "content": "second"}); err != nil {
		t.Fatalf("append second user message: %v", err)
	}

	history, err := store.ReadPromptHistory()
	if err != nil {
		t.Fatalf("read prompt history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 prompt history entries, got %d", len(history))
	}
	if history[0] != "first\nline" || history[1] != "second" {
		t.Fatalf("unexpected prompt history: %+v", history)
	}
}

func TestReadPromptHistoryUsesExplicitPromptHistoryEvents(t *testing.T) {
	root := t.TempDir()
	store, err := Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if _, err := store.AppendEvent("", "prompt_history", map[string]any{"text": "/resume"}); err != nil {
		t.Fatalf("append slash command history: %v", err)
	}
	if _, err := store.AppendEvent("s1", "message", map[string]any{"role": "user", "content": "plain user message"}); err != nil {
		t.Fatalf("append user message: %v", err)
	}
	if _, err := store.AppendEvent("", "prompt_history", map[string]any{"text": "plain user message"}); err != nil {
		t.Fatalf("append explicit user history: %v", err)
	}

	history, err := store.ReadPromptHistory()
	if err != nil {
		t.Fatalf("read prompt history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 explicit prompt history entries, got %d", len(history))
	}
	if history[0] != "/resume" || history[1] != "plain user message" {
		t.Fatalf("unexpected prompt history: %+v", history)
	}
}

func TestReadPromptHistoryKeepsLegacyEntriesBeforeFirstExplicitEvent(t *testing.T) {
	root := t.TempDir()
	store, err := Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if _, err := store.AppendEvent("s1", "message", map[string]any{"role": "user", "content": "legacy one"}); err != nil {
		t.Fatalf("append legacy one: %v", err)
	}
	if _, err := store.AppendEvent("s2", "message", map[string]any{"role": "user", "content": "legacy two"}); err != nil {
		t.Fatalf("append legacy two: %v", err)
	}
	if _, err := store.AppendEvent("", "prompt_history", map[string]any{"text": "/resume"}); err != nil {
		t.Fatalf("append explicit history: %v", err)
	}
	if _, err := store.AppendEvent("s3", "message", map[string]any{"role": "user", "content": "expanded later user message"}); err != nil {
		t.Fatalf("append post-upgrade user message: %v", err)
	}

	history, err := store.ReadPromptHistory()
	if err != nil {
		t.Fatalf("read prompt history: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("expected 3 history entries, got %d", len(history))
	}
	if history[0] != "legacy one" || history[1] != "legacy two" || history[2] != "/resume" {
		t.Fatalf("unexpected prompt history: %+v", history)
	}
}

func TestReadPromptHistoryPreservesExactStoredText(t *testing.T) {
	root := t.TempDir()
	store, err := Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	want := "  line one\nline two  "
	if _, err := store.AppendEvent("", "prompt_history", map[string]any{"text": want}); err != nil {
		t.Fatalf("append prompt history: %v", err)
	}

	history, err := store.ReadPromptHistory()
	if err != nil {
		t.Fatalf("read prompt history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}
	if history[0] != want {
		t.Fatalf("expected exact stored prompt text, got %q want %q", history[0], want)
	}
}

func TestSetInputDraftPersistsAcrossReopen(t *testing.T) {
	root := t.TempDir()
	store, err := NewLazy(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("new lazy store: %v", err)
	}
	want := "draft line one\nline two"
	if err := store.SetInputDraft(want); err != nil {
		t.Fatalf("set input draft: %v", err)
	}
	reopened, err := Open(store.Dir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if reopened.Meta().InputDraft != want {
		t.Fatalf("expected persisted draft %q, got %q", want, reopened.Meta().InputDraft)
	}
}

func TestSetInputDraftClearsPersistedValue(t *testing.T) {
	root := t.TempDir()
	store, err := Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.SetInputDraft("draft"); err != nil {
		t.Fatalf("set draft: %v", err)
	}
	if err := store.SetInputDraft(""); err != nil {
		t.Fatalf("clear draft: %v", err)
	}
	reopened, err := Open(store.Dir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if reopened.Meta().InputDraft != "" {
		t.Fatalf("expected cleared draft, got %q", reopened.Meta().InputDraft)
	}
}

func TestSetUsageStatePersistsAcrossReopen(t *testing.T) {
	root := t.TempDir()
	store, err := NewLazy(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("new lazy store: %v", err)
	}
	if err := store.SetUsageState(&UsageState{
		InputTokens:             900,
		OutputTokens:            120,
		WindowTokens:            400_000,
		CachedInputTokens:       50,
		HasCachedInputTokens:    true,
		EstimatedProviderTokens: 180,
		TotalInputTokens:        1_200,
		TotalCachedInputTokens:  60,
	}); err != nil {
		t.Fatalf("set usage state: %v", err)
	}
	reopened, err := Open(store.Dir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if reopened.Meta().UsageState == nil {
		t.Fatal("expected persisted usage state")
	}
	if got := reopened.Meta().UsageState; got.InputTokens != 900 || got.EstimatedProviderTokens != 180 || got.TotalInputTokens != 1_200 {
		t.Fatalf("unexpected usage state after reopen: %+v", got)
	}
}

func TestListSessionsSortedByUpdatedAt(t *testing.T) {
	root := t.TempDir()
	s1, err := Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create session1: %v", err)
	}
	if _, err := s1.AppendEvent("step1", "message", map[string]any{"a": 1}); err != nil {
		t.Fatalf("append event1: %v", err)
	}

	s2, err := Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create session2: %v", err)
	}
	if _, err := s2.AppendEvent("step1", "message", map[string]any{"b": 2}); err != nil {
		t.Fatalf("append event2: %v", err)
	}

	items, err := ListSessions(root)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(items))
	}
	if filepath.Base(items[0].Path) != s2.Meta().SessionID {
		t.Fatalf("latest session expected first")
	}
}

func TestLockedContractPersistenceIncludesSystemPromptButNotToolSchema(t *testing.T) {
	root := t.TempDir()
	store, err := Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.MarkModelDispatchLocked(LockedContract{
		Model:             "gpt-5",
		Temperature:       1,
		MaxOutputToken:    0,
		SystemPrompt:      "locked system prompt",
		HasSystemPrompt:   true,
		ReviewerPrompt:    "locked reviewer prompt",
		HasReviewerPrompt: true,
	}); err != nil {
		t.Fatalf("mark model dispatch locked: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(store.Dir(), sessionFile))
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "tools_json") {
		t.Fatalf("session metadata must not persist tools_json: %s", text)
	}
	opened, err := Open(store.Dir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	locked := opened.Meta().Locked
	if locked == nil || locked.SystemPrompt != "locked system prompt" || !locked.HasSystemPrompt {
		t.Fatalf("locked system prompt = %+v, want persisted snapshot marker", locked)
	}
	if locked.ReviewerPrompt != "locked reviewer prompt" || !locked.HasReviewerPrompt {
		t.Fatalf("locked reviewer prompt = %+v, want persisted snapshot marker", locked)
	}
}

func TestReadEventsHandlesLargeJSONLines(t *testing.T) {
	root := t.TempDir()
	store, err := Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	const payloadSize = 128 * 1024
	large := strings.Repeat("x", payloadSize)
	if _, err := store.AppendEvent("step1", "message", map[string]any{"blob": large}); err != nil {
		t.Fatalf("append large event: %v", err)
	}

	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}

	var payload map[string]string
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if got := len(payload["blob"]); got != payloadSize {
		t.Fatalf("payload blob size = %d, want %d", got, payloadSize)
	}
}

func TestAppendEventPersistsFirstPromptPreview(t *testing.T) {
	root := t.TempDir()
	store, err := Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if _, err := store.AppendEvent("s1", "message", map[string]any{"role": "assistant", "content": "hello"}); err != nil {
		t.Fatalf("append assistant event: %v", err)
	}
	if got := store.Meta().FirstPromptPreview; got != "" {
		t.Fatalf("expected assistant event to leave preview empty, got %q", got)
	}
	if _, err := store.AppendEvent("s2", "message", map[string]any{"role": "user", "content": "Investigate config load failures\nsecond line"}); err != nil {
		t.Fatalf("append user event: %v", err)
	}
	if got := store.Meta().FirstPromptPreview; got != "Investigate config load failures" {
		t.Fatalf("preview = %q, want %q", got, "Investigate config load failures")
	}

	opened, err := Open(store.Dir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if got := opened.Meta().FirstPromptPreview; got != "Investigate config load failures" {
		t.Fatalf("reopened preview = %q, want %q", got, "Investigate config load failures")
	}

	items, err := ListSessions(root)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one session, got %d", len(items))
	}
	if items[0].FirstPromptPreview != "Investigate config load failures" {
		t.Fatalf("list preview = %q, want %q", items[0].FirstPromptPreview, "Investigate config load failures")
	}
}

func TestConversationFreshnessAdvancesOnlyForVisibleUserMessages(t *testing.T) {
	root := t.TempDir()
	store, err := Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if got := store.ConversationFreshness(); got != ConversationFreshnessFresh {
		t.Fatalf("freshness = %v, want fresh", got)
	}
	if _, err := store.AppendEvent("s1", "message", map[string]any{"role": "assistant", "content": "hello"}); err != nil {
		t.Fatalf("append assistant event: %v", err)
	}
	if got := store.ConversationFreshness(); got != ConversationFreshnessFresh {
		t.Fatalf("freshness after assistant = %v, want fresh", got)
	}
	if _, err := store.AppendEvent("s2", "message", map[string]any{"role": "developer", "message_type": "compaction_summary", "content": "summary"}); err != nil {
		t.Fatalf("append compaction summary event: %v", err)
	}
	if got := store.ConversationFreshness(); got != ConversationFreshnessFresh {
		t.Fatalf("freshness after compaction summary = %v, want fresh", got)
	}
	if _, err := store.AppendEvent("s3", "message", map[string]any{"role": "user", "content": "Investigate config load failures"}); err != nil {
		t.Fatalf("append user event: %v", err)
	}
	if got := store.ConversationFreshness(); got != ConversationFreshnessEstablished {
		t.Fatalf("freshness after visible user message = %v, want established", got)
	}
}

func TestOpenRehydratesConversationFreshnessFromEvents(t *testing.T) {
	root := t.TempDir()
	store, err := Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if _, err := store.AppendEvent("s1", "message", map[string]any{"role": "user", "content": "Investigate config load failures"}); err != nil {
		t.Fatalf("append user event: %v", err)
	}

	opened, err := Open(store.Dir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if got := opened.ConversationFreshness(); got != ConversationFreshnessEstablished {
		t.Fatalf("reopened freshness = %v, want established", got)
	}
}

func TestFirstPromptPreviewSkipsCompactionSummaryMessages(t *testing.T) {
	root := t.TempDir()
	store, err := Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if _, err := store.AppendEvent("s1", "message", map[string]any{"role": "developer", "message_type": "compaction_summary", "content": "summary"}); err != nil {
		t.Fatalf("append compaction summary event: %v", err)
	}
	if got := store.Meta().FirstPromptPreview; got != "" {
		t.Fatalf("expected compaction summary to be ignored, got %q", got)
	}
	if _, err := store.AppendEvent("s2", "message", map[string]any{"role": "user", "content": "\n  Fix config registry boot path\nmore details"}); err != nil {
		t.Fatalf("append visible user event: %v", err)
	}
	if got := store.Meta().FirstPromptPreview; got != "Fix config registry boot path" {
		t.Fatalf("preview = %q, want %q", got, "Fix config registry boot path")
	}
}

func TestAppendTurnAtomicPersistsFirstPromptPreview(t *testing.T) {
	root := t.TempDir()
	store, err := Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if _, err := store.AppendTurnAtomic("s1", []EventInput{{Kind: "message", Payload: map[string]any{"role": "assistant", "content": "hello"}}, {Kind: "message", Payload: map[string]any{"role": "user", "content": "Atomic preview source\nmore"}}}); err != nil {
		t.Fatalf("append turn: %v", err)
	}
	if got := store.Meta().FirstPromptPreview; got != "Atomic preview source" {
		t.Fatalf("preview = %q, want %q", got, "Atomic preview source")
	}
}

func TestListSessionsUsesPersistedFirstPromptPreviewOnly(t *testing.T) {
	root := t.TempDir()
	store, err := Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if _, err := store.AppendEvent("s1", "message", map[string]any{"role": "user", "content": "Preview source\nsecond line"}); err != nil {
		t.Fatalf("append user event: %v", err)
	}

	metaPath := filepath.Join(store.Dir(), sessionFile)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("decode session meta: %v", err)
	}
	meta.FirstPromptPreview = ""
	rewritten, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatalf("encode session meta: %v", err)
	}
	if err := os.WriteFile(metaPath, rewritten, 0o644); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	items, err := ListSessions(root)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one session, got %d", len(items))
	}
	if items[0].FirstPromptPreview != "" {
		t.Fatalf("expected listed session preview to remain empty, got %q", items[0].FirstPromptPreview)
	}

	reloaded, err := Open(store.Dir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if reloaded.Meta().FirstPromptPreview != "" {
		t.Fatalf("expected persisted metadata preview to remain empty after list, got %q", reloaded.Meta().FirstPromptPreview)
	}
}

func TestForkAtUserMessageCopiesPrefixBeforeSelectedMessage(t *testing.T) {
	root := t.TempDir()
	parent, err := Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if _, err := parent.AppendEvent("s1", "message", map[string]any{"role": "user", "content": "u1"}); err != nil {
		t.Fatalf("append u1: %v", err)
	}
	if _, err := parent.AppendEvent("s1", "message", map[string]any{"role": "assistant", "content": "a1"}); err != nil {
		t.Fatalf("append a1: %v", err)
	}
	if _, err := parent.AppendEvent("s2", "message", map[string]any{"role": "user", "content": "u2"}); err != nil {
		t.Fatalf("append u2: %v", err)
	}
	if _, err := parent.AppendEvent("s2", "message", map[string]any{"role": "assistant", "content": "a2"}); err != nil {
		t.Fatalf("append a2: %v", err)
	}

	forked, err := ForkAtUserMessage(parent, 2, "Parent → edit u2")
	if err != nil {
		t.Fatalf("fork at user message: %v", err)
	}
	forkEvents, err := forked.ReadEvents()
	if err != nil {
		t.Fatalf("read fork events: %v", err)
	}
	if len(forkEvents) != 2 {
		t.Fatalf("expected two replayed events, got %d", len(forkEvents))
	}
	var first struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(forkEvents[0].Payload, &first); err != nil {
		t.Fatalf("decode first message: %v", err)
	}
	if first.Role != "user" || first.Content != "u1" {
		t.Fatalf("unexpected first message in fork: %+v", first)
	}
	meta := forked.Meta()
	if meta.ParentSessionID != parent.Meta().SessionID {
		t.Fatalf("expected fork parent session id, got %q", meta.ParentSessionID)
	}
	if meta.Name != "Parent → edit u2" {
		t.Fatalf("expected fork name, got %q", meta.Name)
	}
	if meta.FirstPromptPreview != "u1" {
		t.Fatalf("expected fork preview to persist first user message, got %q", meta.FirstPromptPreview)
	}
}

func TestForkAtUserMessageDerivesReminderIssuedFromReplayedHistory(t *testing.T) {
	parent, err := Create(t.TempDir(), "ws", t.TempDir())
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if _, err := parent.AppendEvent("s1", "message", map[string]any{"role": "user", "content": "u1"}); err != nil {
		t.Fatalf("append first user: %v", err)
	}
	if _, err := parent.AppendEvent("s1", "message", map[string]any{"role": "developer", "message_type": "compaction_soon_reminder", "content": "compact soon"}); err != nil {
		t.Fatalf("append reminder: %v", err)
	}
	if err := parent.SetCompactionSoonReminderIssued(true); err != nil {
		t.Fatalf("persist reminder state: %v", err)
	}
	if _, err := parent.AppendEvent("s2", "message", map[string]any{"role": "user", "content": "u2"}); err != nil {
		t.Fatalf("append second user: %v", err)
	}

	beforeReminder, err := ForkAtUserMessage(parent, 1, "before reminder")
	if err != nil {
		t.Fatalf("fork before reminder: %v", err)
	}
	if beforeReminder.Meta().CompactionSoonReminderIssued {
		t.Fatal("expected fork before reminder to clear reminder-issued state")
	}

	afterReminder, err := ForkAtUserMessage(parent, 2, "after reminder")
	if err != nil {
		t.Fatalf("fork after reminder: %v", err)
	}
	if !afterReminder.Meta().CompactionSoonReminderIssued {
		t.Fatal("expected fork after reminder to preserve reminder-issued state")
	}

	t.Run("legacy reviewer rollback history replacement is ignored", func(t *testing.T) {
		parent, err := Create(t.TempDir(), "ws", t.TempDir())
		if err != nil {
			t.Fatalf("create parent: %v", err)
		}
		if _, err := parent.AppendEvent("s1", "message", map[string]any{"role": "user", "content": "u1"}); err != nil {
			t.Fatalf("append first user: %v", err)
		}
		if _, err := parent.AppendEvent("s1", "history_replaced", map[string]any{
			"engine": "reviewer_rollback",
			"items": []map[string]any{{
				"type":         "message",
				"role":         "developer",
				"message_type": "compaction_soon_reminder",
				"content":      "compact soon",
			}},
		}); err != nil {
			t.Fatalf("append reviewer rollback history replacement: %v", err)
		}
		if _, err := parent.AppendEvent("s2", "message", map[string]any{"role": "user", "content": "u2"}); err != nil {
			t.Fatalf("append second user: %v", err)
		}

		forked, err := ForkAtUserMessage(parent, 2, "after legacy reviewer rollback")
		if err != nil {
			t.Fatalf("fork: %v", err)
		}
		if forked.Meta().CompactionSoonReminderIssued {
			t.Fatal("expected legacy reviewer rollback history replacement to be ignored for reminder-issued state")
		}
	})

	t.Run("non-reviewer history replacement clears reminder state", func(t *testing.T) {
		parent, err := Create(t.TempDir(), "ws", t.TempDir())
		if err != nil {
			t.Fatalf("create parent: %v", err)
		}
		if _, err := parent.AppendEvent("s1", "message", map[string]any{"role": "user", "content": "u1"}); err != nil {
			t.Fatalf("append first user: %v", err)
		}
		if _, err := parent.AppendEvent("s1", "message", map[string]any{"role": "developer", "message_type": "compaction_soon_reminder", "content": "compact soon"}); err != nil {
			t.Fatalf("append reminder: %v", err)
		}
		if _, err := parent.AppendEvent("s1", "history_replaced", map[string]any{
			"engine": "compaction",
			"items":  []map[string]any{},
		}); err != nil {
			t.Fatalf("append compaction history replacement: %v", err)
		}
		if _, err := parent.AppendEvent("s2", "message", map[string]any{"role": "user", "content": "u2"}); err != nil {
			t.Fatalf("append second user: %v", err)
		}

		forked, err := ForkAtUserMessage(parent, 2, "after compaction")
		if err != nil {
			t.Fatalf("fork: %v", err)
		}
		if forked.Meta().CompactionSoonReminderIssued {
			t.Fatal("expected reminder-issued state to clear after non-reviewer history replacement")
		}
	})
}

func TestForkAtUserMessageResetsWorktreeReminderGenerationFlags(t *testing.T) {
	parent, err := Create(t.TempDir(), "ws", t.TempDir())
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if _, err := parent.AppendEvent("s1", "message", map[string]any{"role": "user", "content": "u1"}); err != nil {
		t.Fatalf("append first user: %v", err)
	}
	if err := parent.SetWorktreeReminderState(&WorktreeReminderState{
		Mode:                  WorktreeReminderModeEnter,
		Branch:                "feature/fork",
		WorktreePath:          "/tmp/wt-fork",
		WorkspaceRoot:         "/tmp/workspace",
		EffectiveCwd:          "/tmp/wt-fork",
		HasIssuedInGeneration: true,
		IssuedCompactionCount: 7,
	}); err != nil {
		t.Fatalf("persist worktree reminder state: %v", err)
	}
	if _, err := parent.AppendEvent("s2", "message", map[string]any{"role": "user", "content": "u2"}); err != nil {
		t.Fatalf("append second user: %v", err)
	}

	forked, err := ForkAtUserMessage(parent, 2, "forked")
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	state := forked.Meta().WorktreeReminder
	if state == nil {
		t.Fatal("expected forked worktree reminder state")
	}
	if state.Mode != WorktreeReminderModeEnter || state.Branch != "feature/fork" {
		t.Fatalf("unexpected forked reminder payload: %+v", state)
	}
	if state.HasIssuedInGeneration || state.IssuedCompactionCount != 0 {
		t.Fatalf("expected forked reminder generation flags reset, got %+v", state)
	}
	parentState := parent.Meta().WorktreeReminder
	if parentState == nil || !parentState.HasIssuedInGeneration || parentState.IssuedCompactionCount != 7 {
		t.Fatalf("expected parent reminder state unchanged, got %+v", parentState)
	}
}

func TestInitializeChildFromParentCopiesContextWithoutConversationState(t *testing.T) {
	root := t.TempDir()
	parent, err := Create(root, "workspace-parent", "/tmp/work-parent")
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	toolPreambles := true
	if err := parent.MarkModelDispatchLocked(LockedContract{Model: "locked-parent", EnabledTools: []string{"shell", "patch"}, ToolPreambles: &toolPreambles}); err != nil {
		t.Fatalf("MarkModelDispatchLocked parent: %v", err)
	}
	if err := parent.MarkAgentsInjected(); err != nil {
		t.Fatalf("MarkAgentsInjected parent: %v", err)
	}
	if err := parent.SetContinuationContext(ContinuationContext{OpenAIBaseURL: "http://parent.local/v1"}); err != nil {
		t.Fatalf("SetContinuationContext parent: %v", err)
	}
	if err := parent.SetUsageState(&UsageState{InputTokens: 123}); err != nil {
		t.Fatalf("SetUsageState parent: %v", err)
	}
	if err := parent.SetWorktreeReminderState(&WorktreeReminderState{
		Mode:                  WorktreeReminderModeEnter,
		Branch:                "feature/child-context",
		WorktreePath:          "/tmp/work-parent-wt",
		WorkspaceRoot:         "/tmp/work-parent",
		EffectiveCwd:          "/tmp/work-parent-wt/pkg",
		HasIssuedInGeneration: true,
		IssuedCompactionCount: 2,
	}); err != nil {
		t.Fatalf("SetWorktreeReminderState parent: %v", err)
	}
	child, err := NewLazy(root, "workspace-child", "/tmp/work-child")
	if err != nil {
		t.Fatalf("new child: %v", err)
	}

	if err := InitializeChildFromParent(child, parent); err != nil {
		t.Fatalf("InitializeChildFromParent: %v", err)
	}
	meta := child.Meta()
	if meta.ParentSessionID != parent.Meta().SessionID {
		t.Fatalf("parent session id = %q, want %q", meta.ParentSessionID, parent.Meta().SessionID)
	}
	if meta.WorkspaceRoot != "/tmp/work-parent" || meta.WorkspaceContainer != "workspace-parent" {
		t.Fatalf("workspace context = root %q container %q, want parent", meta.WorkspaceRoot, meta.WorkspaceContainer)
	}
	if meta.AgentsInjected {
		t.Fatal("expected fresh child to reinject developer context on its first turn")
	}
	if meta.Locked == nil || meta.Locked.Model != "locked-parent" || len(meta.Locked.EnabledTools) != 2 {
		t.Fatalf("locked contract = %+v, want parent lock", meta.Locked)
	}
	if meta.Locked.ToolPreambles == nil || !*meta.Locked.ToolPreambles {
		t.Fatalf("locked tool preambles = %+v, want copied true", meta.Locked.ToolPreambles)
	}
	if meta.Locked.ToolPreambles == parent.Meta().Locked.ToolPreambles {
		t.Fatal("expected locked tool preambles pointer to be deep-copied")
	}
	if meta.Continuation == nil || meta.Continuation.OpenAIBaseURL != "http://parent.local/v1" {
		t.Fatalf("continuation = %+v, want parent continuation", meta.Continuation)
	}
	if meta.UsageState != nil {
		t.Fatalf("usage state = %+v, want nil for fresh child", meta.UsageState)
	}
	if meta.FirstPromptPreview != "" || meta.ModelRequestCount != 0 {
		t.Fatalf("conversation state leaked into child: %+v", meta)
	}
	if meta.WorktreeReminder == nil {
		t.Fatal("expected worktree reminder")
	}
	if meta.WorktreeReminder.Branch != "feature/child-context" {
		t.Fatalf("worktree reminder = %+v, want parent branch", meta.WorktreeReminder)
	}
	if meta.WorktreeReminder.HasIssuedInGeneration || meta.WorktreeReminder.IssuedCompactionCount != 0 {
		t.Fatalf("worktree reminder generation flags = %+v, want reset", meta.WorktreeReminder)
	}
}

func TestSetContinuationContextStaysLazyUntilFirstWrite(t *testing.T) {
	root := t.TempDir()
	store, err := NewLazy(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("new lazy store: %v", err)
	}
	if err := store.SetContinuationContext(ContinuationContext{OpenAIBaseURL: "http://example.local/v1"}); err != nil {
		t.Fatalf("set continuation context: %v", err)
	}
	if store.Meta().Continuation == nil || store.Meta().Continuation.OpenAIBaseURL != "http://example.local/v1" {
		t.Fatalf("expected in-memory continuation context, got %+v", store.Meta().Continuation)
	}
	if _, err := os.Stat(store.Dir()); !os.IsNotExist(err) {
		t.Fatalf("expected lazy session to remain unpersisted, stat err=%v", err)
	}
	if _, err := store.AppendEvent("step1", "message", map[string]any{"a": 1}); err != nil {
		t.Fatalf("append event: %v", err)
	}
	opened, err := Open(store.Dir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if opened.Meta().Continuation == nil || opened.Meta().Continuation.OpenAIBaseURL != "http://example.local/v1" {
		t.Fatalf("expected persisted continuation context, got %+v", opened.Meta().Continuation)
	}
}

func TestSessionMetadataDoesNotPersistModelVerbosityState(t *testing.T) {
	root := t.TempDir()
	store, err := Create(root, "workspace-x", "/tmp/work")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.MarkModelDispatchLocked(LockedContract{
		Model:          "gpt-5",
		Temperature:    1,
		MaxOutputToken: 0,
	}); err != nil {
		t.Fatalf("mark model dispatch locked: %v", err)
	}
	if err := store.SetContinuationContext(ContinuationContext{OpenAIBaseURL: "http://example.local/v1"}); err != nil {
		t.Fatalf("set continuation context: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(store.Dir(), sessionFile))
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "openai_base_url") {
		t.Fatalf("expected continuation openai_base_url to persist, got %q", text)
	}
	if strings.Contains(text, "model_verbosity") {
		t.Fatalf("session metadata must not persist model_verbosity: %s", text)
	}

	opened, err := Open(store.Dir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if opened.Meta().Continuation == nil || opened.Meta().Continuation.OpenAIBaseURL != "http://example.local/v1" {
		t.Fatalf("expected persisted continuation context, got %+v", opened.Meta().Continuation)
	}
	reopenedMetaJSON, err := json.Marshal(opened.Meta())
	if err != nil {
		t.Fatalf("marshal reopened meta: %v", err)
	}
	if strings.Contains(string(reopenedMetaJSON), "model_verbosity") {
		t.Fatal("expected reopened session metadata to remain free of model_verbosity")
	}
}
