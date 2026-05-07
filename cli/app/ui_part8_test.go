package app

import (
	"builder/cli/app/commands"
	"builder/cli/tui"
	"builder/server/llm"
	"builder/server/runtime"
	"builder/server/session"
	"builder/server/tools"
	"builder/shared/toolspec"
	"builder/shared/transcript"
	"context"
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"io"
	"strings"
	"testing"
	"time"
)

func TestBusySlashSupervisorOnAppliesToInFlightRunCompletion(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	mainClient := &busyToggleFakeClient{responses: []llm.Response{
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "working", Phase: llm.MessagePhaseCommentary},
			ToolCalls: []llm.ToolCall{{ID: "call_patch_1", Name: string(toolspec.ToolPatch), Custom: true, CustomInput: "*** Begin Patch\n*** Add File: a.txt\n+hello\n*** End Patch"}},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
	}}
	reviewerClient := &busyToggleFakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: `{"suggestions":[]}`},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
	eng, err := runtime.New(store, mainClient, tools.NewRegistry(busyTogglePatchTool{delay: 80 * time.Millisecond}), runtime.Config{
		Model: "gpt-5",
		Reviewer: runtime.ReviewerConfig{
			Frequency:     "off",
			Model:         "gpt-5",
			ThinkingLevel: "low",
			Client:        reviewerClient,
		},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	m := newProjectedEngineUIModel(eng)
	m.busy = true
	m.activity = uiActivityRunning

	submitDone := make(chan error, 1)
	go func() {
		_, submitErr := eng.SubmitUserMessage(context.Background(), "edit file")
		submitDone <- submitErr
	}()
	time.Sleep(10 * time.Millisecond)

	m.input = "/supervisor on"
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if !updated.reviewerEnabled || updated.reviewerMode != "edits" {
		t.Fatalf("expected ui reviewer enabled in edits mode after /supervisor on, got enabled=%v mode=%q", updated.reviewerEnabled, updated.reviewerMode)
	}
	if got := eng.ReviewerFrequency(); got != "edits" {
		t.Fatalf("expected runtime reviewer mode edits, got %q", got)
	}

	if err := <-submitDone; err != nil {
		t.Fatalf("submit user message: %v", err)
	}
	if got := reviewerClient.CallCount(); got != 1 {
		t.Fatalf("expected reviewer call for in-flight run after /supervisor on, got %d", got)
	}
}

func TestSlashSupervisorWithEngineTogglesRuntimeReviewer(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	eng, err := runtime.New(store, statusLineFakeClient{}, tools.NewRegistry(), runtime.Config{
		Model: "gpt-5",
		Reviewer: runtime.ReviewerConfig{
			Frequency:     "off",
			Model:         "gpt-5",
			ThinkingLevel: "low",
			Client:        statusLineFakeClient{},
		},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	m := newProjectedEngineUIModel(eng)
	m.input = "/supervisor on"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected transient status clear timer cmd")
	}
	if got := eng.ReviewerFrequency(); got != "edits" {
		t.Fatalf("expected runtime reviewer mode edits, got %q", got)
	}
	if !updated.reviewerEnabled || updated.reviewerMode != "edits" {
		t.Fatalf("expected ui reviewer enabled in edits mode, got enabled=%v mode=%q", updated.reviewerEnabled, updated.reviewerMode)
	}
	if !strings.Contains(updated.transientStatus, "Supervisor invocation enabled") {
		t.Fatalf("expected enable status message, got %q", updated.transientStatus)
	}

	updated.input = "/supervisor off"
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	if got := eng.ReviewerFrequency(); got != "off" {
		t.Fatalf("expected runtime reviewer mode off, got %q", got)
	}
	if updated.reviewerEnabled || updated.reviewerMode != "off" {
		t.Fatalf("expected ui reviewer disabled in off mode, got enabled=%v mode=%q", updated.reviewerEnabled, updated.reviewerMode)
	}
}

func TestSlashAutoCompactionTogglesAndShowsStatus(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 24
	m.windowSizeKnown = true
	m.syncViewport()
	m.input = "/autocompaction"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected transient status clear timer cmd")
	}
	if updated.autoCompactionEnabled {
		t.Fatal("expected auto-compaction disabled after toggle")
	}
	if updated.input != "" {
		t.Fatalf("expected input cleared after /autocompaction, got %q", updated.input)
	}
	if !strings.Contains(updated.transientStatus, "Auto-compaction disabled") {
		t.Fatalf("expected transient status for /autocompaction toggle, got %q", updated.transientStatus)
	}
	plain := stripANSIAndTrimRight(updated.View())
	if !strings.Contains(plain, "Auto-compaction disabled") {
		t.Fatalf("expected transcript notice for /autocompaction toggle, got %q", plain)
	}
	for _, msg := range collectCmdMessages(t, cmd) {
		next, _ = updated.Update(msg)
		updated = next.(*uiModel)
	}

	updated.input = "/autocompaction on"
	next, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected transient status clear timer cmd")
	}
	if !updated.autoCompactionEnabled {
		t.Fatal("expected auto-compaction enabled")
	}
	if !strings.Contains(updated.transientStatus, "Auto-compaction enabled") {
		t.Fatalf("expected enable transient status, got %q", updated.transientStatus)
	}
}

