package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"builder/prompts"
	"builder/server/llm"
	"builder/server/session"
	"builder/server/tools"
	"builder/shared/toolspec"
	"builder/shared/transcript"
)

type failOnIssuedWorktreeReminderObservation struct {
	failed bool
	calls  int
}

func (o *failOnIssuedWorktreeReminderObservation) ObservePersistedStore(_ context.Context, snapshot session.PersistedStoreSnapshot) error {
	o.calls++
	if !o.failed && snapshot.Meta.WorktreeReminder != nil && snapshot.Meta.WorktreeReminder.HasIssuedInGeneration {
		o.failed = true
		return errors.New("persist observer failed")
	}
	return nil
}

func TestFirstMetaInjectionUsesPendingWorktreeCWD(t *testing.T) {
	prevPrompt := prompts.WorktreeModePrompt
	prompts.WorktreeModePrompt = "enter {{branch}} {{cwd}}"
	defer func() { prompts.WorktreeModePrompt = prevPrompt }()

	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	worktree := t.TempDir()
	worktreeSubdir := filepath.Join(worktree, "pkg")
	if err := os.MkdirAll(worktreeSubdir, 0o755); err != nil {
		t.Fatalf("MkdirAll worktree subdir: %v", err)
	}
	writeTestFile(t, filepath.Join(workspace, agentsFileName), "stale workspace instruction")
	writeTestFile(t, filepath.Join(worktree, agentsFileName), "active worktree instruction")
	store, err := session.Create(t.TempDir(), "ws", workspace)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.SetWorktreeReminderState(&session.WorktreeReminderState{
		Mode:          session.WorktreeReminderModeEnter,
		Branch:        "feature/new",
		WorktreePath:  worktree,
		WorkspaceRoot: workspace,
		EffectiveCwd:  worktreeSubdir,
	}); err != nil {
		t.Fatalf("SetWorktreeReminderState: %v", err)
	}

	client := &fakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "ok"},
		OutputItems: []llm.ResponseItem{{
			Type:    llm.ResponseItemTypeMessage,
			Role:    llm.RoleAssistant,
			Phase:   llm.MessagePhaseFinal,
			Content: "ok",
		}},
		Usage: llm.Usage{WindowTokens: 200000},
	}}}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	if _, err := eng.SubmitUserMessage(context.Background(), "start in the new worktree"); err != nil {
		t.Fatalf("submit: %v", err)
	}

	messages := requestMessages(client.calls[0])
	if len(messages) < 3 {
		t.Fatalf("expected environment, agents, and user messages, got %+v", messages)
	}
	envMsg := messages[0]
	if envMsg.Role != llm.RoleDeveloper || envMsg.MessageType != llm.MessageTypeEnvironment {
		t.Fatalf("expected environment context first, got %+v", envMsg)
	}
	if !strings.Contains(envMsg.Content, "\nCWD: "+worktreeSubdir+"\n") {
		t.Fatalf("expected environment cwd to use pending worktree subdir %q, got %q", worktreeSubdir, envMsg.Content)
	}
	if strings.Contains(envMsg.Content, "\nCWD: "+workspace+"\n") {
		t.Fatalf("expected environment cwd not to use stale workspace %q, got %q", workspace, envMsg.Content)
	}
	agentsMsg := messages[1]
	if agentsMsg.Role != llm.RoleDeveloper || agentsMsg.MessageType != llm.MessageTypeAgentsMD || !strings.Contains(agentsMsg.Content, "source: "+filepath.Join(worktree, agentsFileName)) {
		t.Fatalf("expected active worktree AGENTS context second, got %+v", agentsMsg)
	}
	if strings.Contains(agentsMsg.Content, "stale workspace instruction") {
		t.Fatalf("expected stale workspace AGENTS context to be excluded, got %q", agentsMsg.Content)
	}
}

