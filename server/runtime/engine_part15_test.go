package runtime

import (
	"builder/prompts"
	"builder/server/llm"
	"builder/server/session"
	"builder/server/tools"
	"builder/shared/toolspec"
	"builder/shared/transcript"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteCompactionUsesSublinearPreciseTokenCountCalls(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	maxItemsSeen := 0
	client := &fakeCompactionClient{
		inputTokenCountFn: func(req llm.Request) int {
			if len(req.Items) > maxItemsSeen {
				maxItemsSeen = len(req.Items)
			}
			return len(req.Items) * 1000
		},
		compactionResponses: []llm.CompactionResponse{
			{
				OutputItems: []llm.ResponseItem{
					{Type: llm.ResponseItemTypeMessage, Role: llm.RoleUser, Content: "seed"},
					{Type: llm.ResponseItemTypeCompaction, ID: "cmp_1", EncryptedContent: "enc_1"},
				},
				Usage: llm.Usage{InputTokens: 1000, OutputTokens: 100, WindowTokens: 400000},
			},
		},
	}

	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5", ContextWindowTokens: 400_000})
	for i := 0; i < 600; i++ {
		if err := eng.appendMessage("", llm.Message{Role: llm.RoleAssistant, Content: "a"}); err != nil {
			t.Fatalf("append assistant message %d: %v", i, err)
		}
	}

	if err := eng.CompactContext(context.Background(), ""); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if maxItemsSeen <= 0 {
		t.Fatalf("expected at least one precise token-count request")
	}
	bound := 2*ceilLog2Int(maxItemsSeen+1) + 14
	if client.countInputTokenCalls > bound {
		t.Fatalf("expected sublinear precise token count calls, got=%d bound=%d n=%d", client.countInputTokenCalls, bound, maxItemsSeen)
	}
}

func TestLocalCompactionCarryoverUsesSublinearPreciseTokenCountCalls(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	maxItemsSeen := 0
	client := &fakeCompactionClient{
		inputTokenCountFn: func(req llm.Request) int {
			if len(req.Items) > maxItemsSeen {
				maxItemsSeen = len(req.Items)
			}
			return len(req.Items) * 1000
		},
		responses: []llm.Response{
			{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "summary"}, Usage: llm.Usage{WindowTokens: 400000}},
		},
	}

	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		Model:               "gpt-5",
		ContextWindowTokens: 400_000,
		CompactionMode:      "local",
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	for i := 0; i < 512; i++ {
		if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: "u"}); err != nil {
			t.Fatalf("append user message %d: %v", i, err)
		}
	}

	if err := eng.CompactContext(context.Background(), ""); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if maxItemsSeen <= 0 {
		t.Fatalf("expected at least one precise token-count request")
	}
	bound := 2*ceilLog2Int(maxItemsSeen+1) + 16
	if client.countInputTokenCalls > bound {
		t.Fatalf("expected sublinear precise token count calls for local carryover, got=%d bound=%d n=%d", client.countInputTokenCalls, bound, maxItemsSeen)
	}
}

func ceilLog2Int(value int) int {
	if value <= 1 {
		return 0
	}
	pow := 0
	current := 1
	for current < value {
		current <<= 1
		pow++
	}
	return pow
}