func TestBusySlashAutoCompactionExecutesImmediatelyWithoutQueueing(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.busy = true
	m.activity = uiActivityRunning
	m.input = "/autocompaction off"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected transient status clear timer cmd")
	}
	if !updated.busy {
		t.Fatal("expected busy state unchanged while command executes")
	}
	if updated.autoCompactionEnabled {
		t.Fatal("expected auto-compaction disabled")
	}
	if len(updated.queued) != 0 {
		t.Fatalf("expected no queued messages, got %d", len(updated.queued))
	}
	if len(updated.pendingInjected) != 0 {
		t.Fatalf("expected no pending injected messages, got %d", len(updated.pendingInjected))
	}
	if updated.input != "" {
		t.Fatalf("expected input cleared after /autocompaction, got %q", updated.input)
	}
}

func TestSlashAutoCompactionWithEngineTogglesRuntime(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	eng, err := runtime.New(store, statusLineFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	m := newProjectedEngineUIModel(eng)
	m.input = "/autocompaction off"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected transient status clear timer cmd")
	}
	if got := eng.AutoCompactionEnabled(); got {
		t.Fatalf("expected runtime auto-compaction disabled, got %v", got)
	}
	if updated.autoCompactionEnabled {
		t.Fatal("expected ui auto-compaction disabled")
	}

	updated.input = "/autocompaction on"
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	if got := eng.AutoCompactionEnabled(); !got {
		t.Fatalf("expected runtime auto-compaction enabled, got %v", got)
	}
	if !updated.autoCompactionEnabled {
		t.Fatal("expected ui auto-compaction enabled")
	}
}

func TestSlashAutoCompactionKeepsPriorStateWhenRuntimeToggleFails(t *testing.T) {
	client := &runtimeControlFakeClient{err: errors.New("daemon stalled")}
	m := newProjectedStaticUIModel()
	m.engine = client
	m.autoCompactionEnabled = true
	m.input = "/autocompaction off"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected transient status clear timer cmd")
	}
	if !updated.autoCompactionEnabled {
		t.Fatal("expected prior auto-compaction state preserved on toggle failure")
	}
	if !strings.Contains(updated.transientStatus, "daemon stalled") {
		t.Fatalf("expected transport error in transient status, got %q", updated.transientStatus)
	}
}

