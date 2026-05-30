package runtime

import (
	"builder/server/llm"
	"builder/server/session"
	"builder/server/tools"
	"builder/shared/toolspec"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestQueuedUserMessageFlushesWhenAssistantReturnsWithoutTools(t *testing.T) {
	client := &fakeClient{responses: []llm.Response{
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "first"},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "after flush"},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
	}}

	var seenFlushed bool
	eng := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		OnEvent: func(evt Event) {
			if evt.Kind == EventUserMessageFlushed && evt.UserMessage == "steer now" {
				seenFlushed = true
			}
		},
	})

	eng.QueueUserMessage("steer now")
	msg, err := eng.SubmitUserMessage(context.Background(), "start")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if msg.Content != "after flush" {
		t.Fatalf("assistant content = %q, want after flush", msg.Content)
	}
	if !seenFlushed {
		t.Fatal("expected user_message_flushed event")
	}
	if len(client.calls) < 2 {
		t.Fatalf("expected at least 2 model calls, got %d", len(client.calls))
	}
	second := client.calls[1]
	hasInjected := false
	for _, m := range requestMessages(second) {
		if m.Role == llm.RoleUser && m.Content == "steer now" {
			hasInjected = true
			break
		}
	}
	if !hasInjected {
		t.Fatalf("expected flushed user message in second request, messages=%+v", requestMessages(second))
	}
}

func TestModelResponseEventCarriesContextUsage(t *testing.T) {
	client := &fakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{InputTokens: 420, WindowTokens: 1_000},
	}}}
	var usage *ContextUsage
	autoCompactionEnabled := false
	eng := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{
		ContextWindowTokens:   1_000,
		AutoCompactionEnabled: &autoCompactionEnabled,
		OnEvent: func(evt Event) {
			if evt.Kind == EventModelResponse {
				usage = evt.ContextUsage
			}
		},
	})
	if _, err := eng.SubmitUserMessage(context.Background(), "prompt"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if usage == nil {
		t.Fatal("expected model response event to carry context usage")
	}
	if usage.UsedTokens != 420 || usage.WindowTokens != 1_000 {
		t.Fatalf("context usage = %+v, want used=420 window=1000", usage)
	}
}

func TestQueuedUserMessageFlushDoesNotEmitConversationUpdatedForInjectedMessage(t *testing.T) {
	client := &fakeClient{responses: []llm.Response{
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "first"},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "after flush"},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
	}}

	var (
		events     []Event
		eventIndex int
		flushIndex = -1
	)
	eng := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		OnEvent: func(evt Event) {
			events = append(events, evt)
			eventIndex++
			if evt.Kind == EventUserMessageFlushed && evt.UserMessage == "steer now" && flushIndex < 0 {
				flushIndex = eventIndex
			}
		},
	})

	eng.QueueUserMessage("steer now")
	if _, err := eng.SubmitUserMessage(context.Background(), "start"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if flushIndex < 0 {
		t.Fatal("expected user_message_flushed event")
	}
	if got := committedConversationUpdatedCountAfterLastUserFlush(events); got != 0 {
		t.Fatalf("committed conversation_updated count after injected user flush = %d, want 0; events=%+v", got, events)
	}
}

func TestDirectUserMessageFlushDoesNotEmitConversationUpdated(t *testing.T) {
	client := &fakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done"},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}

	var (
		events     []Event
		eventIndex int
		flushIndex = -1
	)
	eng := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		OnEvent: func(evt Event) {
			events = append(events, evt)
			eventIndex++
			if evt.Kind == EventUserMessageFlushed && evt.UserMessage == "say hi" && flushIndex < 0 {
				flushIndex = eventIndex
			}
		},
	})

	if _, err := eng.SubmitUserMessage(context.Background(), "say hi"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if flushIndex < 0 {
		t.Fatal("expected direct user_message_flushed event")
	}
	if got := committedConversationUpdatedCountAfterLastUserFlush(events); got != 0 {
		t.Fatalf("committed conversation_updated count after direct user flush = %d, want 0; events=%+v", got, events)
	}
}

