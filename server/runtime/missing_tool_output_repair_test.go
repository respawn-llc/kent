package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/shared/transcript"
)

func appendRepairEvent(t *testing.T, store *session.Store, kind string, payload any) session.Event {
	t.Helper()
	event, _, err := store.AppendEvent("step", kind, payload)
	if err != nil {
		t.Fatalf("append %q event: %v", kind, err)
	}
	return event
}

func readRepairEvents(t *testing.T, store *session.Store) []session.Event {
	t.Helper()
	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	return events
}

func repairItemsContainCall(items []llm.ResponseItem, callID string) bool {
	for _, item := range items {
		if strings.TrimSpace(item.CallID) == callID && isToolCallItem(item.Type) {
			return true
		}
	}
	return false
}

func repairItemsContainOutput(items []llm.ResponseItem, callID string) bool {
	for _, item := range items {
		if strings.TrimSpace(item.CallID) == callID && isToolOutputItem(item.Type) {
			return true
		}
	}
	return false
}

func countPersistedLocalEntries(events []session.Event) int {
	count := 0
	for _, event := range events {
		if event.Kind == "local_entry" {
			count++
		}
	}
	return count
}

// On a provider HTTP 400 caused by an interrupted tool call without output, the
// repair appends a synthetic completion (it does not remove the call) and the
// retry succeeds with the call still present and now answered.
func TestMissingToolOutputRepairAppendsSyntheticOutputAndRetries(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{
		Role:      llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{ID: "missing", Name: "exec", Input: json.RawMessage(`{}`)}},
	})
	client := &fakeClient{
		errors: []error{&llm.APIStatusError{StatusCode: 400, Body: "tool call without output"}},
		responses: []llm.Response{{
			Assistant: llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "repaired"},
			Usage:     llm.Usage{InputTokens: 10, OutputTokens: 2, WindowTokens: 100},
		}},
	}
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-5"})

	msg, err := eng.SubmitUserMessage(context.Background(), "continue")
	if err != nil {
		t.Fatalf("submit user message: %v", err)
	}
	if msg.Content != "repaired" {
		t.Fatalf("assistant content = %q, want repaired", msg.Content)
	}
	if len(client.calls) != 2 {
		t.Fatalf("model calls = %d, want 2 (initial 400 + repaired retry)", len(client.calls))
	}
	if !repairItemsContainCall(client.calls[0].Items, "missing") {
		t.Fatalf("first request should include the dangling call, got %+v", client.calls[0].Items)
	}
	if !repairItemsContainCall(client.calls[1].Items, "missing") {
		t.Fatalf("retry request should still include the call (append-only), got %+v", client.calls[1].Items)
	}
	if !repairItemsContainOutput(client.calls[1].Items, "missing") {
		t.Fatalf("retry request should include the synthetic output, got %+v", client.calls[1].Items)
	}
	if !repairItemsContainCall(eng.snapshotItems(), "missing") {
		t.Fatalf("projection must keep the call (not remove it), got %+v", eng.snapshotItems())
	}
	if !repairItemsContainOutput(eng.snapshotItems(), "missing") {
		t.Fatalf("projection must include the synthetic output, got %+v", eng.snapshotItems())
	}
	if got := countPersistedLocalEntries(readRepairEvents(t, store)); got != 1 {
		t.Fatalf("persisted operator warnings = %d, want 1", got)
	}
}

// A 400 that is not caused by a missing tool output (nothing dangling) must
// surface the original error without retrying.
func TestMissingToolOutputRepairLeavesUnrelated400Unrepaired(t *testing.T) {
	store := mustCreateTestSession(t)
	client := &fakeClient{
		errors: []error{
			&llm.APIStatusError{StatusCode: 400, Body: "malformed request"},
		},
	}
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-5"})

	if _, err := eng.SubmitUserMessage(context.Background(), "continue"); err == nil {
		t.Fatal("expected the unrelated 400 to surface as an error")
	}
	if len(client.calls) != 1 {
		t.Fatalf("model calls = %d, want 1 (no repair retry)", len(client.calls))
	}
}

func TestRepairMissingToolOutputsByAppendingIsIdempotent(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{
		Role:      llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{ID: "missing", Name: "exec", Input: json.RawMessage(`{}`)}},
	})
	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(), Config{Model: "gpt-5"})

	repaired, err := eng.repairMissingToolOutputsByAppending("step")
	if err != nil {
		t.Fatalf("first repair: %v", err)
	}
	if repaired != 1 {
		t.Fatalf("first repair count = %d, want 1", repaired)
	}
	repaired, err = eng.repairMissingToolOutputsByAppending("step")
	if err != nil {
		t.Fatalf("second repair: %v", err)
	}
	if repaired != 0 {
		t.Fatalf("second repair count = %d, want 0 (already closed)", repaired)
	}
}

