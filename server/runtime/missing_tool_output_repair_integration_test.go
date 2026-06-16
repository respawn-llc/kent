package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/shared/toolspec"
	"core/shared/transcript"
)

func TestNormalGenerationHTTP400RepairsMissingToolOutputRebuildsAndRetries(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "missing", Name: "exec", Input: json.RawMessage(`{}`)}}})
	client := &fakeClient{
		errors: []error{&llm.APIStatusError{StatusCode: 400, Body: "bad request"}},
		responses: []llm.Response{{
			Assistant: llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "repaired"},
			Usage:     llm.Usage{InputTokens: 10, OutputTokens: 2, WindowTokens: 100},
		}},
	}
	events := make([]Event, 0)
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model:   "gpt-5",
		OnEvent: func(evt Event) { events = append(events, evt) },
	})

	msg, err := eng.SubmitUserMessage(context.Background(), "continue")
	if err != nil {
		t.Fatalf("submit user message: %v", err)
	}
	if msg.Content != "repaired" {
		t.Fatalf("assistant content = %q, want repaired", msg.Content)
	}
	if len(client.calls) != 2 {
		t.Fatalf("model calls = %d, want 2", len(client.calls))
	}
	if !repairItemsContainCall(client.calls[0].Items, "missing") {
		t.Fatalf("first request should include corrupted call, got %+v", client.calls[0].Items)
	}
	if repairItemsContainCall(client.calls[1].Items, "missing") {
		t.Fatalf("retry request should be rebuilt without corrupted call, got %+v", client.calls[1].Items)
	}
	if repairItemsContainCall(eng.snapshotItems(), "missing") {
		t.Fatalf("runtime projection still contains missing call: %+v", eng.snapshotItems())
	}
	warnings := 0
	for _, event := range readRepairEvents(t, store) {
		if event.Kind != "local_entry" {
			continue
		}
		warnings++
	}
	if warnings != 1 {
		t.Fatalf("repair warnings = %d, want 1", warnings)
	}
	liveWarnings := 0
	for _, event := range events {
		if event.Kind == EventLocalEntryAdded &&
			event.LocalEntry != nil &&
			event.LocalEntry.Role == string(transcript.EntryRoleDeveloperErrorFeedback) &&
			strings.TrimSpace(event.LocalEntry.Text) != "" {
			liveWarnings++
		}
	}
	if liveWarnings != 1 {
		t.Fatalf("live repair warnings = %d, want 1; events=%+v", liveWarnings, events)
	}
}

func TestReviewerHTTP400DoesNotRepairMainTranscript(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "missing-review", Name: "exec", Input: json.RawMessage(`{}`)}}})
	reviewerClient := &fakeClient{
		errors: []error{&llm.ProviderAPIError{ProviderID: "openai", StatusCode: 400, Message: "bad request"}},
	}
	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(), Config{Model: "gpt-5", Reviewer: ReviewerConfig{Model: "gpt-5"}})
	if !repairItemsContainCall(eng.snapshotItems(), "missing-review") {
		t.Fatalf("expected pre-review runtime projection to contain corrupted call")
	}

	if _, err := eng.runReviewerSuggestions(context.Background(), "review", reviewerClient); err == nil {
		t.Fatal("expected reviewer request error")
	}
	if len(reviewerClient.calls) != 1 {
		t.Fatalf("reviewer calls = %d, want 1", len(reviewerClient.calls))
	}
	if !repairItemsContainCall(eng.snapshotItems(), "missing-review") {
		t.Fatalf("reviewer 400 repaired main transcript")
	}
}

func TestCompactionHTTP400RepairsMissingToolOutputRebuildsAndRetries(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "missing-compact", Name: "exec", Input: json.RawMessage(`{}`)}}})
	client := &fakeCompactionClient{
		compactionErrors: []error{&llm.APIStatusError{StatusCode: 400, Body: "bad request"}},
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
	if !repairItemsContainCall(client.compactionCalls[0].InputItems, "missing-compact") {
		t.Fatalf("first compaction request should include corrupted call, got %+v", client.compactionCalls[0].InputItems)
	}
	if repairItemsContainCall(client.compactionCalls[1].InputItems, "missing-compact") {
		t.Fatalf("retry compaction request should be rebuilt without corrupted call, got %+v", client.compactionCalls[1].InputItems)
	}
}