func TestQueuedUserMessagesCoalesceIntoSingleFlush(t *testing.T) {
	client := &fakeClient{responses: []llm.Response{
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "first"},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "after flush"},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
	}}

	var (
		flushCount int
		flushed    Event
	)
	eng := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		OnEvent: func(evt Event) {
			if evt.Kind == EventUserMessageFlushed {
				flushCount++
				flushed = evt
			}
		},
	})

	eng.QueueUserMessage("steer now")
	eng.QueueUserMessage("and keep tests focused")
	msg, err := eng.SubmitUserMessage(context.Background(), "start")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if msg.Content != "after flush" {
		t.Fatalf("assistant content = %q, want after flush", msg.Content)
	}
	if flushed.UserMessage != "steer now\n\nand keep tests focused" {
		t.Fatalf("unexpected flushed user message %q", flushed.UserMessage)
	}
	if len(flushed.UserMessageBatch) != 2 {
		t.Fatalf("expected two flushed user messages in batch, got %+v", flushed.UserMessageBatch)
	}
	if flushCount != 1 {
		t.Fatalf("expected one flush event, got %d", flushCount)
	}
	if len(client.calls) < 2 {
		t.Fatalf("expected at least 2 model calls, got %d", len(client.calls))
	}
	second := client.calls[1]
	userMessages := make([]llm.Message, 0, len(requestMessages(second)))
	for _, m := range requestMessages(second) {
		if m.Role == llm.RoleUser {
			userMessages = append(userMessages, m)
		}
	}
	if len(userMessages) < 2 {
		t.Fatalf("expected initial and flushed user messages, got %+v", requestMessages(second))
	}
	last := userMessages[len(userMessages)-1]
	if last.Content != "steer now\n\nand keep tests focused" {
		t.Fatalf("expected coalesced flushed user message, got %+v", userMessages)
	}
}

func TestRequestMessagesPreserveANSIEscapes(t *testing.T) {
	seedContent := "raw \x1b[31mansi\x1b[0m"
	store := mustCreateTestSession(t)

	client := &fakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "ok"},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}

	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{})

	if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: seedContent}); err != nil {
		t.Fatalf("append seed message: %v", err)
	}

	if _, err := eng.SubmitUserMessage(context.Background(), "plain user"); err != nil {
		t.Fatalf("submit: %v", err)
	}

	if len(client.calls) == 0 {
		t.Fatal("expected at least one model call")
	}

	for _, req := range client.calls {
		foundSeed := false
		for _, msg := range requestMessages(req) {
			if msg.Role == llm.RoleUser && msg.Content == seedContent {
				foundSeed = true
			}
		}
		if !foundSeed {
			t.Fatalf("expected request messages to preserve exact seeded ANSI message %q, messages=%+v", seedContent, requestMessages(req))
		}
	}
}

func TestReasoningSummaryVisibleAndEncryptedReasoningRoundTrips(t *testing.T) {
	store := mustCreateTestSession(t)

	client := &fakeClient{responses: []llm.Response{
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "first"},
			Reasoning: []llm.ReasoningEntry{
				{Role: "reasoning", Text: "Plan summary"},
			},
			ReasoningItems: []llm.ReasoningItem{
				{ID: "rs_1", EncryptedContent: "enc_1"},
			},
			Usage: llm.Usage{WindowTokens: 200000},
		},
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "second"},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
	}}

	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{})

	if _, err := eng.SubmitUserMessage(context.Background(), "one"); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if _, err := eng.SubmitUserMessage(context.Background(), "two"); err != nil {
		t.Fatalf("second submit: %v", err)
	}

	if len(client.calls) < 2 {
		t.Fatalf("expected two model calls, got %d", len(client.calls))
	}
	secondReq := client.calls[1]
	foundReasoningItem := false
	for _, msg := range requestMessages(secondReq) {
		if msg.Role != llm.RoleAssistant || msg.Content != "first" {
			continue
		}
		if len(msg.ReasoningItems) == 1 &&
			msg.ReasoningItems[0].ID == "rs_1" &&
			msg.ReasoningItems[0].EncryptedContent == "enc_1" {
			foundReasoningItem = true
		}
	}
	if !foundReasoningItem {
		t.Fatalf("expected prior assistant message to carry encrypted reasoning item, got %+v", requestMessages(secondReq))
	}
	for _, msg := range requestMessages(secondReq) {
		if strings.Contains(msg.Content, "Plan summary") {
			t.Fatalf("reasoning summary text should not be sent back to model input, found in %+v", requestMessages(secondReq))
		}
	}

	snap := eng.ChatSnapshot()
	sawSummary := false
	for _, entry := range snap.Entries {
		if entry.Role == "reasoning" && strings.Contains(entry.Text, "Plan summary") {
			sawSummary = true
			break
		}
	}
	if !sawSummary {
		t.Fatalf("expected reasoning summary in chat snapshot entries, got %+v", snap.Entries)
	}

	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	sawLocal := false
	for _, evt := range events {
		if evt.Kind != "local_entry" {
			continue
		}
		var entry storedLocalEntry
		if err := json.Unmarshal(evt.Payload, &entry); err != nil {
			t.Fatalf("decode local_entry: %v", err)
		}
		if entry.Role == "reasoning" && entry.Text == "Plan summary" {
			sawLocal = true
		}
	}
	if !sawLocal {
		t.Fatalf("expected persisted local_entry for reasoning summary, events=%+v", events)
	}
}