func TestSubmitUserMessageInjectsPendingWorktreeEnterReminder(t *testing.T) {
	prevPrompt := prompts.WorktreeModePrompt
	prompts.WorktreeModePrompt = "enter {{branch}} {{cwd}} {{worktree_path}} {{workspace_root}}"
	defer func() { prompts.WorktreeModePrompt = prevPrompt }()

	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.SetWorktreeReminderState(&session.WorktreeReminderState{
		Mode:          session.WorktreeReminderModeEnter,
		Branch:        "feature/enter",
		WorktreePath:  "/tmp/wt-enter",
		WorkspaceRoot: "/tmp/workspace",
		EffectiveCwd:  "/tmp/wt-enter/pkg",
	}); err != nil {
		t.Fatalf("SetWorktreeReminderState: %v", err)
	}

	client := &fakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "ok"},
		OutputItems: []llm.ResponseItem{{
			Type:    llm.ResponseItemTypeMessage,
			Role:    llm.RoleAssistant,
			Phase:   llm.MessagePhaseFinal,
			Content: "ok",
		}},
		Usage: llm.Usage{WindowTokens: 200000},
	}}}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	if _, err := eng.SubmitUserMessage(context.Background(), "continue"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected one model call, got %d", len(client.calls))
	}
	messages := requestMessages(client.calls[0])
	reminderIdx := -1
	for i, msg := range messages {
		if msg.Role == llm.RoleDeveloper && msg.MessageType == llm.MessageTypeWorktreeMode {
			reminderIdx = i
			if !strings.Contains(msg.Content, "feature/enter") || !strings.Contains(msg.Content, "/tmp/wt-enter/pkg") {
				t.Fatalf("unexpected worktree reminder content: %q", msg.Content)
			}
		}
	}
	if reminderIdx < 0 {
		t.Fatalf("expected worktree enter reminder, messages=%+v", messages)
	}
	state := store.Meta().WorktreeReminder
	if state == nil || !state.HasIssuedInGeneration || state.IssuedCompactionCount != 0 {
		t.Fatalf("unexpected persisted reminder state after submit: %+v", state)
	}
	var entry *ChatEntry
	for idx := range eng.ChatSnapshot().Entries {
		if eng.ChatSnapshot().Entries[idx].MessageType == llm.MessageTypeWorktreeMode {
			entry = &eng.ChatSnapshot().Entries[idx]
			break
		}
	}
	if entry == nil {
		t.Fatal("expected worktree reminder transcript entry")
	}
	if entry.Visibility != transcript.EntryVisibilityAll {
		t.Fatalf("worktree reminder visibility = %q, want all", entry.Visibility)
	}
	if entry.OngoingText != "Switched worktree to feature/enter: /tmp/wt-enter/pkg" || entry.CompactLabel != entry.OngoingText {
		t.Fatalf("ongoing=%q compact=%q, want branch-based switch label", entry.OngoingText, entry.CompactLabel)
	}
	if entry.SourcePath != "/tmp/wt-enter/pkg" {
		t.Fatalf("source path = %q, want effective cwd", entry.SourcePath)
	}
}

func TestRunStepLoopMaterializesPendingWorktreeReminder(t *testing.T) {
	prevPrompt := prompts.WorktreeModePrompt
	prompts.WorktreeModePrompt = "enter {{branch}}"
	defer func() { prompts.WorktreeModePrompt = prevPrompt }()

	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.SetWorktreeReminderState(&session.WorktreeReminderState{
		Mode:          session.WorktreeReminderModeEnter,
		Branch:        "feature/direct",
		WorktreePath:  "/tmp/wt-direct",
		WorkspaceRoot: "/tmp/workspace",
		EffectiveCwd:  "/tmp/wt-direct",
	}); err != nil {
		t.Fatalf("SetWorktreeReminderState: %v", err)
	}
	client := &fakeClient{responses: []llm.Response{{
		Assistant:   llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "ok"},
		OutputItems: []llm.ResponseItem{{Type: llm.ResponseItemTypeMessage, Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "ok"}},
		Usage:       llm.Usage{WindowTokens: 200000},
	}}}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	if _, err := eng.runStepLoop(context.Background(), "step-1"); err != nil {
		t.Fatalf("runStepLoop: %v", err)
	}

	messages := requestMessages(client.calls[0])
	reminderCount := 0
	for _, msg := range messages {
		if msg.Role == llm.RoleDeveloper && msg.MessageType == llm.MessageTypeWorktreeMode {
			reminderCount++
			if msg.CompactContent != "Switched worktree to feature/direct: /tmp/wt-direct" {
				t.Fatalf("compact content = %q", msg.CompactContent)
			}
		}
	}
	if reminderCount != 1 {
		t.Fatalf("expected one worktree reminder, got %d messages=%+v", reminderCount, messages)
	}
	state := store.Meta().WorktreeReminder
	if state == nil || !state.HasIssuedInGeneration {
		t.Fatalf("expected issued reminder state, got %+v", state)
	}
}