func TestRemoteCompactionHTTP400RepairDoesNotConsumeOverflowBudget(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairOverflowSeed(t, store, "missing-budget")
	client := &fakeCompactionClient{
		compactionErrors: []error{
			repairContextOverflowError(),
			repairContextOverflowError(),
			repairContextOverflowError(),
			&llm.APIStatusError{StatusCode: 400, Body: "bad request"},
		},
		compactionResponses: []llm.CompactionResponse{{
			OutputItems: []llm.ResponseItem{{Type: llm.ResponseItemTypeMessage, Role: llm.RoleUser, Content: "summary"}},
		}},
	}
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-5", ContextWindowTokens: 100_000})
	baseRequest := llm.CompactionRequest{Model: "gpt-5", SessionID: store.Meta().SessionID, InputItems: eng.snapshotItems()}

	if _, _, _, err := eng.compactWithContextRepairRetry(context.Background(), "compact", client, baseRequest); err != nil {
		t.Fatalf("compact with repair retry: %v", err)
	}
	if len(client.compactionCalls) != 5 {
		t.Fatalf("compaction calls = %d, want 5", len(client.compactionCalls))
	}
	finalInput := client.compactionCalls[4].InputItems
	if repairItemsContainCall(finalInput, "missing-budget") {
		t.Fatalf("final compaction retry still contains repaired call: %+v", finalInput)
	}
	if !repairItemsContainCollapsedShellOutput(finalInput) {
		t.Fatalf("final compaction retry lost overflow repair: %+v", finalInput)
	}
}

func TestLocalCompactionHTTP400RepairDoesNotConsumeOverflowBudget(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairOverflowSeed(t, store, "missing-local-budget")
	client := &fakeCompactionClient{
		errors: []error{
			repairContextOverflowError(),
			repairContextOverflowError(),
			repairContextOverflowError(),
			&llm.APIStatusError{StatusCode: 400, Body: "bad request"},
			nil,
		},
		responses: []llm.Response{{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "local summary"},
		}},
	}
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-5", ContextWindowTokens: 100_000})

	if _, _, err := eng.localCompactionSummaryWithRepair(context.Background(), eng.snapshotItems(), "summarize", compactionModeAuto); err != nil {
		t.Fatalf("local compaction summary: %v", err)
	}
	if len(client.calls) != 5 {
		t.Fatalf("local compaction calls = %d, want 5", len(client.calls))
	}
	finalInput := client.calls[4].Items
	if repairItemsContainCall(finalInput, "missing-local-budget") {
		t.Fatalf("final local compaction retry still contains repaired call: %+v", finalInput)
	}
	if !repairItemsContainCollapsedShellOutput(finalInput) {
		t.Fatalf("final local compaction retry lost overflow repair: %+v", finalInput)
	}
}

func TestExactTokenCountHTTP400RepairsBeforeDiagnosticAndRetries(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "missing-count", Name: "exec", Input: json.RawMessage(`{}`)}}})
	client := &repairingTokenCountClient{
		errors: []error{&llm.APIStatusError{StatusCode: 400, Body: "bad request"}},
		counts: []int{123},
	}
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-5"})

	count, ok := eng.currentInputTokensPreciselyTrackedWithRepair(context.Background())
	if !ok || count != 123 {
		t.Fatalf("precise count = %d ok=%v, want 123 true", count, ok)
	}
	if len(client.requests) != 2 {
		t.Fatalf("token-count calls = %d, want 2", len(client.requests))
	}
	if !repairItemsContainCall(client.requests[0].Items, "missing-count") {
		t.Fatalf("first token-count request should include corrupted call, got %+v", client.requests[0].Items)
	}
	if repairItemsContainCall(client.requests[1].Items, "missing-count") {
		t.Fatalf("retry token-count request should be rebuilt without corrupted call, got %+v", client.requests[1].Items)
	}
	for _, event := range readRepairEvents(t, store) {
		if event.Kind != "local_entry" {
			continue
		}
		var entry storedLocalEntry
		if err := json.Unmarshal(event.Payload, &entry); err != nil {
			t.Fatalf("decode local entry: %v", err)
		}
		if entry.DiagnosticKey == preciseTokenCountFailureDiagnostic {
			t.Fatalf("did not expect exact-token diagnostic when repair retry succeeds: %+v", entry)
		}
	}
}

