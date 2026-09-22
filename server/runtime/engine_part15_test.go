package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"core/internal/testharness/postprocessfixture"
	"core/prompts"
	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	shelltool "core/server/tools/shell"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func TestAutoCompactionRemoteReplacesHistoryAndCarriesCompactionItem(t *testing.T) {
	t.Parallel()
	store := mustCreateTestSession(t)

	client := &fakeCompactionClient{
		responses: []llm.Response{
			{
				Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("working")},
				ToolCalls: []llm.ToolCall{
					{ID: "call_1", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"command":"pwd"}`)},
				},
				Usage: llm.Usage{InputTokens: 190000, OutputTokens: 2000, WindowTokens: 200000},
			},
			{
				Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("done")},
				Usage:     llm.Usage{InputTokens: 2000, OutputTokens: 1000, WindowTokens: 200000},
			},
		},
		compactionResponses: []llm.CompactionResponse{
			{
				Checkpoint: llm.ResponseItem{
					Type:             llm.ResponseItemTypeCompaction,
					ID:               textutil.Value("cmp_1"),
					EncryptedContent: textutil.Value("enc_1"),
				},
				Usage: llm.Usage{InputTokens: 12000, OutputTokens: 1000, WindowTokens: 200000},
			},
		},
	}

	eng := mustNewTestEngine(t, store, client, newTestToolRegistry(t, tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: fakeTool{name: toolspec.ToolExecCommand}}), Config{
		Model:               "gpt-5",
		ContextWindowTokens: 200_000,
	})

	msg, err := eng.SubmitUserMessage(context.Background(), "run tools")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if messageContent(msg) != "done" {
		t.Fatalf("assistant content = %q, want done", messageContent(msg))
	}
	if len(client.compactionCalls) != 1 {
		t.Fatalf("expected one remote compaction call, got %d", len(client.compactionCalls))
	}
	if len(client.calls) < 2 {
		t.Fatalf("expected second model call after compaction, got %d calls", len(client.calls))
	}

	foundCompactionItem := false
	for _, item := range client.calls[1].Items {
		if item.Type == llm.ResponseItemTypeCompaction && item.EncryptedContent != nil && *item.EncryptedContent == "enc_1" {
			foundCompactionItem = true
			break
		}
	}
	if !foundCompactionItem {
		t.Fatalf("expected compaction item in post-compaction request, got %+v", client.calls[1].Items)
	}

	events, err := collectTestEventRecords(store)
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

func TestCompactionReplacementPayloadEmbedsReinjectedBaseMetaAndPreservedUserMessageAtomically(t *testing.T) {
	t.Parallel()
	store := mustCreateTestSession(t)
	client := &fakeCompactionClient{compactionResponses: []llm.CompactionResponse{{
		Checkpoint: llm.ResponseItem{
			Type:             llm.ResponseItemTypeCompaction,
			ID:               textutil.Value("cmp_1"),
			EncryptedContent: textutil.Value("enc_1"),
		},
		Usage: llm.Usage{InputTokens: 1000, OutputTokens: 100, WindowTokens: 200000},
	}}}
	manager, err := shelltool.NewManager(t.TempDir(), shelltool.WithMinimumExecToBgTime(time.Millisecond))
	if err != nil {
		t.Fatalf("new shell manager: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := manager.Close(); closeErr != nil {
			t.Fatalf("close shell manager: %v", closeErr)
		}
	})
	startShell := func(ownerSessionID, displayCommand string) shelltool.ExecResult {
		t.Helper()
		result, startErr := manager.Start(context.Background(), shelltool.ExecRequest{
			Postprocessor:  postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
			Command:        []string{"/bin/sh", "-c", "sleep 5"},
			DisplayCommand: displayCommand,
			OwnerSessionID: ownerSessionID,
			Workdir:        store.Meta().WorkspaceRoot,
			YieldTime:      time.Millisecond,
		})
		if startErr != nil {
			t.Fatalf("start shell %q: %v", displayCommand, startErr)
		}
		if !result.Running {
			t.Fatalf("shell %q did not remain running: %+v", displayCommand, result)
		}
		return result
	}
	ownedShell := startShell(store.Meta().SessionID, "owned running shell")
	foreignShell := startShell("foreign-session", "foreign running shell")
	eng := mustNewTestEngine(t, store, client, newTestToolRegistry(t, tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: fakeTool{name: toolspec.ToolExecCommand}}), Config{
		Model:                  "gpt-5",
		BackgroundShellManager: manager,
	})
	if _, err := eng.SetGoal(t.Context(), "preserve atomic goal context", session.GoalActorUser); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	mustSetWorktreeReminderState(t, store, testWorktreeReminderState(
		session.WorktreeReminderModeEnter,
		"feature/goal",
		t.TempDir(),
		t.TempDir(),
		t.TempDir(),
	))
	stepID := runtimeTestStepID("step-1")
	restoreStep := setTestActiveStep(eng, stepID)
	defer restoreStep()
	if err := eng.steer(stepID, steerMessagesWithPersistenceIntent(steeringPriorityNormal, steeringMessageEventDefault, true, []llm.Message{{Role: llm.RoleUser, Content: textutil.Value("seed")}})); err != nil {
		t.Fatalf("append seed message: %v", err)
	}

	if _, _, err := eng.compactNow(context.Background(), stepID, compactionModeManual, compactionInstructionsInput{}, true); err != nil {
		t.Fatalf("compactNow: %v", err)
	}

	events, err := collectTestEventRecords(store)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	historyIndex := -1
	var replacement historyReplacementPayload
	for idx, evt := range events {
		if evt.Kind != "history_replaced" {
			continue
		}
		historyIndex = idx
		replacement = persistedHistoryReplacementForTest(t, evt)
		break
	}
	if historyIndex < 0 {
		t.Fatalf("expected history_replaced event, got %+v", events)
	}
	environmentIndex, goalIndex, worktreeIndex, carryoverIndex, reminderIndex := -1, -1, -1, -1, -1
	goalCount := 0
	for idx, item := range replacement.Items {
		if item.Type == llm.ResponseItemTypeMessage &&
			item.Role != nil &&
			*item.Role == llm.RoleDeveloper &&
			item.MessageType == nil &&
			item.Content != nil &&
			strings.Contains(*item.Content, ownedShell.SessionID) {
			reminderIndex = idx
			if !strings.Contains(*item.Content, ownedShell.SessionID) ||
				!strings.Contains(*item.Content, "owned running shell") {
				t.Fatalf("running-shell reminder omitted owned shell data: %+v", item)
			}
			if strings.Contains(*item.Content, foreignShell.SessionID) ||
				strings.Contains(*item.Content, "foreign running shell") {
				t.Fatalf("running-shell reminder included foreign shell data: %+v", item)
			}
		}
		if item.MessageType == nil {
			continue
		}
		switch *item.MessageType {
		case llm.MessageTypeEnvironment:
			environmentIndex = idx
		case llm.MessageTypeActiveGoalContinuation:
			goalIndex = idx
			goalCount++
			if item.Content == nil || *item.Content != prompts.RenderActiveGoalContinuationPrompt("preserve atomic goal context") {
				t.Fatalf("active-goal continuation content = %v", item.Content)
			}
		case llm.MessageTypeWorktreeMode:
			worktreeIndex = idx
		case llm.MessageTypeCompactionPreservedUserMessage:
			carryoverIndex = idx
			if item.Content == nil || !strings.Contains(*item.Content, "seed") {
				t.Fatalf("compaction-preserved user message lost the last visible user message: %+v", item)
			}
		}
	}
	if environmentIndex < 0 || goalIndex < 0 || worktreeIndex < 0 || carryoverIndex < 0 || reminderIndex < 0 || goalCount != 1 {
		t.Fatalf("replacement payload must embed base meta, one active-goal continuation, worktree context, running-shell reminder, environment, and compaction-preserved user message: %+v", replacement.Items)
	}
	if !(worktreeIndex < goalIndex && goalIndex < reminderIndex && reminderIndex < environmentIndex && environmentIndex < carryoverIndex) || carryoverIndex != len(replacement.Items)-1 {
		t.Fatalf("replacement payload order must be stable meta, environment, then carryover: %+v", replacement.Items)
	}
	for _, evt := range events[historyIndex+1:] {
		if evt.Kind != "message" {
			continue
		}
		msg := persistedMessageForTest(t, evt)
		if msg.Role == llm.RoleDeveloper && msg.MessageType != nil &&
			(*msg.MessageType == llm.MessageTypeEnvironment ||
				*msg.MessageType == llm.MessageTypeActiveGoalContinuation ||
				*msg.MessageType == llm.MessageTypeCompactionPreservedUserMessage) {
			t.Fatalf("base meta, active-goal continuation, and compaction-preserved user message must be embedded in the replacement payload, not steered separately afterward: events=%+v", events)
		}
	}

	reopenedStore := mustOpenTestSession(t, store.Dir())
	reopened := mustNewTestEngine(t, reopenedStore, &fakeClient{}, tools.NewRegistry(), Config{
		Model:                  "gpt-5",
		BackgroundShellManager: manager,
	})
	reopenedReminder := false
	for _, item := range reopened.transcriptRuntimeState().SnapshotItems() {
		if item.Type != llm.ResponseItemTypeMessage || item.Content == nil {
			continue
		}
		if strings.Contains(*item.Content, foreignShell.SessionID) {
			t.Fatalf("reopened replacement leaked foreign shell identity: %+v", item)
		}
		if item.Role != nil &&
			*item.Role == llm.RoleDeveloper &&
			item.MessageType == nil &&
			strings.Contains(*item.Content, ownedShell.SessionID) {
			reopenedReminder = true
		}
	}
	if !reopenedReminder {
		t.Fatalf("reopened replacement omitted running shell reminder: %+v", reopened.transcriptRuntimeState().SnapshotItems())
	}
}

func TestCompactionReplacementCapturesShellsStillRunningWhenCompactionCompletes(t *testing.T) {
	t.Parallel()
	store := mustCreateTestSession(t)
	manager, err := shelltool.NewManager(t.TempDir(), shelltool.WithMinimumExecToBgTime(time.Millisecond))
	if err != nil {
		t.Fatalf("new shell manager: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := manager.Close(); closeErr != nil {
			t.Fatalf("close shell manager: %v", closeErr)
		}
	})

	finishing, err := manager.Start(context.Background(), shelltool.ExecRequest{
		Postprocessor:  postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:        []string{"/bin/sh", "-c", "read line"},
		DisplayCommand: "finishing shell",
		OwnerSessionID: store.Meta().SessionID,
		Workdir:        store.Meta().WorkspaceRoot,
		YieldTime:      time.Millisecond,
		KeepStdinOpen:  true,
	})
	if err != nil {
		t.Fatalf("start finishing shell: %v", err)
	}
	if !finishing.Running {
		t.Fatalf("finishing shell did not remain running: %+v", finishing)
	}
	remaining, err := manager.Start(context.Background(), shelltool.ExecRequest{
		Postprocessor:  postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:        []string{"/bin/sh", "-c", "sleep 30"},
		DisplayCommand: "remaining shell",
		OwnerSessionID: store.Meta().SessionID,
		Workdir:        store.Meta().WorkspaceRoot,
		YieldTime:      time.Millisecond,
	})
	if err != nil {
		t.Fatalf("start remaining shell: %v", err)
	}
	if !remaining.Running {
		t.Fatalf("remaining shell did not remain running: %+v", remaining)
	}

	client := &heldRuntimeCompactionClient{
		fakeCompactionClient: &fakeCompactionClient{compactionResponses: []llm.CompactionResponse{{
			Checkpoint: llm.ResponseItem{
				Type:             llm.ResponseItemTypeCompaction,
				ID:               textutil.Value("cmp_running_shells"),
				EncryptedContent: textutil.Value("enc_running_shells"),
			},
			Usage: llm.Usage{InputTokens: 1000, OutputTokens: 100, WindowTokens: 200000},
		}}},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	var releaseOnce sync.Once
	releaseCompaction := func() {
		releaseOnce.Do(func() {
			close(client.release)
		})
	}
	t.Cleanup(releaseCompaction)
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model:                  "gpt-5",
		BackgroundShellManager: manager,
	})
	if err := steerTestActiveStep(eng, "running-shell-input", steerMessagesWithPersistenceIntent(
		steeringPriorityNormal,
		steeringMessageEventNone,
		true,
		[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("compact this")}},
	)); err != nil {
		t.Fatalf("persist compaction input: %v", err)
	}

	compactionDone := make(chan error, 1)
	stepID := runtimeTestStepID("running-shell-compaction")
	restoreStep := setTestActiveStep(eng, stepID)
	go func() {
		_, _, compactErr := eng.compactNow(context.Background(), stepID, compactionModeManual, compactionInstructionsInput{}, false)
		compactionDone <- compactErr
	}()
	select {
	case <-client.started:
	case <-time.After(runtimeTestSynchronizationTimeout):
		restoreStep()
		t.Fatal("timed out waiting for compaction request")
	}

	finished, err := manager.WriteStdin(context.Background(), shelltool.WriteRequest{
		SessionID:      finishing.SessionID,
		Input:          "done\n",
		YieldTime:      time.Second,
		MaxOutputChars: 16_000,
	})
	if err != nil {
		restoreStep()
		t.Fatalf("finish shell during compaction: %v", err)
	}
	if finished.Running {
		restoreStep()
		t.Fatalf("finishing shell remained running: %+v", finished)
	}
	releaseCompaction()
	select {
	case err := <-compactionDone:
		restoreStep()
		if err != nil {
			t.Fatalf("compaction: %v", err)
		}
	case <-time.After(runtimeTestSynchronizationTimeout):
		restoreStep()
		t.Fatal("timed out waiting for compaction completion")
	}

	var reminder string
	reminderIndex, environmentIndex := -1, -1
	for index, item := range eng.transcriptRuntimeState().SnapshotItems() {
		if item.Type != llm.ResponseItemTypeMessage {
			continue
		}
		if item.Role != nil && *item.Role == llm.RoleDeveloper && item.MessageType == nil &&
			item.Content != nil && strings.Contains(*item.Content, remaining.SessionID) {
			reminder = *item.Content
			reminderIndex = index
		}
		if item.MessageType != nil && *item.MessageType == llm.MessageTypeEnvironment {
			environmentIndex = index
		}
	}
	if reminderIndex < 0 {
		t.Fatalf("replacement omitted running shell reminder: %+v", eng.transcriptRuntimeState().SnapshotItems())
	}
	if !strings.Contains(reminder, "remaining shell") || strings.Contains(reminder, finishing.SessionID) {
		t.Fatalf("running shell reminder membership = %q, want remaining shell only", reminder)
	}
	if environmentIndex < 0 || reminderIndex != environmentIndex-1 {
		t.Fatalf("running shell reminder must immediately precede Environment: items=%+v", eng.transcriptRuntimeState().SnapshotItems())
	}
	if snapshot, err := manager.Snapshot(remaining.SessionID); err != nil || !snapshot.Running {
		t.Fatalf("remaining shell must still be running after compaction: snapshot=%+v error=%v", snapshot, err)
	}
	if err := manager.Kill(remaining.SessionID); err != nil {
		t.Fatalf("complete remaining shell after compaction: %v", err)
	}
	for _, item := range eng.transcriptRuntimeState().SnapshotItems() {
		if item.Type != llm.ResponseItemTypeMessage || item.Content == nil {
			continue
		}
		if item.Role != nil &&
			*item.Role == llm.RoleDeveloper &&
			item.MessageType == nil &&
			strings.Contains(*item.Content, remaining.SessionID) {
			return
		}
	}
	t.Fatalf("running shell reminder changed after shell completion: %+v", eng.transcriptRuntimeState().SnapshotItems())
}

func TestCompactionReplacementOmitsRunningShellReminderWhenNoOwnedShellsRemain(t *testing.T) {
	t.Parallel()
	store := mustCreateTestSession(t)
	manager, err := shelltool.NewManager(t.TempDir(), shelltool.WithMinimumExecToBgTime(time.Millisecond))
	if err != nil {
		t.Fatalf("new shell manager: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := manager.Close(); closeErr != nil {
			t.Fatalf("close shell manager: %v", closeErr)
		}
	})
	foreign, err := manager.Start(context.Background(), shelltool.ExecRequest{
		Postprocessor:  postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:        []string{"/bin/sh", "-c", "sleep 30"},
		DisplayCommand: "foreign running shell",
		OwnerSessionID: "foreign-session",
		Workdir:        store.Meta().WorkspaceRoot,
		YieldTime:      time.Millisecond,
	})
	if err != nil {
		t.Fatalf("start foreign shell: %v", err)
	}
	if !foreign.Running {
		t.Fatalf("foreign shell did not remain running: %+v", foreign)
	}

	client := &fakeCompactionClient{compactionResponses: []llm.CompactionResponse{{
		Checkpoint: llm.ResponseItem{
			Type:             llm.ResponseItemTypeCompaction,
			ID:               textutil.Value("cmp_no_owned_shells"),
			EncryptedContent: textutil.Value("enc_no_owned_shells"),
		},
		Usage: llm.Usage{InputTokens: 1000, OutputTokens: 100, WindowTokens: 200000},
	}}}
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model:                  "gpt-5",
		BackgroundShellManager: manager,
	})
	if err := steerTestActiveStep(eng, "no-owned-shells-input", steerMessagesWithPersistenceIntent(
		steeringPriorityNormal,
		steeringMessageEventNone,
		true,
		[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("compact this")}},
	)); err != nil {
		t.Fatalf("persist compaction input: %v", err)
	}
	if _, _, err := compactNowInActiveTestRun(t, eng, compactionModeManual, compactionInstructionsInput{}); err != nil {
		t.Fatalf("compaction: %v", err)
	}

	items := eng.transcriptRuntimeState().SnapshotItems()
	compactedIndex, environmentIndex := -1, -1
	for index, item := range items {
		if item.Type == llm.ResponseItemTypeCompaction ||
			(item.Type == llm.ResponseItemTypeMessage && item.MessageType != nil &&
				*item.MessageType == llm.MessageTypeCompactionSummary) {
			compactedIndex = index
		}
		if item.Type == llm.ResponseItemTypeMessage && item.MessageType != nil &&
			*item.MessageType == llm.MessageTypeEnvironment {
			environmentIndex = index
		}
	}
	if compactedIndex < 0 || environmentIndex <= compactedIndex {
		t.Fatalf("replacement missing compacted output and Environment: %+v", items)
	}
	for _, item := range items[compactedIndex+1 : environmentIndex] {
		if item.Type == llm.ResponseItemTypeMessage &&
			item.Role != nil &&
			*item.Role == llm.RoleDeveloper &&
			item.MessageType == nil {
			t.Fatalf("replacement emitted a running-shell reminder without owned shells: %+v", items)
		}
		if item.Content != nil && strings.Contains(*item.Content, foreign.SessionID) {
			t.Fatalf("replacement leaked foreign shell identity: %+v", items)
		}
	}
}

func TestCompactionRunningShellReminderNormalizesAndLimitsCommandPreview(t *testing.T) {
	t.Parallel()
	store := mustCreateTestSession(t)
	manager, err := shelltool.NewManager(t.TempDir(), shelltool.WithMinimumExecToBgTime(time.Millisecond))
	if err != nil {
		t.Fatalf("new shell manager: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := manager.Close(); closeErr != nil {
			t.Fatalf("close shell manager: %v", closeErr)
		}
	})
	longUnicodeArgument := strings.Repeat("界", 150)
	displayCommand := "kent run \\\n  --workspace /tmp/project \\\n  --message " + longUnicodeArgument
	shell, err := manager.Start(context.Background(), shelltool.ExecRequest{
		Postprocessor:  postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:        []string{"/bin/sh", "-c", "sleep 30"},
		DisplayCommand: displayCommand,
		OwnerSessionID: store.Meta().SessionID,
		Workdir:        store.Meta().WorkspaceRoot,
		YieldTime:      time.Millisecond,
	})
	if err != nil {
		t.Fatalf("start shell: %v", err)
	}
	if !shell.Running {
		t.Fatalf("shell did not remain running: %+v", shell)
	}

	client := &fakeCompactionClient{compactionResponses: []llm.CompactionResponse{{
		Checkpoint: llm.ResponseItem{
			Type:             llm.ResponseItemTypeCompaction,
			ID:               textutil.Value("cmp_shell_preview"),
			EncryptedContent: textutil.Value("enc_shell_preview"),
		},
		Usage: llm.Usage{InputTokens: 1000, OutputTokens: 100, WindowTokens: 200000},
	}}}
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model:                  "gpt-5",
		BackgroundShellManager: manager,
	})
	if err := steerTestActiveStep(eng, "shell-preview-input", steerMessagesWithPersistenceIntent(
		steeringPriorityNormal,
		steeringMessageEventNone,
		true,
		[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("compact this")}},
	)); err != nil {
		t.Fatalf("persist compaction input: %v", err)
	}
	if _, _, err := compactNowInActiveTestRun(t, eng, compactionModeManual, compactionInstructionsInput{}); err != nil {
		t.Fatalf("compaction: %v", err)
	}

	normalizedCommand := strings.Join(strings.Fields(displayCommand), " ")
	wantPreview := string([]rune(normalizedCommand)[:compactionRunningShellCommandPreviewLimit-1]) + "…"
	prefix := shell.SessionID + ": `"
	var preview string
	for _, item := range eng.transcriptRuntimeState().SnapshotItems() {
		if item.Type != llm.ResponseItemTypeMessage ||
			item.Role == nil ||
			*item.Role != llm.RoleDeveloper ||
			item.MessageType != nil ||
			item.Content == nil {
			continue
		}
		for _, line := range strings.Split(*item.Content, "\n") {
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			if !strings.HasSuffix(line, "`") {
				t.Fatalf("running shell entry is not a single-line code preview: %q", line)
			}
			preview = strings.TrimSuffix(strings.TrimPrefix(line, prefix), "`")
		}
	}
	if preview == "" {
		t.Fatalf("replacement omitted running shell command preview: %+v", eng.transcriptRuntimeState().SnapshotItems())
	}
	if preview != wantPreview {
		t.Fatalf("running shell command preview = %q, want normalized first %d Unicode code points %q", preview, compactionRunningShellCommandPreviewLimit, wantPreview)
	}
	if !strings.Contains(preview, "--workspace /tmp/project") || !strings.Contains(preview, "--message") {
		t.Fatalf("running shell command preview omitted continuation-line arguments: %q", preview)
	}
	if !strings.HasSuffix(preview, "…") {
		t.Fatalf("running shell command preview lacks a truncation marker: %q", preview)
	}
	if len([]rune(preview)) != compactionRunningShellCommandPreviewLimit {
		t.Fatalf("running shell command preview length = %d, want %d Unicode code points", len([]rune(preview)), compactionRunningShellCommandPreviewLimit)
	}
}

type failOnHistoryReplacementAgentResetObservation struct {
	failed bool
}

func (o *failOnHistoryReplacementAgentResetObservation) ObservePersistedStore(_ context.Context, snapshot session.PersistedStoreSnapshot) error {
	if !o.failed && snapshot.Meta.LastSequence >= 2 {
		o.failed = true
		return errors.New("persist observer failed after history replacement append")
	}
	return nil
}

type committedCompactionFixture struct {
	store  *session.Store
	engine *Engine
	client *fakeCompactionClient
	events []Event
}

func newCommittedCompactionFixture(t *testing.T, observer session.PersistenceObserver) *committedCompactionFixture {
	t.Helper()
	store := mustCreateTestSessionAt(t, t.TempDir(), session.WithPersistenceObserver(observer))
	if err := store.MarkModelDispatchLocked(session.LockedContract{
		Model:             "gpt-5",
		SystemPrompt:      "stale system prompt",
		HasSystemPrompt:   true,
		ReviewerPrompt:    "stale reviewer prompt",
		HasReviewerPrompt: true,
	}); err != nil {
		t.Fatalf("lock prompt snapshots: %v", err)
	}
	client := &fakeCompactionClient{
		caps: llm.ProviderCapabilities{
			ProviderID:               "openai",
			SupportsResponsesAPI:     true,
			SupportsResponsesCompact: true,
			IsOpenAIFirstParty:       true,
		},
		compactionResponses: []llm.CompactionResponse{{
			Checkpoint: llm.ResponseItem{
				Type:             llm.ResponseItemTypeCompaction,
				ID:               textutil.Value("cmp_1"),
				EncryptedContent: textutil.Value("enc_1"),
			},
			Usage: llm.Usage{InputTokens: 1000, OutputTokens: 100, WindowTokens: 200000},
		}},
	}
	fixture := &committedCompactionFixture{store: store, client: client}
	fixture.engine = mustNewTestEngine(t, store, client, newTestToolRegistry(t, tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: fakeTool{name: toolspec.ToolExecCommand}}), Config{
		Model:   "gpt-5",
		OnEvent: func(event Event) { fixture.events = append(fixture.events, event) },
	})
	if err := fixture.engine.steer(runtimeTestStepID("step-1"), steerMessagesWithPersistenceIntent(steeringPriorityNormal, steeringMessageEventDefault, true, []llm.Message{{Role: llm.RoleUser, Content: textutil.Value("seed")}})); err != nil {
		t.Fatalf("append seed message: %v", err)
	}
	fixture.engine.compactionRuntimeState().SetSoonReminderIssued(true)
	if err := store.SetCompactionSoonReminderIssued(true); err != nil {
		t.Fatalf("persist compaction reminder: %v", err)
	}
	return fixture
}

func activeGoalCompactionTestClient() *fakeCompactionClient {
	return &fakeCompactionClient{compactionResponses: []llm.CompactionResponse{{
		Checkpoint: llm.ResponseItem{
			Type:             llm.ResponseItemTypeCompaction,
			ID:               textutil.Value("cmp_goal"),
			EncryptedContent: textutil.Value("enc_goal"),
		},
		Usage: llm.Usage{InputTokens: 1000, OutputTokens: 100, WindowTokens: 200000},
	}}}
}

func activeGoalContinuationMessages(items []llm.ResponseItem) []llm.Message {
	messages := llm.MessagesFromItems(items)
	out := make([]llm.Message, 0, 1)
	for _, message := range messages {
		if message.Role == llm.RoleDeveloper && message.MessageType != nil && *message.MessageType == llm.MessageTypeActiveGoalContinuation {
			out = append(out, message)
		}
	}
	return out
}

func assertSingleActiveGoalContinuation(t *testing.T, items []llm.ResponseItem, objective string) {
	messages := activeGoalContinuationMessages(items)
	if len(messages) != 1 {
		t.Fatalf("active-goal continuation count = %d, want 1; messages=%+v", len(messages), messages)
	}
	if messageContent(messages[0]) != prompts.RenderActiveGoalContinuationPrompt(objective) {
		t.Fatalf("active-goal continuation content = %q", messageContent(messages[0]))
	}
}

func workflowModeMessagesFromItems(items []llm.ResponseItem) []llm.Message {
	messages := llm.MessagesFromItems(items)
	out := make([]llm.Message, 0, 1)
	for _, message := range messages {
		if message.Role == llm.RoleDeveloper && message.MessageType != nil && *message.MessageType == llm.MessageTypeWorkflowMode {
			out = append(out, message)
		}
	}
	return out
}

func TestAutoCompactionRetries400ByCollapsingShellOutput(t *testing.T) {
	t.Parallel()
	store := mustCreateTestSession(t)

	client := &fakeCompactionClient{
		responses: []llm.Response{
			{
				Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("working")},
				ToolCalls: []llm.ToolCall{
					{ID: "call_1", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"command":"pwd"}`)},
				},
				Usage: llm.Usage{InputTokens: 390000, OutputTokens: 1000, WindowTokens: 400000},
			},
			{
				Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("done")},
				Usage:     llm.Usage{InputTokens: 2000, OutputTokens: 500, WindowTokens: 400000},
			},
		},
		compactionErrors: []error{
			&llm.ProviderAPIError{ProviderID: "openai", StatusCode: 400, Code: llm.UnifiedErrorCodeContextLengthOverflow, ProviderCode: "context_length_exceeded", Message: "prompt exceeded"},
			nil,
		},
		compactionResponses: []llm.CompactionResponse{
			{
				Checkpoint: llm.ResponseItem{
					Type:             llm.ResponseItemTypeCompaction,
					ID:               textutil.Value("cmp_1"),
					EncryptedContent: textutil.Value("enc_1"),
				},
				Usage: llm.Usage{InputTokens: 8000, OutputTokens: 500, WindowTokens: 400000},
			},
		},
	}

	largeOutput := json.RawMessage(`{"output":"` + strings.Repeat("x", 120_000) + `"}`)
	eng := mustNewTestEngine(t, store, client, newTestToolRegistry(t, tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: fakeTool{name: toolspec.ToolExecCommand, out: largeOutput}}), Config{Model: "gpt-5.3-codex"})

	msg, err := eng.SubmitUserMessage(context.Background(), "run tools")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if messageContent(msg) != "done" {
		t.Fatalf("assistant content = %q, want done", messageContent(msg))
	}
	if len(client.compactionCalls) != 2 {
		t.Fatalf("expected two compact calls (retry after 400), got %d", len(client.compactionCalls))
	}
	if len(client.compactionCalls[1].Items) != len(client.compactionCalls[0].Items) {
		t.Fatalf("expected repair to preserve item count, first=%d second=%d", len(client.compactionCalls[0].Items), len(client.compactionCalls[1].Items))
	}
	foundCollapsed := false
	for _, item := range client.compactionCalls[1].Items {
		if item.Type == llm.ResponseItemTypeFunctionCallOutput && item.CallID != nil && *item.CallID == "call_1" {
			foundCollapsed = isCollapsedCompactionOverflowShellOutput(item.Output)
		}
	}
	if !foundCollapsed {
		t.Fatalf("expected repaired retry to collapse shell output, got %+v", client.compactionCalls[1].Items)
	}
}
