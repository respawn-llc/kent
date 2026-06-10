package runtime

import (
	"builder/shared/cachewarn"
	"builder/shared/toolspec"
	"builder/shared/transcript"
	"context"
	"encoding/json"
	"testing"

	"builder/server/llm"
	"builder/server/tools"
)

func TestCommittedTranscriptChangedMarksOnlyDurableTranscriptMutations(t *testing.T) {
	store := mustCreateTestSession(t)
	events := make([]Event, 0, 16)
	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: fakeTool{name: toolspec.ToolExecCommand}}), Config{
		Model:   "gpt-5",
		OnEvent: func(evt Event) { events = append(events, evt) },
	})

	start := len(events)
	eng.AppendLocalEntry("assistant", "transient local note")
	assertEventFlags(t, events[start:], []eventFlagExpectation{{kind: EventLocalEntryAdded, stepID: "", committedChanged: false}, {kind: EventConversationUpdated, stepID: "", committedChanged: false}})

	start = len(events)
	eng.SetOngoingError("boom")
	assertEventFlags(t, events[start:], []eventFlagExpectation{{kind: EventOngoingErrorUpdated, stepID: "", committedChanged: false}})

	start = len(events)
	eng.ClearOngoingError()
	assertEventFlags(t, events[start:], []eventFlagExpectation{{kind: EventOngoingErrorUpdated, stepID: "", committedChanged: false}})

	start = len(events)
	if err := eng.steer("stream-step", steerClearStreamingStateIntent()); err != nil {
		t.Fatalf("clear streaming state: %v", err)
	}
	assertEventFlags(t, events[start:], []eventFlagExpectation{{kind: EventConversationUpdated, stepID: "stream-step", committedChanged: false}, {kind: EventAssistantDeltaReset, stepID: "stream-step", committedChanged: false}, {kind: EventReasoningDeltaReset, stepID: "stream-step", committedChanged: false}})

	start = len(events)
	if err := eng.steer("persist-step", steerLocalEntryIntent(storedLocalEntry{
		Visibility: transcript.EntryVisibilityAuto,
		Role:       "reviewer_status",
		Text:       "persisted local note",
	})); err != nil {
		t.Fatalf("append persisted local entry: %v", err)
	}
	assertEventFlags(t, events[start:], []eventFlagExpectation{{kind: EventLocalEntryAdded, stepID: "persist-step", committedChanged: true}})

	start = len(events)
	if err := eng.replaceHistory("compact-step", "local", compactionModeManual, llm.ItemsFromMessages([]llm.Message{{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeCompactionSummary, Content: "summary"}})); err != nil {
		t.Fatalf("replace history for compaction: %v", err)
	}
	assertEventFlags(t, events[start:], []eventFlagExpectation{{kind: EventLocalEntryAdded, stepID: "compact-step", committedChanged: true}, {kind: EventConversationUpdated, stepID: "compact-step", committedChanged: false}})

	start = len(events)
	if err := eng.steer("message-step", steerMessageIntent(llm.Message{Role: llm.RoleAssistant, Content: "persisted assistant", Phase: llm.MessagePhaseFinal})); err != nil {
		t.Fatalf("append persisted message: %v", err)
	}
	assertEventFlags(t, events[start:], nil)

	start = len(events)
	if err := eng.steer("goal-step", steerMessageIntent(eng.goalDeveloperMessage("Goal paused.", "Goal paused"))); err != nil {
		t.Fatalf("append goal feedback: %v", err)
	}
	assertEventFlags(t, events[start:], []eventFlagExpectation{{kind: EventConversationUpdated, stepID: "goal-step", committedChanged: true}})

	start = len(events)
	eng.QueueUserMessage("queued input")
	if _, err := eng.flushPendingUserInjections("flush-step"); err != nil {
		t.Fatalf("flush pending user injections: %v", err)
	}
	assertEventFlags(t, events[start:], []eventFlagExpectation{{kind: EventUserMessageFlushed, stepID: "flush-step", committedChanged: true}})

	eng.ensureOrchestrationCollaborators()
	start = len(events)
	if err := eng.observePromptCacheResponse("cache-step", preparedCacheRequestObservation{
		request: persistedCacheRequestObserved{
			DigestVersion: requestCacheDigestVersion,
			CacheKey:      "session-1/cache-key",
			Scope:         cachewarn.ScopeConversation,
		},
		exactWarning: &cachewarn.Warning{
			Scope:  cachewarn.ScopeConversation,
			Reason: cachewarn.ReasonNonPostfix,
		},
		previousCachedInputTokens: 10,
	}, llm.Usage{HasCachedInputTokens: true, CachedInputTokens: 0}); err != nil {
		t.Fatalf("observe prompt cache response: %v", err)
	}
	assertEventFlags(t, events[start:], []eventFlagExpectation{{kind: EventCacheWarning, stepID: "cache-step", committedChanged: true}})

	start = len(events)
	if _, err := eng.executeToolCalls(context.Background(), "tool-step", []llm.ToolCall{{
		ID:    "call-1",
		Name:  string(toolspec.ToolExecCommand),
		Input: json.RawMessage(`{"command":"pwd"}`),
	}}); err != nil {
		t.Fatalf("execute tool calls: %v", err)
	}
	assertEventFlags(t, events[start:], []eventFlagExpectation{{kind: EventToolCallStarted, stepID: "tool-step", committedChanged: true}, {kind: EventToolCallCompleted, stepID: "tool-step", committedChanged: true}})
}

func TestToolResultMirrorMessageDoesNotEmitGenericCommittedAdvance(t *testing.T) {
	store := mustCreateTestSession(t)
	events := make([]Event, 0, 16)
	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: fakeTool{name: toolspec.ToolExecCommand}}), Config{
		Model:   "gpt-5",
		OnEvent: func(evt Event) { events = append(events, evt) },
	})

	call := llm.ToolCall{ID: "call-1", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"command":"pwd"}`)}
	if err := eng.steer("step-1", steerMessageWithoutDerivedEventIntent(llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}})); err != nil {
		t.Fatalf("append assistant message: %v", err)
	}
	result := tools.Result{CallID: call.ID, Name: toolspec.ToolExecCommand, Output: json.RawMessage(`{"output":"/tmp","exit_code":0,"truncated":false}`)}
	if err := eng.steer("step-1", steerToolCompletionIntent(result)); err != nil {
		t.Fatalf("persist tool completion: %v", err)
	}

	start := len(events)
	if err := eng.steer("step-1", steerMessageIntent(llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Name: string(result.Name), Content: string(result.Output)})); err != nil {
		t.Fatalf("append tool mirror message: %v", err)
	}
	if got := events[start:]; len(got) != 0 {
		t.Fatalf("expected no generic committed advance for tool mirror message, got %+v", got)
	}
}

type eventFlagExpectation struct {
	kind             EventKind
	stepID           string
	committedChanged bool
}

func assertEventFlags(t *testing.T, events []Event, expected []eventFlagExpectation) {
	t.Helper()
	if len(events) != len(expected) {
		t.Fatalf("event count = %d, want %d; events=%+v", len(events), len(expected), events)
	}
	for idx, want := range expected {
		got := events[idx]
		if got.Kind != want.kind || got.StepID != want.stepID || got.CommittedTranscriptChanged != want.committedChanged {
			t.Fatalf("event[%d] = {Kind:%s StepID:%q CommittedTranscriptChanged:%t}, want {Kind:%s StepID:%q CommittedTranscriptChanged:%t}", idx, got.Kind, got.StepID, got.CommittedTranscriptChanged, want.kind, want.stepID, want.committedChanged)
		}
	}
}