func TestManualCompactionLocalUsesHistorySinceLastCompactionCheckpoint(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	client := &fakeClient{
		responses: []llm.Response{
			{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "summary"}},
		},
	}
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5", CompactionMode: "local"})
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleDeveloper, Content: "canonical context"}); err != nil {
		t.Fatalf("append canonical context: %v", err)
	}
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: "old user request"}); err != nil {
		t.Fatalf("append old user message: %v", err)
	}
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleAssistant, Content: "old assistant response"}); err != nil {
		t.Fatalf("append old assistant message: %v", err)
	}
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeCompactionSummary, Content: "old compacted summary"}); err != nil {
		t.Fatalf("append compaction checkpoint: %v", err)
	}
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: "new user request"}); err != nil {
		t.Fatalf("append new user message: %v", err)
	}
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleAssistant, Content: "new assistant response"}); err != nil {
		t.Fatalf("append new assistant message: %v", err)
	}

	if err := eng.CompactContext(context.Background(), ""); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected one local-summary model call, got %d", len(client.calls))
	}
	if len(client.calls[0].Tools) == 0 {
		t.Fatalf("expected tools to remain declared for local compaction cache stability")
	}

	foundCanonical := false
	foundCheckpoint := false
	foundNewUser := false
	foundOldUser := false
	foundPrompt := false
	for _, item := range client.calls[0].Items {
		if item.Type != llm.ResponseItemTypeMessage {
			continue
		}
		if item.Role == llm.RoleDeveloper && item.Content == "canonical context" {
			foundCanonical = true
		}
		if item.Role == llm.RoleDeveloper && item.MessageType == llm.MessageTypeCompactionSummary {
			foundCheckpoint = true
		}
		if item.Role == llm.RoleUser && item.Content == "new user request" {
			foundNewUser = true
		}
		if item.Role == llm.RoleUser && item.Content == "old user request" {
			foundOldUser = true
		}
		if item.Role == llm.RoleDeveloper && item.Content == prompts.CompactionPrompt {
			foundPrompt = true
		}
	}

	if foundCanonical {
		t.Fatalf("did not expect pre-compaction developer context in local compaction request, got %+v", client.calls[0].Items)
	}
	if !foundCheckpoint {
		t.Fatalf("expected last compaction checkpoint item in local compaction request, got %+v", client.calls[0].Items)
	}
	if !foundNewUser {
		t.Fatalf("expected post-checkpoint history in local compaction request, got %+v", client.calls[0].Items)
	}
	if foundOldUser {
		t.Fatalf("did not expect pre-checkpoint history in local compaction request, got %+v", client.calls[0].Items)
	}
	if !foundPrompt {
		t.Fatalf("expected compaction prompt as developer message, got %+v", client.calls[0].Items)
	}
}

