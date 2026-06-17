package runtimeview

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	"core/server/tools"
	"core/shared/clientui"
	"core/shared/transcript"
	patchformat "core/shared/transcript/patchformat"
)

type projectionFastClient struct{}

func (projectionFastClient) Generate(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("not implemented")
}

func (projectionFastClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true, IsOpenAIFirstParty: true}, nil
}

type projectionPreciseClient struct {
	inputTokens int
}

func (c projectionPreciseClient) Generate(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{InputTokens: 900, OutputTokens: 100, WindowTokens: 400_000},
	}, nil
}

func (c projectionPreciseClient) CountRequestInputTokens(context.Context, llm.Request) (int, error) {
	return c.inputTokens, nil
}

func (c projectionPreciseClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true, SupportsRequestInputTokenCount: true, IsOpenAIFirstParty: true}, nil
}

func newRuntimeViewStore(t *testing.T) *session.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	return store
}

func newRuntimeViewEngine(t *testing.T, store *session.Store, client llm.Client, cfg ...runtime.Config) *runtime.Engine {
	t.Helper()
	engineConfig := runtime.Config{Model: "gpt-5"}
	if len(cfg) > 0 {
		engineConfig = cfg[0]
	}
	engine, err := runtime.New(store, client, tools.NewRegistry(), engineConfig)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	return engine
}

func appendRuntimeViewMessages(t *testing.T, store *session.Store, count int, text func(int) string) {
	t.Helper()
	for i := range count {
		if _, _, err := store.AppendEvent("step-1", "message", llm.Message{Role: llm.RoleAssistant, Content: text(i), Phase: llm.MessagePhaseFinal}); err != nil {
			t.Fatalf("append message %d: %v", i, err)
		}
	}
}

func TestEventFromRuntimeProjectsReasoningAndBackground(t *testing.T) {
	exitCode := 17
	view := EventFromRuntime(runtime.Event{
		Kind:                       runtime.EventBackgroundUpdated,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		AssistantDelta:             "delta",
		ReasoningDelta:             &llm.ReasoningSummaryDelta{Key: "k", Role: "reasoning", Text: "thinking"},
		RunState:                   &runtime.RunState{Lifecycle: runtime.RunningRunLifecycle(runtime.RunModeTurn), RunID: "run-1", Status: runtime.RunStatusRunning},
		Background: &runtime.BackgroundShellEvent{
			Type:              "completed",
			ID:                "123",
			State:             "completed",
			Command:           "echo hi",
			Workdir:           "/tmp/work",
			LogPath:           "/tmp/work/run.log",
			NoticeText:        "done",
			CompactText:       "done compact",
			Preview:           "hi",
			Removed:           2,
			ExitCode:          &exitCode,
			UserRequestedKill: true,
			NoticeSuppressed:  true,
		},
	})
	if view.Kind != "background_updated" || view.StepID != "step-1" || view.AssistantDelta != "delta" {
		t.Fatalf("unexpected projected event: %+v", view)
	}
	if !view.CommittedTranscriptChanged {
		t.Fatalf("expected committed transcript change flag projected, got %+v", view)
	}
	if view.ReasoningDelta == nil || view.ReasoningDelta.Text != "thinking" {
		t.Fatalf("expected reasoning delta projection, got %+v", view.ReasoningDelta)
	}
	if view.RunState == nil || !view.RunState.Lifecycle.IsRunning() {
		t.Fatalf("expected busy run state, got %+v", view.RunState)
	}
	if view.RunState.RunID != "run-1" || view.RunState.Status != "running" {
		t.Fatalf("expected run identity in projected run state, got %+v", view.RunState)
	}
	if view.RunState.Lifecycle.Phase != clientui.RunLifecycleRunning || view.RunState.Lifecycle.Mode != clientui.RunModeTurn {
		t.Fatalf("server/client run lifecycle projection mismatch: %+v", view.RunState.Lifecycle)
	}
	if view.Background == nil || view.Background.ID != "123" {
		t.Fatalf("expected background projection, got %+v", view.Background)
	}
	if view.Background.ExitCode == nil || *view.Background.ExitCode != 17 {
		t.Fatalf("expected copied exit code, got %+v", view.Background.ExitCode)
	}
}