func TestSlashAutoCompactionShowsCompactionModeNoneNote(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	eng, err := runtime.New(store, statusLineFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5", CompactionMode: "none"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	m := newProjectedEngineUIModel(eng)
	m.input = "/autocompaction on"

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if !strings.Contains(updated.transientStatus, "compaction_mode=none") {
		t.Fatalf("expected compaction_mode=none note in status, got %q", updated.transientStatus)
	}
}

func TestBusyUnsupportedSlashCommandShowsTransientErrorAndDoesNotQueue(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.busy = true
	m.activity = uiActivityRunning
	m.input = "/compact keep details"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected transient status clear timer cmd")
	}
	if updated.transientStatus == "" {
		t.Fatal("expected transient status message for unsupported busy command")
	}
	if len(updated.queued) != 0 {
		t.Fatalf("expected no queued messages, got %d", len(updated.queued))
	}
	if len(updated.pendingInjected) != 0 {
		t.Fatalf("expected no pending injected messages, got %d", len(updated.pendingInjected))
	}
	if updated.inputSubmitLocked {
		t.Fatal("did not expect input submit lock for blocked slash command")
	}
	if updated.input != "" {
		t.Fatalf("expected input cleared for blocked slash command, got %q", updated.input)
	}
	status := stripANSIAndTrimRight(updated.renderStatusLine(120, uiThemeStyles("dark")))
	if !strings.Contains(status, "cannot run /compact while model is working") {
		t.Fatalf("expected transient status in status line, got %q", status)
	}

	next, _ = updated.Update(clearTransientStatusMsg{token: updated.transientStatusToken})
	cleared := next.(*uiModel)
	if cleared.transientStatus != "" {
		t.Fatalf("expected transient status to clear, got %q", cleared.transientStatus)
	}
}

func TestSlashCommandSetsExitAction(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.input = "/exit"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected quit cmd for /exit")
	}
	updated := next.(*uiModel)
	if updated.Action() != UIActionExit {
		t.Fatalf("expected UIActionExit, got %q", updated.Action())
	}
}

func TestSlashCommandSetsResumeAction(t *testing.T) {
	m := newProjectedStaticUIModel(WithUIHasOtherSessions(true, true))
	m.input = "/resume"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected quit cmd for /resume")
	}
	updated := next.(*uiModel)
	if updated.Action() != UIActionResume {
		t.Fatalf("expected UIActionResume, got %q", updated.Action())
	}
}

func TestInitialTranscriptVisibleImmediately(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialTranscript([]UITranscriptEntry{
			{Role: "user", Text: "hello"},
			{Role: "assistant", Text: "world"},
		}),
	)
	m.termWidth = 80
	m.termHeight = 20

	ongoing := stripANSIAndTrimRight(m.view.OngoingSnapshot())
	if !strings.Contains(ongoing, "world") {
		t.Fatalf("expected resumed content in ongoing mode, got %q", ongoing)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	detail := stripANSIAndTrimRight(next.(*uiModel).View())
	if !containsInOrder(detail, "❯", "hello", "❮", "world") {
		t.Fatalf("expected resumed transcript in detail mode, got %q", detail)
	}
}

func TestSubmitDoneNoopFinalStaysInvisibleWithoutRuntimeClient(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.busy = true

	next, _ := m.Update(newSubmitDoneMsg(0, uiNoopFinalToken, "", nil))
	updated := next.(*uiModel)
	if updated.busy {
		t.Fatal("expected UI idle after NO_OP final")
	}
	if updated.activity != uiActivityIdle {
		t.Fatalf("activity = %v, want idle", updated.activity)
	}
	plain := stripANSIAndTrimRight(updated.view.OngoingSnapshot())
	if strings.Contains(plain, uiNoopFinalToken) {
		t.Fatalf("expected NO_OP to stay invisible in local flow, got %q", plain)
	}
	if len(updated.transcriptEntries) != 0 {
		t.Fatalf("expected no transcript entries after NO_OP final, got %+v", updated.transcriptEntries)
	}
}