func TestManualCompactionLocalFailsWhenModelAttemptsToolCalls(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	client := &fakeClient{
		responses: []llm.Response{
			{
				Assistant: llm.Message{Role: llm.RoleAssistant, Content: ""},
				ToolCalls: []llm.ToolCall{{ID: "call_1", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"command":"pwd"}`)}},
			},
		},
	}
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5", CompactionMode: "local"})
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: "seed"}); err != nil {
		t.Fatalf("append message: %v", err)
	}

	err = eng.CompactContext(context.Background(), "")
	if err == nil {
		t.Fatal("expected local compaction to fail when model attempts tool calls")
	}
	if !strings.Contains(err.Error(), "tool calls") {
		t.Fatalf("expected tool-call error, got %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected manual local compaction to fail without retry, got %d requests", len(client.calls))
	}
	for _, item := range client.calls[0].Items {
		if item.Type == llm.ResponseItemTypeFunctionCallOutput && item.CallID == "call_1" {
			t.Fatalf("did not expect manual compaction request to inject synthetic failed tool output, got %+v", client.calls[0].Items)
		}
	}
}

func TestManualCompactionDisabledWhenModeNone(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	client := &fakeCompactionClient{}
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5", CompactionMode: "none"})
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: "seed"}); err != nil {
		t.Fatalf("append message: %v", err)
	}

	err = eng.CompactContext(context.Background(), "")
	if err == nil {
		t.Fatal("expected manual compaction to fail when compaction_mode=none")
	}
	if !strings.Contains(err.Error(), "compaction_mode=none") {
		t.Fatalf("expected disabled-compaction error, got %v", err)
	}
	if len(client.compactionCalls) != 0 {
		t.Fatalf("expected no remote compaction call when disabled, got %d", len(client.compactionCalls))
	}
	if len(client.calls) != 0 {
		t.Fatalf("expected no local-summary model call when disabled, got %d", len(client.calls))
	}
}

func TestAutoCompactionRecomputesUsageFromReplacementHistory(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	client := &fakeCompactionClient{
		compactionResponses: []llm.CompactionResponse{
			{
				OutputItems: []llm.ResponseItem{
					{Type: llm.ResponseItemTypeMessage, Role: llm.RoleUser, MessageType: llm.MessageTypeCompactionSummary, Content: "summary"},
					{Type: llm.ResponseItemTypeCompaction, ID: "cmp_1", EncryptedContent: "enc_1"},
				},
				Usage: llm.Usage{InputTokens: 190000, OutputTokens: 1000, WindowTokens: 200000},
			},
		},
	}

	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5"})
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: "seed"}); err != nil {
		t.Fatalf("append seed message: %v", err)
	}
	eng.setLastUsage(llm.Usage{InputTokens: 190000, OutputTokens: 0, WindowTokens: 200000})

	if err := eng.autoCompactIfNeeded(context.Background(), "step-1", compactionModeAuto); err != nil {
		t.Fatalf("auto compact failed: %v", err)
	}
	if eng.shouldAutoCompact() {
		t.Fatalf("expected auto compact threshold to be cleared after replacement, usage=%+v", eng.lastUsageSnapshot())
	}
}

func TestCompactionLabelsSingleSummaryEntry(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	client := &fakeCompactionClient{
		compactionResponses: []llm.CompactionResponse{
			{
				OutputItems: []llm.ResponseItem{
					{Type: llm.ResponseItemTypeMessage, Role: llm.RoleUser, MessageType: llm.MessageTypeCompactionSummary, Content: "summary"},
					{Type: llm.ResponseItemTypeCompaction, ID: "cmp_1", EncryptedContent: "enc_1"},
				},
				Usage: llm.Usage{InputTokens: 190000, OutputTokens: 1000, WindowTokens: 200000},
			},
		},
	}

	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5"})
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: "seed"}); err != nil {
		t.Fatalf("append seed message: %v", err)
	}
	eng.setLastUsage(llm.Usage{InputTokens: 190000, OutputTokens: 0, WindowTokens: 200000})

	if err := eng.autoCompactIfNeeded(context.Background(), "step-1", compactionModeAuto); err != nil {
		t.Fatalf("auto compact failed: %v", err)
	}

	snap := eng.ChatSnapshot()
	summaries := 0
	for _, entry := range snap.Entries {
		if entry.Role == string(transcript.EntryRoleCompactionSummary) {
			summaries++
			if entry.CompactLabel != "context compacted for the 1st time" || entry.OngoingText != "context compacted for the 1st time" {
				t.Fatalf("unexpected compaction summary label: %+v", entry)
			}
		}
		if strings.Contains(strings.ToLower(entry.Text), "compaction started") || strings.Contains(strings.ToLower(entry.Text), "compaction completed") {
			t.Fatalf("unexpected start/completed status entry: %+v", entry)
		}
	}
	if summaries != 1 {
		t.Fatalf("expected one compaction summary, got %d entries=%+v", summaries, snap.Entries)
	}
}

func TestEmitCompactionStatusStillPublishesFailureEventWhenErrorPersistenceFails(t *testing.T) {
	localEntryErr := errors.New("injected compaction error persistence failure")
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	var events []Event
	eng, err := New(store, &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		Model:   "gpt-5",
		OnEvent: func(evt Event) { events = append(events, evt) },
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	eng.beforePersistLocalEntry = func(entry storedLocalEntry) error {
		if entry.Role == "error" {
			return localEntryErr
		}
		return nil
	}

	err = eng.emitCompactionStatus("step-1", EventCompactionFailed, compactionModeAuto, "remote", "openai", 0, 2, "quota exceeded")
	if !errors.Is(err, localEntryErr) {
		t.Fatalf("emitCompactionStatus error = %v, want %v", err, localEntryErr)
	}
	terminalEvents := 0
	for _, evt := range events {
		if evt.Kind == EventLocalEntryAdded {
			t.Fatalf("did not expect persisted local entry event after error persistence failure, got %+v", events)
		}
		if evt.Kind == EventCompactionFailed {
			terminalEvents++
		}
	}
	if terminalEvents != 1 {
		t.Fatalf("expected one compaction failed event despite error persistence failure, got %+v", events)
	}
}

func TestReplaceHistoryDoesNotMutateRuntimeStateWhenEventAppendFails(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5"})
	if err := eng.appendMessage("step-1", llm.Message{Role: llm.RoleUser, Content: "pre-compaction"}); err != nil {
		t.Fatalf("append seed message: %v", err)
	}
	eng.setCompactionSoonReminderIssued(true)

	eventsPath := filepath.Join(store.Dir(), "events.jsonl")
	if err := os.Chmod(eventsPath, 0o444); err != nil {
		t.Fatalf("chmod events read-only: %v", err)
	}
	defer func() {
		_ = os.Chmod(eventsPath, 0o644)
	}()

	err = eng.replaceHistory("step-compact", "local", compactionModeManual, llm.ItemsFromMessages([]llm.Message{{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeCompactionSummary, Content: "summary"}}))
	if err == nil {
		t.Fatal("expected replaceHistory persistence failure")
	}
	if messages := eng.snapshotMessages(); len(messages) != 1 || messages[0].Content != "pre-compaction" {
		t.Fatalf("runtime transcript mutated despite persistence failure: %+v", messages)
	}
	if !eng.compactionRuntimeState().SoonReminderIssued() {
		t.Fatal("reminder state mutated despite persistence failure")
	}
}

type failOnCompactionReminderResetObservation struct {
	failed bool
}

func (o *failOnCompactionReminderResetObservation) ObservePersistedStore(_ context.Context, snapshot session.PersistedStoreSnapshot) error {
	if !o.failed && snapshot.Meta.LastSequence >= 2 && !snapshot.Meta.CompactionSoonReminderIssued {
		o.failed = true
		return errors.New("persist observer failed")
	}
	return nil
}

type failOnUsageStateResetObservation struct {
	failed bool
}

func (o *failOnUsageStateResetObservation) ObservePersistedStore(_ context.Context, snapshot session.PersistedStoreSnapshot) error {
	if !o.failed && snapshot.Meta.LastSequence >= 2 && snapshot.Meta.UsageState == nil {
		o.failed = true
		return errors.New("persist observer failed")
	}
	return nil
}

func TestReplaceHistoryUpdatesRuntimeStateWhenMetadataPersistFailsAfterEventAppend(t *testing.T) {
	dir := t.TempDir()
	observer := &failOnCompactionReminderResetObservation{}
	store, err := session.Create(dir, "ws", dir, session.WithPersistenceObserver(observer))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5"})
	if err := eng.appendMessage("step-1", llm.Message{Role: llm.RoleUser, Content: "pre-compaction"}); err != nil {
		t.Fatalf("append seed message: %v", err)
	}
	if err := store.SetCompactionSoonReminderIssued(true); err != nil {
		t.Fatalf("persist seed reminder state: %v", err)
	}
	eng.setCompactionSoonReminderIssued(true)

	err = eng.replaceHistory("step-compact", "local", compactionModeManual, llm.ItemsFromMessages([]llm.Message{{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeCompactionSummary, Content: "summary"}}))
	if err == nil {
		t.Fatal("expected replaceHistory metadata persistence failure")
	}
	messages := eng.snapshotMessages()
	if len(messages) != 1 || messages[0].Content != "summary" {
		t.Fatalf("runtime transcript not updated after durable history replacement: %+v", messages)
	}
	if eng.compactionRuntimeState().SoonReminderIssued() {
		t.Fatal("reminder state not reset after durable history replacement")
	}
}

func TestReplaceHistoryUpdatesRuntimeStateWhenUsageMetadataPersistFailsAfterEventAppend(t *testing.T) {
	dir := t.TempDir()
	observer := &failOnUsageStateResetObservation{}
	store, err := session.Create(dir, "ws", dir, session.WithPersistenceObserver(observer))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5"})
	if err := eng.appendMessage("step-1", llm.Message{Role: llm.RoleUser, Content: "pre-compaction"}); err != nil {
		t.Fatalf("append seed message: %v", err)
	}
	if err := store.SetUsageState(&session.UsageState{InputTokens: 42, WindowTokens: 100}); err != nil {
		t.Fatalf("persist seed usage state: %v", err)
	}
	eng.setLastUsage(llm.Usage{InputTokens: 42, WindowTokens: 100})

	err = eng.replaceHistory("step-compact", "local", compactionModeManual, llm.ItemsFromMessages([]llm.Message{{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeCompactionSummary, Content: "summary"}}))
	if err == nil {
		t.Fatal("expected replaceHistory usage metadata persistence failure")
	}
	messages := eng.snapshotMessages()
	if len(messages) != 1 || messages[0].Content != "summary" {
		t.Fatalf("runtime transcript not updated after durable history replacement: %+v", messages)
	}
}

func TestAutoCompactionRemoteReplacesHistoryAndCarriesCompactionItem(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	client := &fakeCompactionClient{
		responses: []llm.Response{
			{
				Assistant: llm.Message{Role: llm.RoleAssistant, Content: "working"},
				ToolCalls: []llm.ToolCall{
					{ID: "call_1", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"command":"pwd"}`)},
				},
				Usage: llm.Usage{InputTokens: 190000, OutputTokens: 2000, WindowTokens: 200000},
			},
			{
				Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done"},
				Usage:     llm.Usage{InputTokens: 2000, OutputTokens: 1000, WindowTokens: 200000},
			},
		},
		compactionResponses: []llm.CompactionResponse{
			{
				OutputItems: []llm.ResponseItem{
					{Type: llm.ResponseItemTypeMessage, Role: llm.RoleUser, Content: "run tools"},
					{Type: llm.ResponseItemTypeCompaction, ID: "cmp_1", EncryptedContent: "enc_1"},
				},
				Usage: llm.Usage{InputTokens: 12000, OutputTokens: 1000, WindowTokens: 200000},
			},
		},
	}

	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5"})

	msg, err := eng.SubmitUserMessage(context.Background(), "run tools")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if msg.Content != "done" {
		t.Fatalf("assistant content = %q, want done", msg.Content)
	}
	if len(client.compactionCalls) != 1 {
		t.Fatalf("expected one remote compaction call, got %d", len(client.compactionCalls))
	}
	if len(client.calls) < 2 {
		t.Fatalf("expected second model call after compaction, got %d calls", len(client.calls))
	}

	foundCompactionItem := false
	for _, item := range client.calls[1].Items {
		if item.Type == llm.ResponseItemTypeCompaction && item.EncryptedContent == "enc_1" {
			foundCompactionItem = true
			break
		}
	}
	if !foundCompactionItem {
		t.Fatalf("expected compaction item in post-compaction request, got %+v", client.calls[1].Items)
	}

	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	sawHistoryReplace := false
	for _, evt := range events {
		if evt.Kind == "history_replaced" {
			sawHistoryReplace = true
			break
		}
	}
	if !sawHistoryReplace {
		t.Fatalf("expected history_replaced event, got %+v", events)
	}
}