func TestStatusFromRuntimeIncludesSuspendedGoal(t *testing.T) {
	engine := newRuntimeViewEngine(t, newRuntimeViewStore(t), projectionFastClient{})
	if _, err := engine.SetGoal("ship feature", session.GoalActorUser); err != nil {
		t.Fatalf("set goal: %v", err)
	}
	if err := engine.Interrupt(); err != nil {
		t.Fatalf("interrupt: %v", err)
	}

	status := StatusFromRuntime(engine)
	if status.Goal == nil || !status.Goal.Suspended {
		t.Fatalf("goal status = %+v, want suspended goal", status.Goal)
	}
}

func TestEventFromRuntimeProjectsLocalEntry(t *testing.T) {
	view := EventFromRuntime(runtime.Event{
		Kind:   runtime.EventLocalEntryAdded,
		StepID: "step-1",
		LocalEntry: &runtime.ChatEntry{
			Visibility:  transcript.EntryVisibilityAll,
			Role:        "reviewer_suggestions",
			Text:        "Supervisor suggested:\n1. Add verification notes.",
			OngoingText: "Supervisor made 1 suggestion.",
		},
	})

	if view.Kind != clientui.EventLocalEntryAdded || view.StepID != "step-1" {
		t.Fatalf("unexpected projected local entry event: %+v", view)
	}
	if len(view.TranscriptEntries) != 1 {
		t.Fatalf("expected one projected local entry, got %+v", view.TranscriptEntries)
	}
	entry := view.TranscriptEntries[0]
	if entry.Role != "reviewer_suggestions" || entry.Text != "Supervisor suggested:\n1. Add verification notes." || entry.OngoingText != "Supervisor made 1 suggestion." {
		t.Fatalf("unexpected projected local entry transcript: %+v", entry)
	}
	if entry.Visibility != clientui.EntryVisibilityAll {
		t.Fatalf("local entry visibility = %q, want all", entry.Visibility)
	}
}