func TestRunStepLoopCountsPendingWorktreeReminderBeforeAutoCompaction(t *testing.T) {
	prevPrompt := prompts.WorktreeModePrompt
	prompts.WorktreeModePrompt = "enter {{branch}}"
	defer func() { prompts.WorktreeModePrompt = prevPrompt }()

	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.SetWorktreeReminderState(&session.WorktreeReminderState{
		Mode:          session.WorktreeReminderModeEnter,
		Branch:        "feature/compact",
		WorktreePath:  "/tmp/wt-compact",
		WorkspaceRoot: "/tmp/workspace",
		EffectiveCwd:  "/tmp/wt-compact",
	}); err != nil {
		t.Fatalf("SetWorktreeReminderState: %v", err)
	}

	sawReminderDuringPreCompactionCount := false
	client := &fakeCompactionClient{
		responses: []llm.Response{{
			Assistant:   llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "ok"},
			OutputItems: []llm.ResponseItem{{Type: llm.ResponseItemTypeMessage, Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "ok"}},
			Usage:       llm.Usage{WindowTokens: 2_000},
		}},
		inputTokenCountFn: func(req llm.Request) int {
			hasReminder := requestHasWorktreeReminder(req)
			if hasReminder && !requestHasCompactionCheckpoint(req) {
				sawReminderDuringPreCompactionCount = true
				return 1_000
			}
			return 100
		},
		compactionResponses: []llm.CompactionResponse{{
			OutputItems: []llm.ResponseItem{
				{Type: llm.ResponseItemTypeMessage, Role: llm.RoleUser, Content: "compacted seed"},
				{Type: llm.ResponseItemTypeCompaction, ID: "cmp_1", EncryptedContent: "enc_1"},
			},
			Usage: llm.Usage{InputTokens: 100, WindowTokens: 2_000},
		}},
	}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		Model:                 "gpt-5",
		ContextWindowTokens:   2_000,
		AutoCompactTokenLimit: 1_000,
		CompactionMode:        "native",
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := eng.appendMessage("", llm.Message{Role: llm.RoleUser, Content: "seed"}); err != nil {
		t.Fatalf("append seed: %v", err)
	}
	eng.setLastUsage(llm.Usage{InputTokens: 999, WindowTokens: 2_000})

	if _, err := eng.runStepLoop(context.Background(), "step-1"); err != nil {
		t.Fatalf("runStepLoop: %v", err)
	}
	if !sawReminderDuringPreCompactionCount {
		t.Fatal("expected auto-compaction token count to include pending worktree reminder")
	}
	if len(client.compactionCalls) != 1 {
		t.Fatalf("expected one auto-compaction call, got %d", len(client.compactionCalls))
	}
	if !requestHasWorktreeReminder(client.calls[0]) {
		t.Fatalf("expected post-compaction model request to include worktree reminder, messages=%+v", requestMessages(client.calls[0]))
	}
	state := store.Meta().WorktreeReminder
	if state == nil || !state.HasIssuedInGeneration || state.IssuedCompactionCount != 1 {
		t.Fatalf("expected reminder reissued after compaction, got %+v", state)
	}
}