func TestAutoCompactionRemoteDropsPreCompactionDeveloperContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	globalDir := filepath.Join(home, ".builder")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("create global dir: %v", err)
	}
	globalPath := filepath.Join(globalDir, "AGENTS.md")
	if err := os.WriteFile(globalPath, []byte("global instructions"), 0o644); err != nil {
		t.Fatalf("write global AGENTS.md: %v", err)
	}

	workspace := t.TempDir()
	workspacePath := filepath.Join(workspace, "AGENTS.md")
	if err := os.WriteFile(workspacePath, []byte("workspace instructions"), 0o644); err != nil {
		t.Fatalf("write workspace AGENTS.md: %v", err)
	}

	storeRoot := t.TempDir()
	store, err := session.Create(storeRoot, "ws", workspace)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	client := &fakeCompactionClient{
		responses: []llm.Response{
			{
				Assistant: llm.Message{Role: llm.RoleAssistant, Content: "working"},
				ToolCalls: []llm.ToolCall{
					{ID: "call_1", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"command":"pwd"}`)},
				},
				Usage: llm.Usage{InputTokens: 190000, OutputTokens: 2000, WindowTokens: 200000},
			},
			{
				Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done"},
				Usage:     llm.Usage{InputTokens: 2000, OutputTokens: 1000, WindowTokens: 200000},
			},
		},
		compactionResponses: []llm.CompactionResponse{
			{
				OutputItems: []llm.ResponseItem{
					{Type: llm.ResponseItemTypeMessage, Role: llm.RoleUser, Content: "run tools"},
					{Type: llm.ResponseItemTypeCompaction, ID: "cmp_1", EncryptedContent: "enc_1"},
				},
				Usage: llm.Usage{InputTokens: 12000, OutputTokens: 1000, WindowTokens: 200000},
			},
		},
	}

	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5"})

	msg, err := eng.SubmitUserMessage(context.Background(), "run tools")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if msg.Content != "done" {
		t.Fatalf("assistant content = %q, want done", msg.Content)
	}
	if len(client.calls) < 2 {
		t.Fatalf("expected second model call after compaction, got %d calls", len(client.calls))
	}

	post := client.calls[1]
	globalCount := 0
	workspaceCount := 0
	envCount := 0
	for _, item := range post.Items {
		if item.Type != llm.ResponseItemTypeMessage || item.Role != llm.RoleDeveloper {
			continue
		}
		if strings.Contains(item.Content, "source: "+globalPath) {
			globalCount++
		}
		if strings.Contains(item.Content, "source: "+workspacePath) {
			workspaceCount++
		}
		if strings.Contains(item.Content, environmentInjectedHeader) {
			envCount++
		}
	}
	if globalCount != 1 {
		t.Fatalf("expected remote compaction to reinject exactly one current global AGENTS context, got %d", globalCount)
	}
	if workspaceCount != 1 {
		t.Fatalf("expected remote compaction to reinject exactly one current workspace AGENTS context, got %d", workspaceCount)
	}
	if envCount != 1 {
		t.Fatalf("expected remote compaction to reinject exactly one current environment context, got %d", envCount)
	}
}

func TestManualRemoteCompactionRebuildsCanonicalPrefixOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	globalDir := filepath.Join(home, agentsGlobalDirName)
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("mkdir global agents dir: %v", err)
	}
	globalPath := filepath.Join(globalDir, agentsFileName)
	if err := os.WriteFile(globalPath, []byte("global instructions"), 0o644); err != nil {
		t.Fatalf("write global AGENTS.md: %v", err)
	}

	workspace := t.TempDir()
	workspacePath := filepath.Join(workspace, agentsFileName)
	if err := os.WriteFile(workspacePath, []byte("workspace instructions"), 0o644); err != nil {
		t.Fatalf("write workspace AGENTS.md: %v", err)
	}
	writeTestSkill(t, filepath.Join(workspace, ".builder", "skills", "workspace-skill"), "workspace-skill", "from workspace")

	store, err := session.Create(t.TempDir(), "ws", workspace)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	client := &fakeCompactionClient{compactionResponses: []llm.CompactionResponse{{
		OutputItems: []llm.ResponseItem{
			{Type: llm.ResponseItemTypeMessage, Role: llm.RoleUser, MessageType: llm.MessageTypeCompactionSummary, Content: "remote summary"},
			{Type: llm.ResponseItemTypeCompaction, ID: "cmp_1", EncryptedContent: "enc_1"},
		},
		Usage: llm.Usage{InputTokens: 1000, OutputTokens: 100, WindowTokens: 200000},
	}}}
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5"})
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeHeadlessMode, Content: "headless mode instructions"}); err != nil {
		t.Fatalf("append headless mode: %v", err)
	}
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: "seed"}); err != nil {
		t.Fatalf("append seed message: %v", err)
	}

	if _, err := eng.compactNow(context.Background(), "step-1", compactionModeManual, "", false); err != nil {
		t.Fatalf("compactNow: %v", err)
	}

	items := eng.snapshotItems()
	if len(items) < 7 {
		t.Fatalf("expected canonical remote compaction prefix, got %+v", items)
	}
	if items[0].MessageType != llm.MessageTypeCompactionSummary || items[0].Content != "remote summary" {
		t.Fatalf("expected provider summary first, got %+v", items[0])
	}
	if items[1].Type != llm.ResponseItemTypeCompaction || items[1].EncryptedContent != "enc_1" {
		t.Fatalf("expected provider checkpoint second, got %+v", items[1])
	}
	if items[2].MessageType != llm.MessageTypeEnvironment {
		t.Fatalf("expected environment after provider output, got %+v", items[2])
	}
	if items[3].MessageType != llm.MessageTypeSkills {
		t.Fatalf("expected skills after environment, got %+v", items[3])
	}
	if items[4].MessageType != llm.MessageTypeAgentsMD || !strings.Contains(items[4].Content, "source: "+globalPath) {
		t.Fatalf("expected global AGENTS after skills, got %+v", items[4])
	}
	if items[5].MessageType != llm.MessageTypeAgentsMD || !strings.Contains(items[5].Content, "source: "+workspacePath) {
		t.Fatalf("expected workspace AGENTS after global AGENTS, got %+v", items[5])
	}
	if items[6].MessageType != llm.MessageTypeHeadlessMode {
		t.Fatalf("expected headless reinjection after canonical base context, got %+v", items[6])
	}
}

func TestSanitizeRemoteCompactionOutputAcceptsEncryptedReasoningCheckpoint(t *testing.T) {
	output := []llm.ResponseItem{
		{Type: llm.ResponseItemTypeMessage, Role: llm.RoleUser, Content: "u1"},
		{Type: llm.ResponseItemTypeReasoning, ID: "rs_1", EncryptedContent: "enc_reason"},
	}

	replacement, err := sanitizeRemoteCompactionOutput(output)
	if err != nil {
		t.Fatalf("sanitize remote compaction output: %v", err)
	}

	foundReasoning := false
	for _, item := range replacement {
		if item.Type == llm.ResponseItemTypeReasoning && item.EncryptedContent == "enc_reason" {
			foundReasoning = true
			break
		}
	}
	if !foundReasoning {
		t.Fatalf("expected encrypted reasoning checkpoint in replacement history, got %+v", replacement)
	}
}

func TestRemoteCompactionMissingCheckpointFallsBackToLocal(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	client := &fakeCompactionClient{
		responses: []llm.Response{
			{
				Assistant: llm.Message{Role: llm.RoleAssistant, Content: "working"},
				ToolCalls: []llm.ToolCall{
					{ID: "call_1", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"command":"pwd"}`)},
				},
				Usage: llm.Usage{InputTokens: 190000, OutputTokens: 2000, WindowTokens: 200000},
			},
			{
				Assistant: llm.Message{Role: llm.RoleAssistant, Content: "local summary"},
				Usage:     llm.Usage{InputTokens: 8000, OutputTokens: 1000, WindowTokens: 200000},
			},
			{
				Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done"},
				Usage:     llm.Usage{InputTokens: 2000, OutputTokens: 500, WindowTokens: 200000},
			},
		},
		compactionResponses: []llm.CompactionResponse{
			{
				OutputItems: []llm.ResponseItem{
					{Type: llm.ResponseItemTypeMessage, Role: llm.RoleUser, Content: "run tools"},
				},
				Usage: llm.Usage{InputTokens: 12000, OutputTokens: 1000, WindowTokens: 200000},
			},
		},
	}

	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5"})

	msg, err := eng.SubmitUserMessage(context.Background(), "run tools")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if msg.Content != "done" {
		t.Fatalf("assistant content = %q, want done", msg.Content)
	}
	if len(client.compactionCalls) != 1 {
		t.Fatalf("expected one remote compaction call, got %d", len(client.compactionCalls))
	}
	if len(client.calls) < 3 {
		t.Fatalf("expected first turn + local summary + post-compaction turn, got %d calls", len(client.calls))
	}

	foundLocalSummaryCarryover := false
	for _, req := range client.calls {
		for _, item := range req.Items {
			if item.Type == llm.ResponseItemTypeMessage && item.Role == llm.RoleDeveloper && item.MessageType == llm.MessageTypeCompactionSummary {
				foundLocalSummaryCarryover = true
				break
			}
		}
		if foundLocalSummaryCarryover {
			break
		}
	}
	if !foundLocalSummaryCarryover {
		t.Fatalf("expected local summary carryover item in model requests, got %+v", client.calls)
	}
}