func TestEventFromRuntimeLeavesCompactionStatusWithoutTranscriptEntriesUntilPersistedLocalEntry(t *testing.T) {
	testCases := []struct {
		name string
		evt  runtime.Event
	}{
		{
			name: "compaction completed",
			evt: runtime.Event{
				Kind:   runtime.EventCompactionCompleted,
				StepID: "step-1",
				Compaction: &runtime.CompactionStatus{
					Mode:  "auto",
					Count: 1,
				},
			},
		},
		{
			name: "compaction failed",
			evt: runtime.Event{
				Kind:   runtime.EventCompactionFailed,
				StepID: "step-1",
				Compaction: &runtime.CompactionStatus{
					Mode:  "manual",
					Error: "quota exceeded",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			view := EventFromRuntime(tc.evt)
			if tc.evt.Compaction == nil || view.Compaction == nil {
				t.Fatalf("expected compaction status projection, got %+v", view.Compaction)
			}
			if view.Compaction.Mode != tc.evt.Compaction.Mode || view.Compaction.Count != tc.evt.Compaction.Count || view.Compaction.Error != tc.evt.Compaction.Error {
				t.Fatalf("projected compaction = %+v, want %+v", view.Compaction, tc.evt.Compaction)
			}
			if len(view.TranscriptEntries) != 0 {
				t.Fatalf("expected no projected transcript entries before persisted local entry, got %+v", view.TranscriptEntries)
			}
		})
	}

	local := EventFromRuntime(runtime.Event{
		Kind:                       runtime.EventLocalEntryAdded,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		LocalEntry:                 &runtime.ChatEntry{Role: "compaction_notice", Text: "context compacted for the 1st time"},
	})
	if len(local.TranscriptEntries) != 1 {
		t.Fatalf("expected persisted local entry to remain the transcript source, got %+v", local.TranscriptEntries)
	}
	if got := local.TranscriptEntries[0].Role; got != "compaction_notice" {
		t.Fatalf("projected local entry role = %q, want compaction_notice", got)
	}
}

func TestRunViewFromRuntimeCopiesSnapshot(t *testing.T) {
	startedAt := time.Now().UTC().Add(-time.Minute)
	finishedAt := time.Now().UTC()
	view := RunViewFromRuntime("session-1", &runtime.RunSnapshot{
		RunID:      "run-1",
		StepID:     "step-1",
		Status:     runtime.RunStatusCompleted,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	})
	if view == nil {
		t.Fatal("expected run view")
	}
	if view.RunID != "run-1" || view.SessionID != "session-1" || view.StepID != "step-1" {
		t.Fatalf("unexpected run view ids: %+v", view)
	}
	if view.Status != "completed" || !view.StartedAt.Equal(startedAt) || !view.FinishedAt.Equal(finishedAt) {
		t.Fatalf("unexpected run view timing/status: %+v", view)
	}
}

func TestMainViewFromRuntimeBundlesStatusAndSession(t *testing.T) {
	store := newRuntimeViewStore(t)
	if err := store.SetName("Session Name"); err != nil {
		t.Fatalf("set name: %v", err)
	}
	if err := store.SetParentSessionID("parent-123"); err != nil {
		t.Fatalf("set parent session id: %v", err)
	}
	if _, _, err := store.AppendEvent("step-1", "message", llm.Message{Role: llm.RoleAssistant, Content: "final answer", Phase: llm.MessagePhaseFinal}); err != nil {
		t.Fatalf("append assistant message: %v", err)
	}
	eng := newRuntimeViewEngine(t, store, projectionFastClient{}, runtime.Config{Model: "gpt-5", ContextWindowTokens: 400_000})
	if err := eng.SetThinkingLevel("high"); err != nil {
		t.Fatalf("set thinking level: %v", err)
	}
	if changed, err := eng.SetFastModeEnabled(true); err != nil {
		t.Fatalf("enable fast mode: %v", err)
	} else if !changed {
		t.Fatal("expected fast mode enable to report changed=true")
	}
	if changed, enabled := eng.SetAutoCompactionEnabled(false); !changed || enabled {
		t.Fatalf("expected auto-compaction disabled, changed=%v enabled=%v", changed, enabled)
	}

	view := MainViewFromRuntime(eng)
	if view.Session.SessionID != store.Meta().SessionID || view.Session.SessionName != "Session Name" {
		t.Fatalf("unexpected session hydration: %+v", view.Session)
	}
	if got := len(view.Session.Chat.Entries); got != 0 {
		t.Fatalf("expected main view to omit transcript payload, got %d entries", got)
	}
	if view.Status.ParentSessionID != "parent-123" || view.Status.LastCommittedAssistantFinalAnswer != "final answer" {
		t.Fatalf("unexpected status hydration: %+v", view.Status)
	}
	if view.Status.ThinkingLevel != "high" || !view.Status.FastModeEnabled || view.Status.AutoCompactionEnabled {
		t.Fatalf("unexpected runtime flags: %+v", view.Status)
	}
	if view.Status.ContextUsage.WindowTokens != 400_000 {
		t.Fatalf("context window tokens = %d, want 400000", view.Status.ContextUsage.WindowTokens)
	}
	if view.ActiveRun != nil {
		t.Fatalf("expected no active run in idle main view, got %+v", view.ActiveRun)
	}
}

func TestSessionViewFromRuntimeUsesCommittedEntryMetadata(t *testing.T) {
	store := newRuntimeViewStore(t)
	if _, _, err := store.AppendEvent("step-1", "message", llm.Message{Role: llm.RoleUser, Content: "hello"}); err != nil {
		t.Fatalf("append user message: %v", err)
	}
	if _, _, err := store.AppendEvent("step-1", "local_entry", map[string]any{"role": "system", "text": "local note", "ongoing_text": ""}); err != nil {
		t.Fatalf("append local entry: %v", err)
	}
	if _, _, err := store.AppendEvent("step-1", "message", llm.Message{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeErrorFeedback, Content: "warn"}); err != nil {
		t.Fatalf("append warning message: %v", err)
	}
	eng := newRuntimeViewEngine(t, store, projectionFastClient{})
	view := SessionViewFromRuntime(eng)
	if view.Transcript.CommittedEntryCount != eng.CommittedTranscriptEntryCount() {
		t.Fatalf("projected committed entry count = %d, engine committed entry count = %d", view.Transcript.CommittedEntryCount, eng.CommittedTranscriptEntryCount())
	}
	if got := len(view.Chat.Entries); got != 0 {
		t.Fatalf("session view chat entry count = %d, want 0", got)
	}
}

func TestStatusFromRuntimeUsesFreshPreciseCurrentTokens(t *testing.T) {
	eng := newRuntimeViewEngine(t, newRuntimeViewStore(t), projectionPreciseClient{inputTokens: 180}, runtime.Config{
		Model:                         "gpt-5",
		ContextWindowTokens:           400_000,
		AutoCompactTokenLimit:         1_000,
		PreSubmitCompactionLeadTokens: 100,
	})
	if _, err := eng.SubmitUserMessage(context.Background(), "prompt"); err != nil {
		t.Fatalf("submit user message: %v", err)
	}
	if _, err := eng.ShouldCompactBeforeUserMessage(context.Background(), "follow-up"); err != nil {
		t.Fatalf("warm exact count: %v", err)
	}
	view := StatusFromRuntime(eng)
	if view.ContextUsage.UsedTokens != 180 {
		t.Fatalf("projected used tokens=%d, want exact 180", view.ContextUsage.UsedTokens)
	}
}

func TestEventFromRuntimeCopiesContextUsage(t *testing.T) {
	projected := EventFromRuntime(runtime.Event{
		Kind: runtime.EventModelResponse,
		ContextUsage: &runtime.ContextUsage{
			UsedTokens:            420,
			WindowTokens:          1_000,
			CacheHitPercent:       25,
			HasCacheHitPercentage: true,
		},
	})
	if projected.ContextUsage == nil {
		t.Fatal("expected projected event to carry context usage")
	}
	if projected.ContextUsage.UsedTokens != 420 || projected.ContextUsage.WindowTokens != 1_000 {
		t.Fatalf("projected context usage = %+v", projected.ContextUsage)
	}
	if projected.ContextUsage.CacheHitPercent != 25 || !projected.ContextUsage.HasCacheHitPercentage {
		t.Fatalf("projected cache hit usage = %+v", projected.ContextUsage)
	}
}

func TestEventFromRuntimeCopiesCacheWarningLostInputTokens(t *testing.T) {
	event := EventFromRuntime(runtime.Event{
		Kind:                   runtime.EventCacheWarning,
		CacheWarningVisibility: transcript.EntryVisibilityAll,
		CacheWarning: &transcript.CacheWarning{
			Scope:           transcript.CacheWarningScopeReviewer,
			Reason:          transcript.CacheWarningReasonNonPostfix,
			CacheKey:        "reviewer-cache-key",
			LostInputTokens: 12_000,
		},
	})
	if event.CacheWarning == nil {
		t.Fatal("expected projected cache warning")
	}
	if event.CacheWarning.LostInputTokens != 12_000 {
		t.Fatalf("cache warning lost input tokens = %d, want 12000", event.CacheWarning.LostInputTokens)
	}
	if event.CacheWarning.Scope != transcript.CacheWarningScopeReviewer {
		t.Fatalf("cache warning scope = %q, want %q", event.CacheWarning.Scope, transcript.CacheWarningScopeReviewer)
	}
	if event.CacheWarningVisibility != clientui.EntryVisibilityAll {
		t.Fatalf("cache warning visibility = %q, want %q", event.CacheWarningVisibility, clientui.EntryVisibilityAll)
	}
	if len(event.TranscriptEntries) != 1 {
		t.Fatalf("expected one projected transcript entry, got %d", len(event.TranscriptEntries))
	}
	if entry := event.TranscriptEntries[0]; entry.Role != "cache_warning" || entry.Visibility != clientui.EntryVisibilityAll {
		t.Fatalf("unexpected projected cache warning entry: %+v", entry)
	}
}

func TestEventFromRuntimeProjectsDefaultCacheWarningAsDetailOnly(t *testing.T) {
	event := EventFromRuntime(runtime.Event{
		Kind:                   runtime.EventCacheWarning,
		CacheWarningVisibility: transcript.EntryVisibilityDetailOnly,
		CacheWarning: &transcript.CacheWarning{
			Scope:  transcript.CacheWarningScopeConversation,
			Reason: transcript.CacheWarningReasonNonPostfix,
		},
	})
	if event.CacheWarningVisibility != clientui.EntryVisibilityDetailOnly {
		t.Fatalf("cache warning visibility = %q, want %q", event.CacheWarningVisibility, clientui.EntryVisibilityDetailOnly)
	}
	if len(event.TranscriptEntries) != 1 {
		t.Fatalf("expected one projected transcript entry, got %d", len(event.TranscriptEntries))
	}
	if entry := event.TranscriptEntries[0]; entry.Role != "cache_warning" || entry.Visibility != clientui.EntryVisibilityDetailOnly {
		t.Fatalf("unexpected projected cache warning entry: %+v", entry)
	}
}

func TestChatSnapshotFromRuntimeCopiesEntries(t *testing.T) {
	toolCall := &transcript.ToolCallMeta{
		ToolName:    "shell",
		Suggestions: []string{"a", "b"},
	}
	snapshot := ChatSnapshotFromRuntime(runtime.ChatSnapshot{
		Entries: []runtime.ChatEntry{{
			Visibility:        transcript.EntryVisibilityDetailOnly,
			Role:              "assistant",
			Text:              "hello",
			OngoingText:       "hel",
			Phase:             llm.MessagePhaseFinal,
			MessageType:       llm.MessageTypeEnvironment,
			SourcePath:        "/tmp/source",
			CompactLabel:      "compact",
			ToolResultSummary: "summary",
			ToolCallID:        "call-1",
			ToolCall:          toolCall,
		}},
		Ongoing:      "ongoing",
		OngoingError: "warn",
	})
	if len(snapshot.Entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(snapshot.Entries))
	}
	entry := snapshot.Entries[0]
	if entry.Phase != string(llm.MessagePhaseFinal) || entry.ToolCall == nil || entry.ToolCall.ToolName != "shell" {
		t.Fatalf("unexpected projected entry: %+v", entry)
	}
	if entry.Visibility != clientui.EntryVisibilityDetailOnly {
		t.Fatalf("entry visibility = %q, want %q", entry.Visibility, clientui.EntryVisibilityDetailOnly)
	}
	if entry.MessageType != string(llm.MessageTypeEnvironment) || entry.SourcePath != "/tmp/source" || entry.CompactLabel != "compact" || entry.ToolResultSummary != "summary" {
		t.Fatalf("metadata was not projected: %+v", entry)
	}
	if len(entry.ToolCall.Suggestions) != 2 {
		t.Fatalf("expected copied suggestions, got %+v", entry.ToolCall.Suggestions)
	}
	toolCall.Suggestions[0] = "changed"
	if snapshot.Entries[0].ToolCall.Suggestions[0] != "a" {
		t.Fatalf("expected projection to copy suggestions, got %+v", snapshot.Entries[0].ToolCall.Suggestions)
	}
	if snapshot.Ongoing != "ongoing" || snapshot.OngoingError != "warn" {
		t.Fatalf("unexpected snapshot projection: %+v", snapshot)
	}
}