func TestDiscardQueuedUserMessageRemovesExactQueuedEntry(t *testing.T) {
	eng := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{})

	first := eng.QueueUserMessage("same")
	eng.QueueUserMessage("other")
	duplicate := eng.QueueUserMessage("same")

	if removed := eng.DiscardQueuedUserMessage(duplicate.ID); !removed {
		t.Fatal("expected duplicate queued item removed")
	}

	messages := eng.messageFlow.(*defaultMessageLifecycle).queuedUserMessagesSnapshot()
	if len(messages) != 2 || messages[0].ID != first.ID || messages[0].Text != "same" || messages[1].Text != "other" {
		t.Fatalf("unexpected pending queue after discard: %+v", messages)
	}
}

func TestContextUsageUsesLastUsageWhenAvailable(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5", ContextWindowTokens: 400_000})
	eng.setLastUsage(llm.Usage{InputTokens: 1234, OutputTokens: 66, WindowTokens: 399_000})

	usage := eng.ContextUsage()
	if usage.UsedTokens != 1234 {
		t.Fatalf("used tokens=%d, want 1234", usage.UsedTokens)
	}
	if usage.WindowTokens != 400_000 {
		t.Fatalf("window tokens=%d, want 400000", usage.WindowTokens)
	}
}

func TestContextUsageFallsBackToEstimatedTokens(t *testing.T) {
	eng := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{ContextWindowTokens: 410_000})
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: "estimate me"}); err != nil {
		t.Fatalf("append message: %v", err)
	}

	usage := eng.ContextUsage()
	if usage.WindowTokens != 410_000 {
		t.Fatalf("window tokens=%d, want 410000", usage.WindowTokens)
	}
	if usage.UsedTokens <= 0 {
		t.Fatalf("expected estimated used tokens > 0, got %d", usage.UsedTokens)
	}
}

func TestContextUsageTracksWeightedCacheHitPercentageFromModelUsage(t *testing.T) {
	eng := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{ContextWindowTokens: 410_000})

	if usage := eng.ContextUsage(); usage.HasCacheHitPercentage {
		t.Fatalf("expected cache hit percentage to be unavailable before model usage, got %+v", usage)
	}

	eng.setLastUsage(llm.Usage{InputTokens: 100, CachedInputTokens: 40, HasCachedInputTokens: true})
	eng.setLastUsage(llm.Usage{InputTokens: 300, CachedInputTokens: 60, HasCachedInputTokens: true})
	eng.setLastUsage(llm.Usage{InputTokens: 999})

	usage := eng.ContextUsage()
	if !usage.HasCacheHitPercentage {
		t.Fatalf("expected cache hit percentage to be available, got %+v", usage)
	}
	if usage.CacheHitPercent != 25 {
		t.Fatalf("cache hit percent=%d, want 25", usage.CacheHitPercent)
	}
}