func requestHasWorktreeReminder(req llm.Request) bool {
	for _, msg := range requestMessages(req) {
		if msg.Role == llm.RoleDeveloper && (msg.MessageType == llm.MessageTypeWorktreeMode || msg.MessageType == llm.MessageTypeWorktreeModeExit) {
			return true
		}
	}
	return false
}

func requestHasCompactionCheckpoint(req llm.Request) bool {
	for _, item := range req.Items {
		if item.Type == llm.ResponseItemTypeCompaction || item.MessageType == llm.MessageTypeCompactionSummary {
			return true
		}
	}
	return false
}

func TestWorktreeReminderPersistFailureDoesNotDuplicateMaterializedReminder(t *testing.T) {
	prevPrompt := prompts.WorktreeModePrompt
	prompts.WorktreeModePrompt = "enter {{branch}}"
	defer func() { prompts.WorktreeModePrompt = prevPrompt }()

	observer := &failOnIssuedWorktreeReminderObservation{}
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir, session.WithPersistenceObserver(observer))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.SetWorktreeReminderState(&session.WorktreeReminderState{
		Mode:          session.WorktreeReminderModeEnter,
		Branch:        "feature/fail",
		WorktreePath:  "/tmp/wt-fail",
		WorkspaceRoot: "/tmp/workspace",
		EffectiveCwd:  "/tmp/wt-fail",
	}); err != nil {
		t.Fatalf("SetWorktreeReminderState: %v", err)
	}
	client := &fakeClient{responses: []llm.Response{{
		Assistant:   llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "ok"},
		OutputItems: []llm.ResponseItem{{Type: llm.ResponseItemTypeMessage, Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "ok"}},
		Usage:       llm.Usage{WindowTokens: 200000},
	}}}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	if _, err := eng.SubmitUserMessage(context.Background(), "continue"); err == nil || !strings.Contains(err.Error(), "persist observer failed") {
		t.Fatalf("submit error = %v, want reminder state persistence failure", err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("expected no model calls after materialization failure, got %d", len(client.calls))
	}
	assertWorktreeReminderEntryCount(t, eng.ChatSnapshot(), 1)

	if _, err := eng.SubmitUserMessage(context.Background(), "continue again"); err != nil {
		t.Fatalf("retry submit: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected one model call after retry, got %d", len(client.calls))
	}
	assertWorktreeReminderEntryCount(t, eng.ChatSnapshot(), 1)
}

func assertWorktreeReminderEntryCount(t *testing.T, snapshot ChatSnapshot, want int) {
	t.Helper()
	got := 0
	for _, entry := range snapshot.Entries {
		if entry.MessageType == llm.MessageTypeWorktreeMode || entry.MessageType == llm.MessageTypeWorktreeModeExit {
			got++
		}
	}
	if got != want {
		t.Fatalf("worktree reminder entry count = %d, want %d entries=%+v", got, want, snapshot.Entries)
	}
}

func TestSubmitUserMessageInjectsPendingWorktreeExitReminder(t *testing.T) {
	prevPrompt := prompts.WorktreeModeExitPrompt
	prompts.WorktreeModeExitPrompt = "exit {{branch}} {{cwd}} {{worktree_path}} {{workspace_root}}"
	defer func() { prompts.WorktreeModeExitPrompt = prevPrompt }()

	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.SetWorktreeReminderState(&session.WorktreeReminderState{
		Mode:          session.WorktreeReminderModeExit,
		Branch:        "feature/exit",
		WorktreePath:  "/tmp/wt-exit",
		WorkspaceRoot: "/tmp/workspace",
		EffectiveCwd:  "/tmp/workspace/pkg",
	}); err != nil {
		t.Fatalf("SetWorktreeReminderState: %v", err)
	}

	client := &fakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "ok"},
		OutputItems: []llm.ResponseItem{{
			Type:    llm.ResponseItemTypeMessage,
			Role:    llm.RoleAssistant,
			Phase:   llm.MessagePhaseFinal,
			Content: "ok",
		}},
		Usage: llm.Usage{WindowTokens: 200000},
	}}}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	if _, err := eng.SubmitUserMessage(context.Background(), "continue"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	messages := requestMessages(client.calls[0])
	found := false
	for _, msg := range messages {
		if msg.Role == llm.RoleDeveloper && msg.MessageType == llm.MessageTypeWorktreeModeExit {
			found = true
			if !strings.Contains(msg.Content, "feature/exit") || !strings.Contains(msg.Content, "/tmp/workspace/pkg") {
				t.Fatalf("unexpected worktree exit reminder content: %q", msg.Content)
			}
		}
	}
	if !found {
		t.Fatalf("expected worktree exit reminder, messages=%+v", messages)
	}
}