func TestChatSnapshotFromRuntimeSuppressesNoopFinalAssistantState(t *testing.T) {
	snapshot := ChatSnapshotFromRuntime(runtime.ChatSnapshot{
		Entries: []runtime.ChatEntry{{
			Role:  "assistant",
			Text:  "NO_OP",
			Phase: llm.MessagePhaseFinal,
		}},
		Ongoing:      "NO_OP",
		OngoingError: "warn",
	})
	if got := len(snapshot.Entries); got != 0 {
		t.Fatalf("noop final entry count = %d, want 0", got)
	}
	if got := snapshot.Ongoing; got != "" {
		t.Fatalf("noop ongoing text = %q, want empty", got)
	}
	if got := snapshot.OngoingError; got != "warn" {
		t.Fatalf("ongoing error = %q, want warn", got)
	}
}

func TestTranscriptPageFromChatClonesPatchRender(t *testing.T) {
	snapshot := clientui.ChatSnapshot{Entries: []clientui.ChatEntry{{
		Role: "tool_call",
		ToolCall: &clientui.ToolCallMeta{
			PatchRender: &patchformat.RenderedPatch{
				SummaryLines: []patchformat.RenderedLine{{Text: "before"}},
			},
		},
	}}}

	page := TranscriptPageFromChat("session-1", "session", clientui.ConversationFreshnessEstablished, 1, snapshot, clientui.TranscriptPageRequest{})
	if len(page.Entries) != 1 || page.Entries[0].ToolCall == nil || page.Entries[0].ToolCall.PatchRender == nil {
		t.Fatalf("expected patch render copied into transcript page, got %+v", page.Entries)
	}
	snapshot.Entries[0].ToolCall.PatchRender.SummaryLines[0].Text = "after"
	if page.Entries[0].ToolCall.PatchRender.SummaryLines[0].Text != "before" {
		t.Fatalf("expected transcript page to deep copy patch render, got %+v", page.Entries[0].ToolCall.PatchRender.SummaryLines)
	}
}