func TestContextUsageUsesEstimatedTokensWhenLastUsageIsStale(t *testing.T) {
	eng := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{ContextWindowTokens: 410_000})
	eng.setLastUsage(llm.Usage{InputTokens: 100, OutputTokens: 0, WindowTokens: 410_000})
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: strings.Repeat("x", 1600)}); err != nil {
		t.Fatalf("append message: %v", err)
	}

	estimated := estimateItemsTokens(eng.snapshotItems())
	if estimated <= 100 {
		t.Fatalf("expected estimated tokens above stale usage baseline, got %d", estimated)
	}

	usage := eng.ContextUsage()
	want := 100 + estimated
	if usage.UsedTokens != want {
		t.Fatalf("used tokens=%d, want baseline+delta %d", usage.UsedTokens, want)
	}
}

func TestContextUsageAddsOnlyPostCheckpointEstimateDelta(t *testing.T) {
	eng := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{ContextWindowTokens: 410_000})
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: strings.Repeat("seed-", 100)}); err != nil {
		t.Fatalf("append seed message: %v", err)
	}
	checkpointEstimate := estimateItemsTokens(eng.snapshotItems())
	eng.setLastUsage(llm.Usage{InputTokens: 900, OutputTokens: 120, WindowTokens: 410_000})
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: strings.Repeat("delta-", 40)}); err != nil {
		t.Fatalf("append delta message: %v", err)
	}

	currentEstimate := estimateItemsTokens(eng.snapshotItems())
	deltaEstimate := currentEstimate - checkpointEstimate
	if deltaEstimate <= 0 {
		t.Fatalf("expected positive estimated delta, got checkpoint=%d current=%d", checkpointEstimate, currentEstimate)
	}

	usage := eng.ContextUsage()
	want := 900 + deltaEstimate
	if usage.UsedTokens != want {
		t.Fatalf("used tokens=%d, want baseline+delta %d", usage.UsedTokens, want)
	}
}

func TestReopenedSessionRestoresUsageCheckpointDeltaAccounting(t *testing.T) {
	store := mustCreateTestSession(t)
	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{ContextWindowTokens: 410_000})
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: strings.Repeat("seed-", 100)}); err != nil {
		t.Fatalf("append seed message: %v", err)
	}
	checkpointEstimate := estimateItemsTokens(eng.snapshotItems())
	if err := eng.recordLastUsage(llm.Usage{InputTokens: 900, OutputTokens: 120, WindowTokens: 410_000, CachedInputTokens: 45, HasCachedInputTokens: true}); err != nil {
		t.Fatalf("record last usage: %v", err)
	}
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: strings.Repeat("delta-", 40)}); err != nil {
		t.Fatalf("append delta message: %v", err)
	}

	reopenedStore := mustOpenTestSession(t, store.Dir())
	restored := mustNewTestEngine(t, reopenedStore, &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{ContextWindowTokens: 410_000})

	currentEstimate := estimateItemsTokens(restored.snapshotItems())
	deltaEstimate := currentEstimate - checkpointEstimate
	if deltaEstimate <= 0 {
		t.Fatalf("expected positive estimated delta after reopen, got checkpoint=%d current=%d", checkpointEstimate, currentEstimate)
	}
	usage := restored.ContextUsage()
	want := 900 + deltaEstimate
	if usage.UsedTokens != want {
		t.Fatalf("used tokens after reopen=%d, want baseline+delta %d", usage.UsedTokens, want)
	}
	if !usage.HasCacheHitPercentage || usage.CacheHitPercent != 5 {
		t.Fatalf("cache hit metadata after reopen=%+v, want 5%%", usage)
	}
}

func TestHistoryReplacementResetsDiagnosticDedupe(t *testing.T) {
	store := mustCreateTestSession(t)
	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{ContextWindowTokens: 410_000})
	if err := eng.appendPersistedDiagnosticEntry("step-1", preciseTokenCountFailureDiagnostic, "error", "first fallback"); err != nil {
		t.Fatalf("append first diagnostic: %v", err)
	}
	if err := eng.replaceHistory("step-compact", "local", compactionModeManual, llm.ItemsFromMessages([]llm.Message{{Role: llm.RoleAssistant, Content: "summary"}})); err != nil {
		t.Fatalf("replace history: %v", err)
	}
	if err := eng.appendPersistedDiagnosticEntry("step-2", preciseTokenCountFailureDiagnostic, "error", "second fallback"); err != nil {
		t.Fatalf("append second diagnostic: %v", err)
	}

	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	count := 0
	for _, evt := range events {
		if evt.Kind != "local_entry" {
			continue
		}
		var entry storedLocalEntry
		if err := json.Unmarshal(evt.Payload, &entry); err != nil {
			t.Fatalf("decode local entry: %v", err)
		}
		if entry.DiagnosticKey == preciseTokenCountFailureDiagnostic {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("diagnostic entry count=%d, want 2", count)
	}
}