func TestSubmitUserMessageMaterializesWorktreeReminderBeforeModelFailure(t *testing.T) {
	withGenerateRetryDelays(t, nil)

	prevPrompt := prompts.WorktreeModePrompt
	prompts.WorktreeModePrompt = "enter {{branch}}"
	defer func() { prompts.WorktreeModePrompt = prevPrompt }()

	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.SetWorktreeReminderState(&session.WorktreeReminderState{
		Mode:          session.WorktreeReminderModeEnter,
		Branch:        "feature/retry",
		WorktreePath:  "/tmp/wt-retry",
		WorkspaceRoot: "/tmp/workspace",
		EffectiveCwd:  "/tmp/wt-retry",
	}); err != nil {
		t.Fatalf("SetWorktreeReminderState: %v", err)
	}

	failingClient := &hookClient{beforeReturn: func() error { return context.DeadlineExceeded }}
	eng, err := New(store, failingClient, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	if _, err := eng.SubmitUserMessage(context.Background(), "continue"); err == nil {
		t.Fatal("expected submit failure")
	}
	state := store.Meta().WorktreeReminder
	if state == nil || !state.HasIssuedInGeneration || state.IssuedCompactionCount != 0 {
		t.Fatalf("unexpected reminder state after failed submit: %+v", state)
	}

	successClient := &fakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "ok"},
		OutputItems: []llm.ResponseItem{{
			Type:    llm.ResponseItemTypeMessage,
			Role:    llm.RoleAssistant,
			Phase:   llm.MessagePhaseFinal,
			Content: "ok",
		}},
		Usage: llm.Usage{WindowTokens: 200000},
	}}}
	eng.llm = successClient

	if _, err := eng.SubmitUserMessage(context.Background(), "continue again"); err != nil {
		t.Fatalf("submit retry: %v", err)
	}
	if len(successClient.calls) != 1 {
		t.Fatalf("expected one successful retry call, got %d", len(successClient.calls))
	}
	reminderCount := 0
	for _, msg := range requestMessages(successClient.calls[0]) {
		if msg.Role == llm.RoleDeveloper && msg.MessageType == llm.MessageTypeWorktreeMode {
			reminderCount++
		}
	}
	if reminderCount != 1 {
		t.Fatalf("expected materialized reminder after failed submit, got %d messages=%+v", reminderCount, requestMessages(successClient.calls[0]))
	}
}