func TestRuntimeSubmitNoopFinalStaysSilent(t *testing.T) {
	ringer := &countRinger{}
	bells := newBellHooks(ringer, nil)
	client := &runtimeControlFakeClient{submitResult: uiNoopFinalToken}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents(), WithUITurnQueueHook(bells))
	m.startupCmds = nil
	m.input = "hello"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected submit command batch")
	}
	msgs := collectCmdMessages(t, cmd)
	var done submitDoneMsg
	doneFound := false
	for _, msg := range msgs {
		if typed, ok := msg.(submitDoneMsg); ok {
			done = typed
			doneFound = true
		}
	}
	if !doneFound {
		t.Fatalf("expected submitDone message, got %+v", msgs)
	}
	if !done.silentFinal {
		t.Fatalf("expected runtime NO_OP submit result to be marked silent, got %+v", done)
	}

	next, _ = updated.Update(done)
	updated = next.(*uiModel)
	if updated.busy {
		t.Fatal("expected UI idle after runtime NO_OP final")
	}
	if updated.activity != uiActivityIdle {
		t.Fatalf("activity = %v, want idle", updated.activity)
	}
	if client.submitText != "hello" {
		t.Fatalf("submit text = %q, want hello", client.submitText)
	}
	if got := ringer.Count(); got != 0 {
		t.Fatalf("ring count = %d after runtime NO_OP final, want 0", got)
	}
	plain := stripANSIAndTrimRight(updated.view.OngoingSnapshot())
	if strings.Contains(plain, uiNoopFinalToken) {
		t.Fatalf("expected runtime NO_OP to stay invisible, got %q", plain)
	}
	if len(updated.transcriptEntries) != 0 {
		t.Fatalf("expected no transcript entries after runtime NO_OP final, got %+v", updated.transcriptEntries)
	}
}

func TestInitAutoSubmitsStartupPrompt(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIStartupSubmit("run review"),
	)
	m.termWidth = 80
	m.termHeight = 20

	_ = m.Init()

	if !m.busy {
		t.Fatal("expected startup prompt to start submission immediately")
	}
	plain := stripANSIAndTrimRight(m.view.OngoingSnapshot())
	if !strings.Contains(plain, "run review") {
		t.Fatalf("expected startup prompt in transcript, got %q", plain)
	}
}

func TestInitialInputSeedsDraftWithoutAutoSubmit(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIInitialInput("draft reply"),
	)

	if m.input != "draft reply" {
		t.Fatalf("expected initial input draft, got %q", m.input)
	}
	if m.inputCursor != -1 {
		t.Fatalf("expected initial input cursor at tail, got %d", m.inputCursor)
	}
	if m.busy {
		t.Fatal("did not expect initial input to auto-submit")
	}
}

func TestReviewerStatusEndToEnd_VerboseSuggestionsIssuedAndStatusConcise(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	eng, err := runtime.New(store, statusLineFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	eng.AppendLocalEntryWithOngoingText("reviewer_suggestions", "Supervisor suggested:\n1. First detailed suggestion text\n2. Second detailed suggestion text", "Supervisor suggested:\n1. First detailed suggestion text\n2. Second detailed suggestion text")
	eng.AppendLocalEntry("reviewer_status", "Supervisor ran: 2 suggestions, no changes applied.")

	m := newProjectedEngineUIModel(eng, WithUITheme("dark"))
	m.termWidth = 100
	m.termHeight = 24

	rawOngoing := m.view.OngoingSnapshot()
	ongoing := stripANSIAndTrimRight(rawOngoing)
	if !containsInOrder(ongoing, "Supervisor suggested:", "1. First detailed suggestion text", "2. Second detailed suggestion text") {
		t.Fatalf("expected verbose reviewer suggestions in ongoing mode, got %q", ongoing)
	}
	if !strings.Contains(ongoing, "Supervisor ran: 2 suggestions, no changes applied.") {
		t.Fatalf("expected short reviewer status in ongoing mode, got %q", ongoing)
	}
	if strings.Count(ongoing, "Supervisor suggested:") != 1 {
		t.Fatalf("expected reviewer suggestions details only at issuance time in ongoing mode, got %q", ongoing)
	}
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("#98C379"))
	if !strings.Contains(rawOngoing, green.Render("Supervisor suggested:")) {
		t.Fatalf("expected reviewer suggestions to use success styling in ongoing mode, got %q", rawOngoing)
	}
	if !strings.Contains(rawOngoing, green.Render("Supervisor ran: 2 suggestions, no changes applied.")) {
		t.Fatalf("expected reviewer status to use success styling in ongoing mode, got %q", rawOngoing)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	rawDetail := next.(*uiModel).View()
	detail := stripANSIAndTrimRight(rawDetail)
	if !containsInOrder(detail, "Supervisor suggested:", "1. First detailed suggestion text", "2. Second detailed suggestion text", "Supervisor ran: 2 suggestions, no changes applied.") {
		t.Fatalf("expected full reviewer suggestions in detail mode, got %q", detail)
	}
	if !strings.Contains(rawDetail, green.Render("Supervisor suggested:")) {
		t.Fatalf("expected reviewer suggestions to use success styling in detail mode, got %q", rawDetail)
	}
	if !strings.Contains(rawDetail, green.Render("Supervisor ran: 2 suggestions, no changes applied.")) {
		t.Fatalf("expected reviewer status to use success styling in detail mode, got %q", rawDetail)
	}
}

func TestDisconnectedEnterKeepsInputAndDoesNotStartSubmission(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, nil, nil)
	m.setRuntimeDisconnected(true)
	m.input = "continue with tests"

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if updated.busy {
		t.Fatal("did not expect busy state while disconnected")
	}
	if updated.input != "continue with tests" {
		t.Fatalf("expected input preserved while disconnected, got %q", updated.input)
	}
	if client.submitText != "" {
		t.Fatalf("did not expect runtime submit attempt, got %q", client.submitText)
	}
	if updated.activity != uiActivityError {
		t.Fatalf("expected error activity while disconnected, got %v", updated.activity)
	}
}