// The resume path re-executes interrupted calls; the repair must defer while
// pending tool-call starts remain so it never pre-empts a real output.
func TestRepairMissingToolOutputsDefersToPendingToolCallStarts(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{
		Role:      llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{ID: "missing", Name: "exec", Input: json.RawMessage(`{}`)}},
	})
	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(), Config{Model: "gpt-5"})
	eng.rememberPendingToolCallStarts(map[string]int{"missing": 1})

	repaired, err := eng.repairMissingToolOutputsByAppending("step")
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if repaired != 0 {
		t.Fatalf("repair count = %d, want 0 while a pending start exists", repaired)
	}
}

func TestRepairMissingToolOutputsMarksResultAsError(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{
		Role:      llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{ID: "missing", Name: "exec", Input: json.RawMessage(`{}`)}},
	})
	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(), Config{Model: "gpt-5"})

	if _, err := eng.repairMissingToolOutputsByAppending("step"); err != nil {
		t.Fatalf("repair: %v", err)
	}
	var completion *storedToolCompletion
	for _, event := range readRepairEvents(t, store) {
		if event.Kind != "tool_completed" {
			continue
		}
		var stored storedToolCompletion
		if err := json.Unmarshal(event.Payload, &stored); err != nil {
			t.Fatalf("decode completion: %v", err)
		}
		if stored.CallID == "missing" {
			completion = &stored
		}
	}
	if completion == nil {
		t.Fatal("expected a persisted synthetic completion for the dangling call")
	}
	if !completion.IsError {
		t.Fatal("synthetic completion should be marked as an error result")
	}
}

// A compaction request that 400s on a dangling tool call is repaired by
// appending synthetic outputs and retried; the retry keeps the call and adds its
// output.
func TestCompactionMissingToolOutputRepairAppendsAndRetries(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{
		Role:      llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{ID: "missing-compact", Name: "exec", Input: json.RawMessage(`{}`)}},
	})
	client := &fakeCompactionClient{
		compactionErrors: []error{&llm.APIStatusError{StatusCode: 400, Body: "tool call without output"}},
		compactionResponses: []llm.CompactionResponse{{
			OutputItems: []llm.ResponseItem{{Type: llm.ResponseItemTypeMessage, Role: llm.RoleUser, Content: "summary"}},
			Usage:       llm.Usage{InputTokens: 10, OutputTokens: 2, WindowTokens: 100},
		}},
	}
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-5"})
	baseRequest := llm.CompactionRequest{Model: "gpt-5", SessionID: store.Meta().SessionID, InputItems: eng.snapshotItems()}

	if _, _, _, err := eng.compactWithContextRepairRetry(context.Background(), "compact", client, baseRequest); err != nil {
		t.Fatalf("compact with repair retry: %v", err)
	}
	if len(client.compactionCalls) != 2 {
		t.Fatalf("compaction calls = %d, want 2", len(client.compactionCalls))
	}
	retryInput := client.compactionCalls[1].InputItems
	if !repairItemsContainCall(retryInput, "missing-compact") {
		t.Fatalf("retry compaction request should still include the call, got %+v", retryInput)
	}
	if !repairItemsContainOutput(retryInput, "missing-compact") {
		t.Fatalf("retry compaction request should include the synthetic output, got %+v", retryInput)
	}
}

// A repeated 400 must not loop: the repair runs a single pass, then the error
// surfaces.
func TestCompactionMissingToolOutputRepairRunsSinglePass(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{
		Role:      llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{ID: "missing-compact", Name: "exec", Input: json.RawMessage(`{}`)}},
	})
	client := &fakeCompactionClient{
		compactionErrors: []error{
			&llm.APIStatusError{StatusCode: 400, Body: "bad request"},
			&llm.APIStatusError{StatusCode: 400, Body: "bad request"},
		},
	}
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-5"})
	baseRequest := llm.CompactionRequest{Model: "gpt-5", SessionID: store.Meta().SessionID, InputItems: eng.snapshotItems()}

	_, _, _, err := eng.compactWithContextRepairRetry(context.Background(), "compact", client, baseRequest)
	if !llm.HasHTTPStatus(err, 400) {
		t.Fatalf("compaction error = %v, want HTTP 400 to surface", err)
	}
	if len(client.compactionCalls) != 2 {
		t.Fatalf("compaction calls = %d, want single repair retry", len(client.compactionCalls))
	}
}

func TestRepairWarningUsesOperatorFacingRole(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{
		Role:      llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{ID: "missing", Name: "exec", Input: json.RawMessage(`{}`)}},
	})
	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(), Config{Model: "gpt-5"})

	if _, err := eng.repairMissingToolOutputsByAppending("step"); err != nil {
		t.Fatalf("repair: %v", err)
	}
	found := false
	for _, event := range readRepairEvents(t, store) {
		if event.Kind != "local_entry" {
			continue
		}
		var entry storedLocalEntry
		if err := json.Unmarshal(event.Payload, &entry); err != nil {
			t.Fatalf("decode local entry: %v", err)
		}
		if entry.Role == string(transcript.EntryRoleDeveloperErrorFeedback) && strings.TrimSpace(entry.Text) != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected an operator-facing repair warning entry")
	}
}