func TestSubmitUserMessageUsesLatestPendingWorktreeReminder(t *testing.T) {
	prevPrompt := prompts.WorktreeModePrompt
	prompts.WorktreeModePrompt = "enter {{branch}}"
	defer func() { prompts.WorktreeModePrompt = prevPrompt }()

	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.SetWorktreeReminderState(&session.WorktreeReminderState{
		Mode:          session.WorktreeReminderModeEnter,
		Branch:        "feature/old",
		WorktreePath:  "/tmp/wt-old",
		WorkspaceRoot: "/tmp/workspace",
		EffectiveCwd:  "/tmp/wt-old",
	}); err != nil {
		t.Fatalf("SetWorktreeReminderState first: %v", err)
	}
	if err := store.SetWorktreeReminderState(&session.WorktreeReminderState{
		Mode:          session.WorktreeReminderModeEnter,
		Branch:        "feature/new",
		WorktreePath:  "/tmp/wt-new",
		WorkspaceRoot: "/tmp/workspace",
		EffectiveCwd:  "/tmp/wt-new",
	}); err != nil {
		t.Fatalf("SetWorktreeReminderState second: %v", err)
	}

	client := &fakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "ok"},
		OutputItems: []llm.ResponseItem{{
			Type:    llm.ResponseItemTypeMessage,
			Role:    llm.RoleAssistant,
			Phase:   llm.MessagePhaseFinal,
			Content: "ok",
		}},
		Usage: llm.Usage{WindowTokens: 200000},
	}}}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	if _, err := eng.SubmitUserMessage(context.Background(), "continue"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	messages := requestMessages(client.calls[0])
	for _, msg := range messages {
		if msg.Role != llm.RoleDeveloper || msg.MessageType != llm.MessageTypeWorktreeMode {
			continue
		}
		if !strings.Contains(msg.Content, "feature/new") {
			t.Fatalf("expected latest reminder state, got %q", msg.Content)
		}
		if strings.Contains(msg.Content, "feature/old") {
			t.Fatalf("did not expect stale reminder state, got %q", msg.Content)
		}
		return
	}
	t.Fatalf("expected worktree reminder, messages=%+v", messages)
}

func TestSubmitUserMessageReinjectsWorktreeReminderAfterCompactionGenerationChange(t *testing.T) {
	prevPrompt := prompts.WorktreeModePrompt
	prompts.WorktreeModePrompt = "enter {{branch}}"
	defer func() { prompts.WorktreeModePrompt = prevPrompt }()

	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.SetWorktreeReminderState(&session.WorktreeReminderState{
		Mode:                  session.WorktreeReminderModeEnter,
		Branch:                "feature/reinject",
		WorktreePath:          "/tmp/wt-reinject",
		WorkspaceRoot:         "/tmp/workspace",
		EffectiveCwd:          "/tmp/wt-reinject",
		HasIssuedInGeneration: true,
		IssuedCompactionCount: 0,
	}); err != nil {
		t.Fatalf("SetWorktreeReminderState: %v", err)
	}

	client := &fakeClient{responses: []llm.Response{
		{
			Assistant:   llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "ok-1"},
			OutputItems: []llm.ResponseItem{{Type: llm.ResponseItemTypeMessage, Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "ok-1"}},
			Usage:       llm.Usage{WindowTokens: 200000},
		},
		{
			Assistant:   llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "ok-2"},
			OutputItems: []llm.ResponseItem{{Type: llm.ResponseItemTypeMessage, Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "ok-2"}},
			Usage:       llm.Usage{WindowTokens: 200000},
		},
	}}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	eng.compactionRuntimeState().SetCount(1)

	if _, err := eng.SubmitUserMessage(context.Background(), "continue"); err != nil {
		t.Fatalf("submit 1: %v", err)
	}
	if _, err := eng.SubmitUserMessage(context.Background(), "continue again"); err != nil {
		t.Fatalf("submit 2: %v", err)
	}
	if len(client.calls) != 2 {
		t.Fatalf("expected two model calls, got %d", len(client.calls))
	}
	firstCount := 0
	for _, msg := range requestMessages(client.calls[0]) {
		if msg.Role == llm.RoleDeveloper && msg.MessageType == llm.MessageTypeWorktreeMode {
			firstCount++
		}
	}
	if firstCount != 1 {
		t.Fatalf("expected one reinjected worktree reminder in first request, got %d messages=%+v", firstCount, requestMessages(client.calls[0]))
	}
	secondCount := 0
	for _, msg := range requestMessages(client.calls[1]) {
		if msg.Role == llm.RoleDeveloper && msg.MessageType == llm.MessageTypeWorktreeMode {
			secondCount++
		}
	}
	if secondCount != 1 {
		t.Fatalf("expected latest materialized worktree reminder in second request, got %d messages=%+v", secondCount, requestMessages(client.calls[1]))
	}
	state := store.Meta().WorktreeReminder
	if state == nil || !state.HasIssuedInGeneration || state.IssuedCompactionCount != 1 {
		t.Fatalf("unexpected persisted reminder state after reinjection: %+v", state)
	}
}