func TestDisconnectedEnterAppendsOperatorFeedbackWhenRuntimeAppendFails(t *testing.T) {
	client := &runtimeControlFakeClient{appendErr: errors.New("append failed")}
	m := newProjectedTestUIModel(client, nil, nil)
	m.setRuntimeDisconnected(true)
	m.input = "continue with tests"

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)

	if len(updated.transcriptEntries) != 1 {
		t.Fatalf("expected one fallback transcript entry, got %+v", updated.transcriptEntries)
	}
	entry := updated.transcriptEntries[0]
	if entry.Role != tui.TranscriptRoleDeveloperErrorFeedback || entry.Text != runtimeDisconnectedStatusMessage {
		t.Fatalf("unexpected fallback transcript entry: %+v", entry)
	}
	if client.submitText != "" {
		t.Fatalf("did not expect runtime submit attempt, got %q", client.submitText)
	}
}

func TestBlockDisconnectedSubmissionBlocksEvenWhenRuntimeAppendSucceeds(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, nil, nil)
	m.setRuntimeDisconnected(true)
	m.pendingInjected = queuedUserMessagesForTest("hidden steering")

	blocked, cmd := m.inputController().blockDisconnectedSubmission(true, "generated prompt")

	if !blocked {
		t.Fatal("expected disconnected submission to block")
	}
	if cmd != nil {
		t.Fatal("did not expect fallback transcript cmd when runtime append succeeds")
	}
	if m.activity != uiActivityError {
		t.Fatalf("expected error activity while disconnected, got %v", m.activity)
	}
	if m.input != "hidden steering\n\ngenerated prompt" {
		t.Fatalf("expected hidden drafts restored into input, got %q", m.input)
	}
	if client.appendedRole != string(transcript.EntryRoleDeveloperErrorFeedback) || client.appendedText != runtimeDisconnectedStatusMessage {
		t.Fatalf("unexpected runtime local entry attempt: role=%q text=%q", client.appendedRole, client.appendedText)
	}
}

func TestDisconnectedQueuedFlushRestoresHiddenQueuedDrafts(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, nil, nil)
	m.setRuntimeDisconnected(true)
	m.queued = queuedInputsForTest("first queued", "second queued")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)

	if cmd != nil {
		t.Fatal("did not expect queued flush command while disconnected")
	}
	if updated.input != "first queued\n\nsecond queued" {
		t.Fatalf("expected hidden queued drafts restored into input, got %q", updated.input)
	}
	if len(updated.queued) != 0 {
		t.Fatalf("expected queued drafts restored and cleared, got %+v", updated.queued)
	}
}

