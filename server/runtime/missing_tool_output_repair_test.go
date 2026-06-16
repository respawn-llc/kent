package runtime

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/shared/toolspec"
	"core/shared/transcript"
)

func TestMissingToolOutputRepairRemovesUnfinishedFunctionCallAndAppendsWarning(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleUser, Content: "run pwd"})
	appendRepairEvent(t, store, "message", llm.Message{
		Role:      llm.RoleAssistant,
		Content:   "I will run it",
		ToolCalls: []llm.ToolCall{{ID: "call-1", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"command":"pwd"}`)}},
	})

	result, committed, err := repairMissingToolOutputsInSessionStore(store, "repair-step")
	if err != nil {
		t.Fatalf("repair missing tool outputs: %v", err)
	}
	if !committed || !result.Changed || result.RemovedCalls != 1 {
		t.Fatalf("unexpected repair result=%+v committed=%v", result, committed)
	}

	events := readRepairEvents(t, store)
	messages := repairMessagesFromEvents(t, events)
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2: %+v", len(messages), messages)
	}
	if got := messages[1]; got.Content != "I will run it" || len(got.ToolCalls) != 0 {
		t.Fatalf("expected assistant content preserved and calls removed, got %+v", got)
	}
	warning := lastRepairWarning(t, events)
	if warning.Role != string(transcript.EntryRoleDeveloperErrorFeedback) || !strings.Contains(warning.Text, "1 calls") {
		t.Fatalf("unexpected warning: %+v", warning)
	}

	reopened := mustOpenTestSession(t, store.Dir())
	projector := NewTranscriptProjector()
	if err := reopened.WalkEvents(projector.ApplyPersistedEvent); err != nil {
		t.Fatalf("project reopened events: %v", err)
	}
	for _, item := range projector.chat.snapshotItems() {
		if item.Type == llm.ResponseItemTypeFunctionCall && item.CallID == "call-1" {
			t.Fatalf("reopened provider items still contain removed call: %+v", projector.chat.snapshotItems())
		}
	}
}

func TestMissingToolOutputRepairPreservesCompletedSiblingAndDropsEmptyAssistantMessage(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
		{ID: "done", Name: "done", Input: json.RawMessage(`{}`)},
		{ID: "missing", Name: "missing", Input: json.RawMessage(`{}`)},
	}})
	appendRepairEvent(t, store, "tool_completed", storedToolCompletion{CallID: "done", Name: "done", Output: json.RawMessage(`{"ok":true}`)})
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "only-missing", Name: "missing", Input: json.RawMessage(`{}`)}}})

	result, committed, err := repairMissingToolOutputsInSessionStore(store, "repair")
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if !committed || result.RemovedCalls != 2 {
		t.Fatalf("unexpected repair result=%+v committed=%v", result, committed)
	}
	messages := repairMessagesFromEvents(t, readRepairEvents(t, store))
	if len(messages) != 1 {
		t.Fatalf("messages len = %d, want 1: %+v", len(messages), messages)
	}
	if len(messages[0].ToolCalls) != 1 || messages[0].ToolCalls[0].ID != "done" {
		t.Fatalf("expected completed sibling only, got %+v", messages[0].ToolCalls)
	}
}

func TestMissingToolOutputRepairPreservesReasoningAndRecognizesMatchingMaterializedOutput(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{
		Role:           llm.RoleAssistant,
		Content:        "thinking done",
		ReasoningItems: []llm.ReasoningItem{{ID: "rs-1", EncryptedContent: "encrypted"}},
		ToolCalls: []llm.ToolCall{
			{ID: "matched", Name: "matched", Input: json.RawMessage(`{}`)},
			{ID: "missing", Name: "missing", Input: json.RawMessage(`{}`)},
		},
	})
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleTool, ToolCallID: "matched", Content: `{"ok":true}`})

	result, _, err := repairMissingToolOutputsInSessionStore(store, "repair")
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if result.RemovedCalls != 1 {
		t.Fatalf("removed calls = %d, want 1", result.RemovedCalls)
	}
	messages := repairMessagesFromEvents(t, readRepairEvents(t, store))
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2: %+v", len(messages), messages)
	}
	assistant := messages[0]
	if assistant.Content != "thinking done" || len(assistant.ReasoningItems) != 1 {
		t.Fatalf("expected content and reasoning preserved, got %+v", assistant)
	}
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "matched" {
		t.Fatalf("expected matching materialized call preserved, got %+v", assistant.ToolCalls)
	}
}

func TestMissingToolOutputRepairHandlesCustomToolOutputKindAndMaterializedPrecedence(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
		{ID: "custom-ok", Name: "custom", Custom: true, CustomInput: "input"},
		{ID: "custom-bad", Name: "custom", Custom: true, CustomInput: "input"},
	}})
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleTool, MessageType: llm.MessageTypeCustomToolCallOutput, ToolCallID: "custom-ok", Content: "ok"})
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleTool, ToolCallID: "custom-bad", Content: "wrong kind"})
	appendRepairEvent(t, store, "tool_completed", storedToolCompletion{CallID: "custom-bad", Name: "custom", Output: json.RawMessage(`"ok"`)})

	result, _, err := repairMissingToolOutputsInSessionStore(store, "repair")
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if result.RemovedCalls != 1 {
		t.Fatalf("removed calls = %d, want 1", result.RemovedCalls)
	}
	messages := repairMessagesFromEvents(t, readRepairEvents(t, store))
	var toolOutputs []llm.Message
	for _, msg := range messages {
		if msg.Role == llm.RoleTool {
			toolOutputs = append(toolOutputs, msg)
		}
	}
	if len(messages[0].ToolCalls) != 1 || messages[0].ToolCalls[0].ID != "custom-ok" {
		t.Fatalf("expected only matching custom call preserved, got %+v", messages[0].ToolCalls)
	}
	if len(toolOutputs) != 1 || toolOutputs[0].ToolCallID != "custom-ok" {
		t.Fatalf("expected mismatched materialized output removed, got %+v", toolOutputs)
	}
}

func TestMissingToolOutputRepairTreatsToolCompletedOrderInsensitively(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "tool_completed", storedToolCompletion{CallID: "call-1", Name: "exec", Output: json.RawMessage(`{"ok":true}`)})
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "exec", Input: json.RawMessage(`{}`)}}})

	result, committed, err := repairMissingToolOutputsInSessionStore(store, "repair")
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if committed || result.Changed || result.RemovedCalls != 0 {
		t.Fatalf("expected no-op repair, got result=%+v committed=%v", result, committed)
	}
}

func TestMissingToolOutputRepairValidatesToolCompletedProviderItemsAgainstOuterCall(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
		{ID: "call-1", Name: "exec", Input: json.RawMessage(`{}`)},
		{ID: "call-2", Name: "exec", Input: json.RawMessage(`{}`)},
	}})
	appendRepairEvent(t, store, "tool_completed", storedToolCompletion{
		CallID: "call-1",
		Name:   "exec",
		ProviderItems: []llm.ResponseItem{{
			Type:   llm.ResponseItemTypeFunctionCallOutput,
			CallID: "call-1",
			Output: json.RawMessage(`{"ok":true}`),
		}},
	})
	appendRepairEvent(t, store, "tool_completed", storedToolCompletion{
		CallID: "other",
		Name:   "exec",
		ProviderItems: []llm.ResponseItem{{
			Type:   llm.ResponseItemTypeFunctionCallOutput,
			CallID: "call-2",
			Output: json.RawMessage(`{"ok":true}`),
		}},
	})

	result, _, err := repairMissingToolOutputsInSessionStore(store, "repair")
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if result.RemovedCalls != 1 {
		t.Fatalf("removed calls = %d, want 1", result.RemovedCalls)
	}
	messages := repairMessagesFromEvents(t, readRepairEvents(t, store))
	if len(messages[0].ToolCalls) != 1 || messages[0].ToolCalls[0].ID != "call-1" {
		t.Fatalf("expected only outer matching provider item call preserved, got %+v", messages[0].ToolCalls)
	}
}

func TestMissingToolOutputRepairRespectsLatestValidHistoryReplacementBoundary(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "before", Name: "exec", Input: json.RawMessage(`{}`)}}})
	appendRepairEvent(t, store, "history_replaced", historyReplacementPayload{Items: llm.ItemsFromMessages([]llm.Message{{Role: llm.RoleUser, Content: "summary"}})})
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "after", Name: "exec", Input: json.RawMessage(`{}`)}}})

	result, _, err := repairMissingToolOutputsInSessionStore(store, "repair")
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if result.RemovedCalls != 1 {
		t.Fatalf("removed calls = %d, want 1", result.RemovedCalls)
	}
	messages := repairMessagesFromEvents(t, readRepairEvents(t, store))
	if len(messages) != 1 || len(messages[0].ToolCalls) != 1 || messages[0].ToolCalls[0].ID != "before" {
		t.Fatalf("expected pre-boundary missing call untouched and post-boundary call removed, got %+v", messages)
	}
}

func TestMissingToolOutputRepairIgnoresLegacyHistoryReplacementBoundary(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "before-legacy", Name: "exec", Input: json.RawMessage(`{}`)}}})
	appendRepairEvent(t, store, "history_replaced", historyReplacementPayload{Engine: legacyHistoryReplacementEngineReviewerRollback, Items: llm.ItemsFromMessages([]llm.Message{{Role: llm.RoleUser, Content: "ignored"}})})

	result, _, err := repairMissingToolOutputsInSessionStore(store, "repair")
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if result.RemovedCalls != 1 {
		t.Fatalf("removed calls = %d, want 1", result.RemovedCalls)
	}
}

func TestMissingToolOutputRepairAbortsMalformedHistoryReplacementBeforeCommit(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "exec", Input: json.RawMessage(`{}`)}}})
	_, _, err := store.AppendEvent("bad", "history_replaced", map[string]any{"items": "not response items"})
	if err != nil {
		t.Fatalf("append malformed boundary: %v", err)
	}

	result, committed, err := repairMissingToolOutputsInSessionStore(store, "repair")
	if err == nil || !errors.Is(err, errDecodeHistoryReplacedEvent) {
		t.Fatalf("expected malformed history replacement error, got %v", err)
	}
	if committed || result.Changed {
		t.Fatalf("expected no commit on malformed boundary, got result=%+v committed=%v", result, committed)
	}
	messages := repairMessagesFromEvents(t, readRepairEvents(t, store))
	if len(messages) != 1 || len(messages[0].ToolCalls) != 1 {
		t.Fatalf("expected original call preserved after abort, got %+v", messages)
	}
}

func TestMissingToolOutputRepairAfterHTTP400ReloadsRuntimeProjectionAndDerivedState(t *testing.T) {
	store := mustCreateTestSession(t)
	if err := store.SetUsageState(&session.UsageState{InputTokens: 42, WindowTokens: 100, EstimatedProviderTokens: 42}); err != nil {
		t.Fatalf("set usage state: %v", err)
	}
	appendRepairEvent(t, store, "history_replaced", historyReplacementPayload{WorkflowRunID: "workflow-1", Items: llm.ItemsFromMessages([]llm.Message{{Role: llm.RoleUser, Content: "summary"}})})
	appendRepairEvent(t, store, "local_entry", storedLocalEntry{Role: "error", Text: "Exact token counting failed", DiagnosticKey: "precise-token-count"})
	appendRepairEvent(t, store, sessionEventCacheResponseObserved, persistedCacheResponseObserved{CacheKey: "cache-key", ChunkCount: 1, TerminalHash: "hash", HasCachedInputTokens: true, CachedInputTokens: 7})
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeCompactionSoonReminder, Content: "compact soon"})
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "missing", Name: "exec", Input: json.RawMessage(`{}`)}}})

	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(), Config{Model: "gpt-5"})
	if !repairItemsContainCall(eng.snapshotItems(), "missing") {
		t.Fatalf("expected pre-repair runtime projection to contain missing call")
	}
	eng.compactionRuntimeState().SetCount(0)
	eng.setLastCompactionWorkflowRunID("")
	eng.resetLocalDiagnostics()
	eng.setCompactionSoonReminderIssued(false)
	eng.baseMetaInjected = false
	eng.modelRequests().mu.Lock()
	eng.modelRequests().requestCache = newRequestCacheTracker()
	eng.modelRequests().mu.Unlock()

	result, committed, err := eng.repairMissingToolOutputsAfterHTTP400("repair")
	if err != nil {
		t.Fatalf("repair after http 400: %v", err)
	}
	if !committed || !result.Changed || result.RemovedCalls != 1 {
		t.Fatalf("unexpected repair result=%+v committed=%v", result, committed)
	}
	if repairItemsContainCall(eng.snapshotItems(), "missing") {
		t.Fatalf("runtime projection still contains missing call after repair: %+v", eng.snapshotItems())
	}
	if repairSnapshotContainsToolCall(eng.ChatSnapshot(), "missing") {
		t.Fatalf("chat snapshot still contains missing call after repair: %+v", eng.ChatSnapshot())
	}
	if !repairSnapshotContainsText(eng.ChatSnapshot(), "Transcript history was rolled back 1 calls") {
		t.Fatalf("chat snapshot missing repair warning: %+v", eng.ChatSnapshot())
	}
	if got := eng.compactionCountSnapshot(); got != 1 {
		t.Fatalf("compaction count = %d, want 1", got)
	}
	if got := eng.LastCompactionWorkflowRunID(); got != "workflow-1" {
		t.Fatalf("last workflow compaction id = %q, want workflow-1", got)
	}
	if !eng.hasPersistedDiagnostic("precise-token-count") {
		t.Fatal("expected local diagnostic dedupe state restored")
	}
	if !eng.compactionRuntimeState().SoonReminderIssued() {
		t.Fatal("expected compaction-soon reminder state restored")
	}
	if !eng.baseMetaInjected {
		t.Fatal("expected base meta injected signal restored")
	}
	eng.modelRequests().RequestCache().mu.Lock()
	_, hasCacheLineage := eng.modelRequests().RequestCache().lineage["cache-key"]
	eng.modelRequests().RequestCache().mu.Unlock()
	if !hasCacheLineage {
		t.Fatal("expected prompt-cache response lineage restored")
	}
	if store.Meta().UsageState != nil {
		t.Fatalf("expected persisted usage state cleared after repair, got %+v", store.Meta().UsageState)
	}
	if got := eng.lastUsageSnapshot(); got.InputTokens != 0 || got.WindowTokens != 0 {
		t.Fatalf("expected runtime usage tracking reset, got %+v", got)
	}

	reopened := mustOpenTestSession(t, store.Dir())
	restored := mustNewTestEngine(t, reopened, &fakeClient{}, tools.NewRegistry(), Config{Model: "gpt-5"})
	if repairItemsContainCall(restored.snapshotItems(), "missing") {
		t.Fatalf("reopened projection still contains missing call: %+v", restored.snapshotItems())
	}
}

func TestMissingToolOutputRepairAfterHTTP400EmitsAppendOnlyWarningEvent(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "missing", Name: "exec", Input: json.RawMessage(`{}`)}}})
	liveEvents := make([]Event, 0)
	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(), Config{
		Model:   "gpt-5",
		OnEvent: func(evt Event) { liveEvents = append(liveEvents, evt) },
	})
	preRepairCount := eng.CommittedTranscriptEntryCount()
	if preRepairCount != 1 {
		t.Fatalf("pre-repair committed count = %d, want 1", preRepairCount)
	}

	result, committed, err := eng.repairMissingToolOutputsAfterHTTP400("repair")
	if err != nil {
		t.Fatalf("repair after http 400: %v", err)
	}
	if !committed || !result.Changed {
		t.Fatalf("unexpected repair result=%+v committed=%v", result, committed)
	}
	warningEvents := make([]Event, 0)
	for _, evt := range liveEvents {
		if evt.Kind == EventLocalEntryAdded && evt.LocalEntry != nil && strings.Contains(evt.LocalEntry.Text, "Transcript history was rolled back") {
			warningEvents = append(warningEvents, evt)
		}
	}
	if len(warningEvents) != 1 {
		t.Fatalf("warning event count = %d, want 1: %+v", len(warningEvents), liveEvents)
	}
	warning := warningEvents[0]
	if !warning.CommittedEntryStartSet || warning.CommittedEntryStart != preRepairCount {
		t.Fatalf("warning start=%d set=%t, want append at %d", warning.CommittedEntryStart, warning.CommittedEntryStartSet, preRepairCount)
	}
	if warning.CommittedEntryCount != preRepairCount+1 {
		t.Fatalf("warning committed count = %d, want %d", warning.CommittedEntryCount, preRepairCount+1)
	}
	if warning.TranscriptRevision != result.Rewrite.LastSequence {
		t.Fatalf("warning revision = %d, want %d", warning.TranscriptRevision, result.Rewrite.LastSequence)
	}
	localEntries := 0
	for _, event := range readRepairEvents(t, store) {
		if event.Kind == "local_entry" {
			localEntries++
		}
	}
	if localEntries != 1 {
		t.Fatalf("persisted local entries = %d, want exactly one repair warning", localEntries)
	}
}

func appendRepairEvent(t *testing.T, store *session.Store, kind string, payload any) session.Event {
	t.Helper()
	event, _, err := store.AppendEvent("step", kind, payload)
	if err != nil {
		t.Fatalf("append %q event: %v", kind, err)
	}
	return event
}

func repairItemsContainCall(items []llm.ResponseItem, callID string) bool {
	for _, item := range items {
		if item.CallID == callID && (item.Type == llm.ResponseItemTypeFunctionCall || item.Type == llm.ResponseItemTypeCustomToolCall) {
			return true
		}
	}
	return false
}

func repairSnapshotContainsToolCall(snapshot ChatSnapshot, callID string) bool {
	for _, entry := range snapshot.Entries {
		if entry.ToolCallID == callID {
			return true
		}
	}
	return false
}

func repairSnapshotContainsText(snapshot ChatSnapshot, text string) bool {
	for _, entry := range snapshot.Entries {
		if strings.Contains(entry.Text, text) {
			return true
		}
	}
	return false
}

func readRepairEvents(t *testing.T, store *session.Store) []session.Event {
	t.Helper()
	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	return events
}

func repairMessagesFromEvents(t *testing.T, events []session.Event) []llm.Message {
	t.Helper()
	out := make([]llm.Message, 0)
	for _, event := range events {
		if event.Kind != "message" {
			continue
		}
		var msg llm.Message
		if err := json.Unmarshal(event.Payload, &msg); err != nil {
			t.Fatalf("decode message event: %v", err)
		}
		out = append(out, msg)
	}
	return out
}

func lastRepairWarning(t *testing.T, events []session.Event) storedLocalEntry {
	t.Helper()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind != "local_entry" {
			continue
		}
		var entry storedLocalEntry
		if err := json.Unmarshal(events[i].Payload, &entry); err != nil {
			t.Fatalf("decode local entry: %v", err)
		}
		return entry
	}
	t.Fatal("missing repair warning")
	return storedLocalEntry{}
}