func TestSubmitUserMessageReplacesHistoricalWorktreeReminderWithLatestState(t *testing.T) {
	prevEnterPrompt := prompts.WorktreeModePrompt
	prevExitPrompt := prompts.WorktreeModeExitPrompt
	prompts.WorktreeModePrompt = "enter {{branch}}"
	prompts.WorktreeModeExitPrompt = "exit {{branch}}"
	defer func() {
		prompts.WorktreeModePrompt = prevEnterPrompt
		prompts.WorktreeModeExitPrompt = prevExitPrompt
	}()

	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	client := &fakeClient{responses: []llm.Response{
		{Assistant: llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "ok-1"}, OutputItems: []llm.ResponseItem{{Type: llm.ResponseItemTypeMessage, Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "ok-1"}}, Usage: llm.Usage{WindowTokens: 200000}},
		{Assistant: llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "ok-2"}, OutputItems: []llm.ResponseItem{{Type: llm.ResponseItemTypeMessage, Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal, Content: "ok-2"}}, Usage: llm.Usage{WindowTokens: 200000}},
	}}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	if err := store.SetWorktreeReminderState(&session.WorktreeReminderState{Mode: session.WorktreeReminderModeEnter, Branch: "feature/enter", WorktreePath: "/tmp/wt-enter", WorkspaceRoot: "/tmp/workspace", EffectiveCwd: "/tmp/wt-enter"}); err != nil {
		t.Fatalf("SetWorktreeReminderState enter: %v", err)
	}
	if _, err := eng.SubmitUserMessage(context.Background(), "first"); err != nil {
		t.Fatalf("submit first: %v", err)
	}
	if err := store.SetWorktreeReminderState(&session.WorktreeReminderState{Mode: session.WorktreeReminderModeExit, Branch: "feature/exit", WorktreePath: "/tmp/wt-exit", WorkspaceRoot: "/tmp/workspace", EffectiveCwd: "/tmp/workspace"}); err != nil {
		t.Fatalf("SetWorktreeReminderState exit: %v", err)
	}
	if _, err := eng.SubmitUserMessage(context.Background(), "second"); err != nil {
		t.Fatalf("submit second: %v", err)
	}

	if len(client.calls) != 2 {
		t.Fatalf("expected two model calls, got %d", len(client.calls))
	}
	firstMessages := requestMessages(client.calls[0])
	firstCount := 0
	for _, msg := range firstMessages {
		if msg.Role == llm.RoleDeveloper && msg.MessageType == llm.MessageTypeWorktreeMode {
			firstCount++
		}
	}
	if firstCount != 1 {
		t.Fatalf("expected one enter reminder in first request, got %d messages=%+v", firstCount, firstMessages)
	}
	secondMessages := requestMessages(client.calls[1])
	enterCount := 0
	exitCount := 0
	for _, msg := range secondMessages {
		switch {
		case msg.Role == llm.RoleDeveloper && msg.MessageType == llm.MessageTypeWorktreeMode:
			enterCount++
		case msg.Role == llm.RoleDeveloper && msg.MessageType == llm.MessageTypeWorktreeModeExit:
			exitCount++
		}
	}
	if enterCount != 0 || exitCount != 1 {
		t.Fatalf("expected only latest exit reminder in second request, got enter=%d exit=%d messages=%+v", enterCount, exitCount, secondMessages)
	}
	snapshot := eng.ChatSnapshot()
	detailEntries := 0
	for _, entry := range snapshot.Entries {
		if entry.Role != string(transcript.EntryRoleDeveloperContext) {
			continue
		}
		if strings.Contains(entry.Text, "enter feature/enter") || strings.Contains(entry.Text, "exit feature/exit") {
			detailEntries++
		}
	}
	if detailEntries != 2 {
		t.Fatalf("expected detail transcript to retain both reminder rows, got %d entries=%+v", detailEntries, snapshot.Entries)
	}
}