func appendRepairOverflowSeed(t *testing.T, store *session.Store, missingCallID string) {
	t.Helper()
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleUser, Content: "seed"})
	for idx := 0; idx < 4; idx++ {
		callID := fmt.Sprintf("shell-budget-%d", idx)
		appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID:    callID,
			Name:  string(toolspec.ToolExecCommand),
			Input: json.RawMessage(`{"cmd":"echo hi"}`),
		}}})
		appendRepairEvent(t, store, "message", llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: callID,
			Name:       string(toolspec.ToolExecCommand),
			Content:    `{"output":"` + strings.Repeat("x", 48_000) + `"}`,
		})
	}
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: missingCallID, Name: "exec", Input: json.RawMessage(`{}`)}}})
}

func repairContextOverflowError() error {
	return &llm.ProviderAPIError{ProviderID: "openai", StatusCode: 413, Code: llm.UnifiedErrorCodeContextLengthOverflow, ProviderCode: "context_length_exceeded", Message: "prompt exceeded"}
}

func repairItemsContainCollapsedShellOutput(items []llm.ResponseItem) bool {
	for _, item := range items {
		if item.Type == llm.ResponseItemTypeFunctionCallOutput && isCollapsedCompactionOverflowShellOutput(item.Output) {
			return true
		}
	}
	return false
}

func TestIneligibleActiveToolTokenCountHTTP400DoesNotRepairButLaterGenerationCan(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "active-call", Name: "exec", Input: json.RawMessage(`{}`)}}})
	countClient := &repairingTokenCountClient{errors: []error{&llm.APIStatusError{StatusCode: 400, Body: "bad request"}}}
	eng := mustNewTestEngine(t, store, countClient, tools.NewRegistry(), Config{Model: "gpt-5"})
	eng.rememberPendingToolCallStarts(map[string]int{"active-call": 0})

	if count, ok := eng.currentInputTokensPreciselyTrackedWithRepair(context.Background()); ok || count != 0 {
		t.Fatalf("active token count = %d ok=%v, want fallback", count, ok)
	}
	if !repairItemsContainCall(eng.snapshotItems(), "active-call") {
		t.Fatalf("active call was repaired while pending")
	}

	eng.forgetPendingToolCallStart("active-call")
	modelClient := &fakeClient{
		errors: []error{&llm.APIStatusError{StatusCode: 400, Body: "bad request"}},
		responses: []llm.Response{{
			Assistant: llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "done"},
			Usage:     llm.Usage{InputTokens: 10, OutputTokens: 1, WindowTokens: 100},
		}},
	}
	eng.llm = modelClient
	if _, err := eng.SubmitUserMessage(context.Background(), "continue"); err != nil {
		t.Fatalf("submit after active tool ended: %v", err)
	}
	if repairItemsContainCall(eng.snapshotItems(), "active-call") {
		t.Fatalf("call was not repaired from later safe generation boundary")
	}
}