func TestDisconnectedQueuedFlushWithoutQueuedWorkDoesNotAppendFeedback(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, nil, nil)
	m.setRuntimeDisconnected(true)

	next, cmd := m.inputController().flushQueuedInputs(queueDrainAuto)
	updated := next.(*uiModel)

	if cmd != nil {
		t.Fatal("did not expect command for no-op queued flush")
	}
	if updated.activity != uiActivityIdle {
		t.Fatalf("activity = %v, want idle", updated.activity)
	}
	if client.appendedRole != "" || client.appendedText != "" {
		t.Fatalf("did not expect disconnect feedback append, got role=%q text=%q", client.appendedRole, client.appendedText)
	}
}

func TestDisconnectedQueuedInjectionSubmissionRestoresHiddenInjectedDrafts(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, nil, nil)
	m.setRuntimeDisconnected(true)
	m.pendingInjected = queuedUserMessagesForTest("hidden steering")

	cmd := m.inputController().startQueuedInjectionSubmission()
	if cmd != nil {
		t.Fatal("did not expect queued injection submission command while disconnected")
	}
	if m.input != "hidden steering" {
		t.Fatalf("expected hidden injected draft restored into input, got %q", m.input)
	}
	if len(m.pendingInjected) != 0 {
		t.Fatalf("expected pending injected drafts restored and cleared, got %+v", m.pendingInjected)
	}
}

func TestDisconnectedCommandSubmitRestoresGeneratedPrompt(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, nil, nil)
	m.setRuntimeDisconnected(true)

	next, _ := m.inputController().applyCommandResult(commands.Result{Handled: true, SubmitUser: true, User: "generated prompt"})
	updated := next.(*uiModel)
	if updated.input != "generated prompt" {
		t.Fatalf("expected generated prompt restored into input, got %q", updated.input)
	}
	if updated.busy {
		t.Fatal("did not expect busy state while disconnected")
	}
}

func TestDisconnectedCommandSubmitRestoresGeneratedPromptAlongsideHiddenSteering(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, nil, nil)
	m.setRuntimeDisconnected(true)
	m.pendingInjected = queuedUserMessagesForTest("hidden steering")

	next, _ := m.inputController().applyCommandResult(commands.Result{Handled: true, SubmitUser: true, User: "generated prompt"})
	updated := next.(*uiModel)
	if updated.input != "hidden steering\n\ngenerated prompt" {
		t.Fatalf("expected generated prompt restored after hidden steering, got %q", updated.input)
	}
	if len(updated.pendingInjected) != 0 {
		t.Fatalf("expected pending injected drafts restored and cleared, got %+v", updated.pendingInjected)
	}
}

func TestApplyCommandResultBackWithoutParentReturnsVisibleSystemFeedbackCmd(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 120

	next, cmd := m.inputController().applyCommandResult(commands.Result{Handled: true, Action: commands.ActionBack})
	updated := next.(*uiModel)

	if len(updated.transcriptEntries) != 1 {
		t.Fatalf("expected one transcript entry, got %+v", updated.transcriptEntries)
	}
	entry := updated.transcriptEntries[0]
	if entry.Role != "system" || entry.Text != "No parent session available" {
		t.Fatalf("unexpected transcript entry: %+v", entry)
	}
	if cmd == nil {
		t.Fatal("expected native history sync command")
	}
	flush, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", cmd())
	}
	if !strings.Contains(stripANSIAndTrimRight(flush.Text), "No parent session available") {
		t.Fatalf("expected native history flush to include system feedback, got %q", flush.Text)
	}
}

func TestEnqueueRuntimeConnectionStateChangeDropsStaleWithoutBlocking(t *testing.T) {
	ch := make(chan runtimeConnectionStateChangedMsg, 1)
	enqueueRuntimeConnectionStateChange(ch, errors.New("stale"))

	done := make(chan struct{})
	go func() {
		enqueueRuntimeConnectionStateChange(ch, io.EOF)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for non-blocking connection-state enqueue")
	}

	select {
	case msg := <-ch:
		if !errors.Is(msg.err, io.EOF) {
			t.Fatalf("expected latest connection-state error preserved, got %v", msg.err)
		}
	default:
		t.Fatal("expected queued connection-state message")
	}
}