func TestAutoCompactionRetries400ByCollapsingShellOutput(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	client := &fakeCompactionClient{
		responses: []llm.Response{
			{
				Assistant: llm.Message{Role: llm.RoleAssistant, Content: "working"},
				ToolCalls: []llm.ToolCall{
					{ID: "call_1", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"command":"pwd"}`)},
				},
				Usage: llm.Usage{InputTokens: 390000, OutputTokens: 1000, WindowTokens: 400000},
			},
			{
				Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done"},
				Usage:     llm.Usage{InputTokens: 2000, OutputTokens: 500, WindowTokens: 400000},
			},
		},
		compactionErrors: []error{
			&llm.ProviderAPIError{ProviderID: "openai", StatusCode: 400, Code: llm.UnifiedErrorCodeContextLengthOverflow, ProviderCode: "context_length_exceeded", Message: "prompt exceeded"},
			nil,
		},
		compactionResponses: []llm.CompactionResponse{
			{
				OutputItems: []llm.ResponseItem{
					{Type: llm.ResponseItemTypeMessage, Role: llm.RoleUser, Content: "run tools"},
					{Type: llm.ResponseItemTypeCompaction, ID: "cmp_1", EncryptedContent: "enc_1"},
				},
				Usage: llm.Usage{InputTokens: 8000, OutputTokens: 500, WindowTokens: 400000},
			},
		},
	}

	largeOutput := json.RawMessage(`{"output":"` + strings.Repeat("x", 120_000) + `"}`)
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand, out: largeOutput}), Config{Model: "gpt-5.3-codex"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	msg, err := eng.SubmitUserMessage(context.Background(), "run tools")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if msg.Content != "done" {
		t.Fatalf("assistant content = %q, want done", msg.Content)
	}
	if len(client.compactionCalls) != 2 {
		t.Fatalf("expected two compact calls (retry after 400), got %d", len(client.compactionCalls))
	}
	if len(client.compactionCalls[1].InputItems) != len(client.compactionCalls[0].InputItems) {
		t.Fatalf("expected repair to preserve item count, first=%d second=%d", len(client.compactionCalls[0].InputItems), len(client.compactionCalls[1].InputItems))
	}
	foundCollapsed := false
	for _, item := range client.compactionCalls[1].InputItems {
		if item.Type == llm.ResponseItemTypeFunctionCallOutput && item.CallID == "call_1" {
			foundCollapsed = isCollapsedCompactionOverflowShellOutput(item.Output)
		}
	}
	if !foundCollapsed {
		t.Fatalf("expected repaired retry to collapse shell output, got %+v", client.compactionCalls[1].InputItems)
	}
}