func TestTranscriptPageFromChatSupportsPageNumberPagination(t *testing.T) {
	snapshot := clientui.ChatSnapshot{Entries: []clientui.ChatEntry{
		{Role: "assistant", Text: "a0"},
		{Role: "assistant", Text: "a1"},
		{Role: "assistant", Text: "a2"},
		{Role: "assistant", Text: "a3"},
		{Role: "assistant", Text: "a4"},
	}}

	page := TranscriptPageFromChat("session-1", "incident triage", clientui.ConversationFreshnessEstablished, 7, snapshot, clientui.TranscriptPageRequest{Page: 1, PageSize: 2})
	if page.TotalEntries != 5 {
		t.Fatalf("total entries = %d, want 5", page.TotalEntries)
	}
	if page.Offset != 2 {
		t.Fatalf("offset = %d, want 2", page.Offset)
	}
	if !page.HasMore || page.NextOffset != 4 {
		t.Fatalf("unexpected pagination metadata: %+v", page)
	}
	if len(page.Entries) != 2 || page.Entries[0].Text != "a2" || page.Entries[1].Text != "a3" {
		t.Fatalf("unexpected page entries: %+v", page.Entries)
	}
}

func TestTranscriptPageFromRuntimeUsesOngoingTailWindow(t *testing.T) {
	store := newRuntimeViewStore(t)
	appendRuntimeViewMessages(t, store, 600, func(int) string { return "reply" })
	eng := newRuntimeViewEngine(t, store, projectionFastClient{})

	page := TranscriptPageFromRuntime(eng, clientui.TranscriptPageRequest{Window: clientui.TranscriptWindowOngoingTail})
	if page.TotalEntries != 600 {
		t.Fatalf("total entries = %d, want 600", page.TotalEntries)
	}
	if page.Offset != 100 {
		t.Fatalf("offset = %d, want 100", page.Offset)
	}
	if page.HasMore {
		t.Fatalf("expected ongoing tail page to terminate at end, got %+v", page)
	}
	if len(page.Entries) != 500 {
		t.Fatalf("entries = %d, want 500", len(page.Entries))
	}
}