func TestCurrentTokenCountWithoutRepairEligibilityDoesNotRepairHTTP400(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "preflight-call", Name: "exec", Input: json.RawMessage(`{}`)}}})
	client := &repairingTokenCountClient{errors: []error{&llm.APIStatusError{StatusCode: 400, Body: "bad request"}}}
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-5"})

	if count, ok := eng.currentInputTokensPreciselyTracked(context.Background()); ok || count != 0 {
		t.Fatalf("preflight token count = %d ok=%v, want fallback", count, ok)
	}
	if !repairItemsContainCall(eng.snapshotItems(), "preflight-call") {
		t.Fatalf("preflight token count repaired without explicit eligibility")
	}
}

func TestNormalGenerationNon400AndUnrelated400DoNotRepair(t *testing.T) {
	for _, tc := range []struct {
		name    string
		seedBad bool
		err     error
	}{
		{name: "non 400", seedBad: true, err: &llm.APIStatusError{StatusCode: 404, Body: "not found"}},
		{name: "unrelated 400", seedBad: false, err: &llm.APIStatusError{StatusCode: 400, Body: "bad request"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := mustCreateTestSession(t)
			if tc.seedBad {
				appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "missing-no-repair", Name: "exec", Input: json.RawMessage(`{}`)}}})
			}
			client := &fakeClient{errors: []error{tc.err}}
			eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-5"})
			if _, err := eng.SubmitUserMessage(context.Background(), "continue"); err == nil {
				t.Fatal("expected model error")
			}
			if tc.seedBad && !repairItemsContainCall(eng.snapshotItems(), "missing-no-repair") {
				t.Fatalf("non-400 repaired corrupted call")
			}
			for _, event := range readRepairEvents(t, store) {
				if event.Kind == "local_entry" {
					t.Fatalf("unexpected local repair warning for %s", tc.name)
				}
			}
		})
	}
}

func TestNormalGenerationHTTP400AfterStreamingDeltaClearsOngoingBeforeRetry(t *testing.T) {
	store := mustCreateTestSession(t)
	appendRepairEvent(t, store, "message", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "missing-stream", Name: "exec", Input: json.RawMessage(`{}`)}}})
	client := &repairingStreamClient{}
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-5"})

	if _, err := eng.SubmitUserMessage(context.Background(), "continue"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if got := eng.ChatSnapshot().Ongoing; got != "" {
		t.Fatalf("ongoing streaming state = %q, want cleared", got)
	}
	if client.calls != 2 {
		t.Fatalf("stream calls = %d, want 2", client.calls)
	}
}

type repairingTokenCountClient struct {
	errors   []error
	counts   []int
	requests []llm.Request
}

type repairingStreamClient struct {
	calls int
}

func (c *repairingStreamClient) Generate(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, nil
}

func (c *repairingStreamClient) GenerateStream(_ context.Context, _ llm.Request, onDelta func(string)) (llm.Response, error) {
	c.calls++
	if c.calls == 1 {
		onDelta("partial")
		return llm.Response{}, &llm.APIStatusError{StatusCode: 400, Body: "bad request"}
	}
	return llm.Response{
		Assistant: llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "done"},
		Usage:     llm.Usage{InputTokens: 10, OutputTokens: 1, WindowTokens: 100},
	}, nil
}

func (c *repairingStreamClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true, SupportsPromptCacheKey: true}, nil
}

func (c *repairingTokenCountClient) Generate(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, nil
}

func (c *repairingTokenCountClient) CountRequestInputTokens(_ context.Context, req llm.Request) (int, error) {
	c.requests = append(c.requests, req)
	if len(c.errors) > 0 {
		err := c.errors[0]
		c.errors = c.errors[1:]
		if err != nil {
			return 0, err
		}
	}
	if len(c.counts) == 0 {
		return 0, nil
	}
	count := c.counts[0]
	c.counts = c.counts[1:]
	return count, nil
}

func (c *repairingTokenCountClient) SupportsRequestInputTokenCount(context.Context) (bool, error) {
	return true, nil
}

func (c *repairingTokenCountClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilities{
		ProviderID:                     "openai",
		SupportsResponsesAPI:           true,
		SupportsRequestInputTokenCount: true,
		SupportsPromptCacheKey:         true,
	}, nil
}