func TestReopenedSessionHistoryReplacementResetsDiagnosticDedupe(t *testing.T) {
	store := mustCreateTestSession(t)
	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{ContextWindowTokens: 410_000})
	if err := eng.appendPersistedDiagnosticEntry("step-1", preciseTokenCountFailureDiagnostic, "error", "first fallback"); err != nil {
		t.Fatalf("append first diagnostic: %v", err)
	}
	if err := eng.replaceHistory("step-compact", "local", compactionModeManual, llm.ItemsFromMessages([]llm.Message{{Role: llm.RoleAssistant, Content: "summary"}})); err != nil {
		t.Fatalf("replace history: %v", err)
	}

	reopenedStore := mustOpenTestSession(t, store.Dir())
	restored := mustNewTestEngine(t, reopenedStore, &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{ContextWindowTokens: 410_000})
	if err := restored.appendPersistedDiagnosticEntry("step-2", preciseTokenCountFailureDiagnostic, "error", "second fallback"); err != nil {
		t.Fatalf("append second diagnostic after reopen: %v", err)
	}

	events, err := reopenedStore.ReadEvents()
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	count := 0
	for _, evt := range events {
		if evt.Kind != "local_entry" {
			continue
		}
		var entry storedLocalEntry
		if err := json.Unmarshal(evt.Payload, &entry); err != nil {
			t.Fatalf("decode local entry: %v", err)
		}
		if entry.DiagnosticKey == preciseTokenCountFailureDiagnostic {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("diagnostic entry count after reopen=%d, want 2", count)
	}
}

func TestEstimateItemsTokensDoesNotTreatInlineImagePayloadAsPlainText(t *testing.T) {
	base64Payload := strings.Repeat("A", 24_000)
	item := llm.ResponseItem{
		Type:   llm.ResponseItemTypeFunctionCallOutput,
		Name:   string(toolspec.ToolViewImage),
		CallID: "call-1",
		Output: json.RawMessage(`[{"type":"input_image","image_url":"data:image/png;base64,` + base64Payload + `"}]`),
	}

	estimated := estimateItemsTokens([]llm.ResponseItem{item})
	naive := (len(item.Name) + len(item.CallID) + len(item.Output) + 3) / 4
	if estimated <= 0 {
		t.Fatalf("expected multimodal estimate > 0, got %d", estimated)
	}
	if estimated >= naive/4 {
		t.Fatalf("expected multimodal estimate to stay well below plain-text estimate, got estimated=%d naive=%d", estimated, naive)
	}
}

func TestContextUsageDoesNotInflateInlineImagePayloadByBase64Length(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5", ContextWindowTokens: 410_000})
	eng.setLastUsage(llm.Usage{InputTokens: 100, OutputTokens: 0, WindowTokens: 410_000})
	if err := eng.appendMessage("", llm.Message{
		Role:       llm.RoleTool,
		ToolCallID: "call-1",
		Name:       string(toolspec.ToolViewImage),
		Content:    `[{"type":"input_image","image_url":"data:image/png;base64,` + strings.Repeat("A", 24_000) + `"}]`,
	}); err != nil {
		t.Fatalf("append tool message: %v", err)
	}

	usage := eng.ContextUsage()
	if usage.UsedTokens <= 100 {
		t.Fatalf("expected local estimate to exceed stale usage baseline, got %d", usage.UsedTokens)
	}
	if usage.UsedTokens >= 2_000 {
		t.Fatalf("expected inline image estimate to avoid base64 inflation, got %d", usage.UsedTokens)
	}
}