func TestTranscriptPageFromRuntimeUsesOngoingTailWindowByDefault(t *testing.T) {
	store := newRuntimeViewStore(t)
	appendRuntimeViewMessages(t, store, 600, func(int) string { return "reply" })
	eng := newRuntimeViewEngine(t, store, projectionFastClient{})

	page := TranscriptPageFromRuntime(eng, clientui.TranscriptPageRequest{})
	if page.TotalEntries != 600 {
		t.Fatalf("total entries = %d, want 600", page.TotalEntries)
	}
	if page.Offset != 100 {
		t.Fatalf("offset = %d, want 100", page.Offset)
	}
	if page.HasMore {
		t.Fatalf("expected default transcript request to return ongoing tail, got %+v", page)
	}
	if len(page.Entries) != 500 {
		t.Fatalf("entries = %d, want 500", len(page.Entries))
	}
}

func TestTranscriptPageFromRuntimeUsesIncrementalOngoingTailWhenClientKnowsRecentRevision(t *testing.T) {
	store := newRuntimeViewStore(t)
	appendRuntimeViewMessages(t, store, 600, func(i int) string { return fmt.Sprintf("reply-%03d", i) })
	eng := newRuntimeViewEngine(t, store, projectionFastClient{})

	page := TranscriptPageFromRuntime(eng, clientui.TranscriptPageRequest{
		Window:                   clientui.TranscriptWindowOngoingTail,
		KnownRevision:            599,
		KnownCommittedEntryCount: 590,
	})
	if page.TotalEntries != 600 {
		t.Fatalf("total entries = %d, want 600", page.TotalEntries)
	}
	if page.Offset != 558 {
		t.Fatalf("offset = %d, want 558", page.Offset)
	}
	if len(page.Entries) != 42 {
		t.Fatalf("entries = %d, want 42", len(page.Entries))
	}
	if got := page.Entries[0].Text; got != "reply-558" {
		t.Fatalf("first entry = %q, want reply-558", got)
	}
}

func TestTranscriptPageFromRuntimeUsesPagedSnapshotForOffsetLimit(t *testing.T) {
	store := newRuntimeViewStore(t)
	appendRuntimeViewMessages(t, store, 600, func(i int) string { return fmt.Sprintf("reply-%03d", i) })
	eng := newRuntimeViewEngine(t, store, projectionFastClient{})

	page := TranscriptPageFromRuntime(eng, clientui.TranscriptPageRequest{Offset: 550, Limit: 25})
	if page.TotalEntries != 600 {
		t.Fatalf("total entries = %d, want 600", page.TotalEntries)
	}
	if page.Offset != 550 {
		t.Fatalf("offset = %d, want 550", page.Offset)
	}
	if !page.HasMore || page.NextOffset != 575 {
		t.Fatalf("unexpected pagination metadata: %+v", page)
	}
	if len(page.Entries) != 25 {
		t.Fatalf("entries = %d, want 25", len(page.Entries))
	}
	if first := page.Entries[0].Text; first != "reply-550" {
		t.Fatalf("first entry = %q, want reply-550", first)
	}
	if last := page.Entries[len(page.Entries)-1].Text; last != "reply-574" {
		t.Fatalf("last entry = %q, want reply-574", last)
	}
}