func TestShouldAutoCompactAccountsForMessagesAppendedAfterLastUsage(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	eng, err := New(store, &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		Model:                 "gpt-5",
		ContextWindowTokens:   410_000,
		AutoCompactTokenLimit: 300,
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	eng.setLastUsage(llm.Usage{InputTokens: 120, OutputTokens: 0, WindowTokens: 410_000})
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: strings.Repeat("stale-usage-gap-", 120)}); err != nil {
		t.Fatalf("append message: %v", err)
	}

	if !eng.shouldAutoCompact() {
		t.Fatalf("expected auto compaction to trigger from appended message growth")
	}
}

func TestShouldAutoCompactUsesPreciseRequestInputTokenCountWhenAvailable(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	client := &preciseCompactionClient{inputTokenCount: 960, contextWindow: 1000}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		Model:                 "gpt-5",
		ContextWindowTokens:   400_000,
		AutoCompactTokenLimit: 900,
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: "short"}); err != nil {
		t.Fatalf("append message: %v", err)
	}

	if !eng.shouldAutoCompact() {
		t.Fatalf("expected auto compaction to trigger from precise input token count")
	}
}

func TestPreSubmitCompactionTokenLimitUsesFixedRunwayReserve(t *testing.T) {
	tests := []struct {
		name     string
		limit    int
		runway   int
		expected int
	}{
		{
			name:     "subtracts fixed runway from auto threshold",
			limit:    190_000,
			runway:   35_000,
			expected: 155_000,
		},
		{
			name:     "large windows still use same fixed runway",
			limit:    950_000,
			runway:   35_000,
			expected: 915_000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := session.Create(dir, "ws", dir)
			if err != nil {
				t.Fatalf("create store: %v", err)
			}

			eng, err := New(store, &fakeClient{}, tools.NewRegistry(), Config{
				Model:                         "gpt-5",
				AutoCompactTokenLimit:         tt.limit,
				ContextWindowTokens:           1_000_000,
				PreSubmitCompactionLeadTokens: tt.runway,
			})
			if err != nil {
				t.Fatalf("new engine: %v", err)
			}

			if got := eng.preSubmitCompactionTokenLimit(context.Background()); got != tt.expected {
				t.Fatalf("unexpected pre-submit compaction threshold: got %d want %d", got, tt.expected)
			}
		})
	}
}

func TestShouldCompactBeforeUserMessageUsesPromptGrowthBelowPreSubmitBand(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	client := &preciseCompactionClient{inputTokenCount: 960, contextWindow: 1000}
	eng, err := New(store, client, tools.NewRegistry(), Config{
		Model:                         "gpt-5",
		AutoCompactTokenLimit:         950,
		ContextWindowTokens:           1000,
		PreSubmitCompactionLeadTokens: 50,
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: strings.Repeat("a", 3400)}); err != nil {
		t.Fatalf("append message: %v", err)
	}

	shouldCompact, err := eng.ShouldCompactBeforeUserMessage(context.Background(), strings.Repeat("b", 400))
	if err != nil {
		t.Fatalf("ShouldCompactBeforeUserMessage: %v", err)
	}
	if !shouldCompact {
		t.Fatal("expected pre-submit compaction when prompt growth would cross the real threshold")
	}
	if client.countCalls == 0 {
		t.Fatal("expected precise request token count to be used for prompt-growth check")
	}
}

func TestShouldCompactBeforeUserMessageFallsBackWhenExactCountUnsupported(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	supported := false
	client := &preciseCompactionClient{inputTokenCount: 960, contextWindow: 1000, countSupported: &supported}
	eng, err := New(store, client, tools.NewRegistry(), Config{
		Model:                         "gpt-5",
		AutoCompactTokenLimit:         950,
		ContextWindowTokens:           1000,
		PreSubmitCompactionLeadTokens: 50,
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: strings.Repeat("a", 3400)}); err != nil {
		t.Fatalf("append message: %v", err)
	}

	shouldCompact, err := eng.ShouldCompactBeforeUserMessage(context.Background(), strings.Repeat("b", 400))
	if err != nil {
		t.Fatalf("ShouldCompactBeforeUserMessage: %v", err)
	}
	if !shouldCompact {
		t.Fatal("expected fallback estimator to trigger pre-submit compaction when exact counting is unsupported")
	}
	if client.countCalls != 0 {
		t.Fatalf("count calls=%d, want 0 when exact counting is unsupported", client.countCalls)
	}
}
