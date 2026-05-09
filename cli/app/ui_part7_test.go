package app

import (
	"builder/cli/app/commands"
	"builder/cli/tui"
	"builder/server/llm"
	"builder/server/runtime"
	"builder/server/session"
	"builder/server/tools"
	shelltool "builder/server/tools/shell"
	"builder/shared/clientui"
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
	"time"
)

func TestHydrationCompletionKeepsDeferredQueuedDrainArmedUntilUnrelatedBusyStateClears(t *testing.T) {
	client := &refreshingRuntimeClient{
		transcripts: []clientui.TranscriptPage{{
			SessionID:    "session-1",
			TotalEntries: 1,
			Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "final answer"}},
		}},
	}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.startupCmds = nil
	m.busy = true
	m.activity = uiActivityRunning
	m.queued = queuedInputsForTest("follow up")
	m.runtimeTranscriptBusy = true
	m.runtimeTranscriptToken = 7

	next, _ := m.Update(submitDoneMsg{message: "ignored by runtime-backed flow"})
	updated := next.(*uiModel)
	if !updated.pendingQueuedDrainAfterHydration {
		t.Fatal("expected queued drain deferred until hydration completes")
	}

	next, _ = updated.Update(runtimeEventMsg{event: clientui.Event{Kind: clientui.EventRunStateChanged, RunState: &clientui.RunState{Busy: true}}})
	updated = next.(*uiModel)
	if !updated.busy {
		t.Fatal("expected unrelated runtime activity to mark UI busy before hydration completes")
	}

	next, refreshCmd := updated.Update(runtimeTranscriptRefreshedMsg{token: 7, transcript: client.transcripts[0]})
	updated = next.(*uiModel)
	if !updated.pendingQueuedDrainAfterHydration {
		t.Fatal("expected queued drain to remain armed while dirty follow-up hydration is still required")
	}
	if updated.queuedDrainReadyAfterHydration {
		t.Fatal("did not expect queued drain marked ready before the final hydration completes")
	}
	if refreshCmd == nil {
		t.Fatal("expected dirty follow-up hydration after in-flight refresh completes")
	}
	followRefresh, ok := refreshCmd().(runtimeTranscriptRefreshedMsg)
	if !ok {
		t.Fatalf("expected follow-up hydration message, got %T", refreshCmd())
	}

	next, _ = updated.Update(followRefresh)
	updated = next.(*uiModel)
	if !updated.pendingQueuedDrainAfterHydration {
		t.Fatal("expected queued drain to remain armed when hydration completes during unrelated busy state")
	}
	if updated.queuedDrainReadyAfterHydration == false {
		t.Fatal("expected hydration completion to mark queued drain ready even when unrelated busy state blocks auto-drain")
	}
	if len(updated.queued) != 1 || updated.queued[0].Text != "follow up" {
		t.Fatalf("expected queued follow-up preserved while unrelated busy state blocks auto-drain, got %+v", updated.queued)
	}

	next, idleCmd := updated.Update(runtimeEventMsg{event: clientui.Event{Kind: clientui.EventRunStateChanged, RunState: &clientui.RunState{Busy: false}}})
	updated = next.(*uiModel)
	if !updated.busy {
		t.Fatal("expected queued follow-up to start once unrelated busy state clears")
	}
	if updated.pendingQueuedDrainAfterHydration {
		t.Fatal("expected deferred queued drain cleared once unrelated busy state clears and auto-drain starts")
	}
	if updated.activeSubmit.text != "follow up" {
		t.Fatalf("expected queued follow-up to submit after unrelated busy state clears, got %q", updated.activeSubmit.text)
	}
	if len(updated.queued) != 0 {
		t.Fatalf("expected active queued follow-up removed from visible queue after submit starts, got %+v", updated.queued)
	}
	if idleCmd == nil {
		t.Fatal("expected queued follow-up command sequence after unrelated busy state clears")
	}
	_ = collectCmdMessages(t, idleCmd)
	if client.submitText != "follow up" {
		t.Fatalf("expected queued follow-up submit after unrelated busy state clears, got %q", client.submitText)
	}
}

func TestBusyQueuedUnknownSlashDrainsAsPromptSubmission(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.busy = true
	m.activity = uiActivityRunning
	m.input = "/nope queued"

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated := next.(*uiModel)
	if len(updated.queued) != 1 || updated.queued[0].Text != "/nope queued" {
		t.Fatalf("expected unknown slash text queued verbatim, got %+v", updated.queued)
	}
	if updated.sessionName != "" {
		t.Fatalf("did not expect unknown slash queue-submit to execute as command, got session name %q", updated.sessionName)
	}

	next, cmd := updated.Update(submitDoneMsg{})
	updated = next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected queued unknown slash to start submission after turn completion")
	}
	if !updated.busy {
		t.Fatal("expected queued unknown slash to start prompt submission during drain")
	}
	if len(updated.queued) != 0 {
		t.Fatalf("expected queued unknown slash drained, got %+v", updated.queued)
	}
	if updated.sessionName != "" {
		t.Fatalf("did not expect unknown slash drain to route through slash command execution, got session name %q", updated.sessionName)
	}
	plain := stripANSIAndTrimRight(updated.view.OngoingSnapshot())
	if !strings.Contains(plain, "/nope queued") {
		t.Fatalf("expected queued unknown slash in transcript as prompt text, got %q", plain)
	}
}

func TestAutoDrainStopsAfterQueuedPSInlineAppendsToInput(t *testing.T) {
	manager := newFastBackgroundTestManager(t)

	workdir := t.TempDir()
	res, err := manager.Start(context.Background(), shelltool.ExecRequest{
		Command:        []string{"sh", "-c", "printf 'queued-inline\n'; sleep 1"},
		DisplayCommand: "queued-inline",
		Workdir:        workdir,
		YieldTime:      fastBackgroundTestYield,
	})
	if err != nil {
		t.Fatalf("start queued-inline: %v", err)
	}
	if !res.Backgrounded {
		t.Fatal("expected background process")
	}

	m := newProjectedStaticUIModel(WithUIBackgroundManager(manager))
	m.busy = true
	m.activity = uiActivityRunning
	m.queued = queuedInputsForTest("/ps inline "+res.SessionID, "summarize this")

	next, cmd := m.Update(submitDoneMsg{})
	updated := next.(*uiModel)

	if cmd == nil {
		t.Fatal("expected command batch from queued /ps inline execution")
	}
	if updated.busy {
		t.Fatal("did not expect queued /ps inline to auto-submit the follow-up prompt")
	}
	if !strings.Contains(updated.input, "Output of bg shell "+res.SessionID+":") {
		t.Fatalf("expected inline shell transcript pasted into input, got %q", updated.input)
	}
	if !strings.Contains(updated.input, "queued-inline") {
		t.Fatalf("expected pasted shell transcript content in input, got %q", updated.input)
	}
	if len(updated.queued) != 1 || updated.queued[0].Text != "summarize this" {
		t.Fatalf("expected follow-up prompt to remain queued after inline paste, got %+v", updated.queued)
	}
	plain := stripANSIAndTrimRight(updated.view.OngoingSnapshot())
	if strings.Contains(plain, "summarize this") {
		t.Fatalf("did not expect follow-up prompt submitted without pasted transcript, got %q", plain)
	}
}

func TestBusyQueuedReviewSlashCommandStartsFreshSessionAfterTurn(t *testing.T) {
	m := newProjectedStaticUIModel(
		WithUIConversationFreshness(session.ConversationFreshnessEstablished),
	)
	m.busy = true
	m.activity = uiActivityRunning
	m.input = "/review cli/app"

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated := next.(*uiModel)
	if len(updated.queued) != 1 || updated.queued[0].Text != "/review cli/app" {
		t.Fatalf("expected queued /review command, got %+v", updated.queued)
	}

	next, cmd := updated.Update(submitDoneMsg{message: "done"})
	updated = next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected quit cmd for queued /review handoff")
	}
	if updated.Action() != UIActionNewSession {
		t.Fatalf("expected UIActionNewSession, got %q", updated.Action())
	}
	if strings.TrimSpace(updated.nextSessionInitialPrompt) == "" {
		t.Fatal("expected queued /review to populate the next-session prompt")
	}
	if !strings.Contains(updated.nextSessionInitialPrompt, "cli/app") {
		t.Fatalf("expected queued /review args in handoff payload, got %q", updated.nextSessionInitialPrompt)
	}
	if len(updated.queued) != 0 {
		t.Fatalf("expected queued /review drained, got %+v", updated.queued)
	}
}

func TestBusyQueuedReviewSlashCommandWaitsForHydrationBeforePromptSubmission(t *testing.T) {
	client := &refreshingRuntimeClient{
		transcripts: []clientui.TranscriptPage{{
			SessionID:    "session-1",
			TotalEntries: 1,
			Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "final answer"}},
		}},
	}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.startupCmds = nil
	m.busy = true
	m.activity = uiActivityRunning
	m.input = "/review cli/app"

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated := next.(*uiModel)
	if len(updated.queued) != 1 || updated.queued[0].Text != "/review cli/app" {
		t.Fatalf("expected queued /review command, got %+v", updated.queued)
	}

	next, cmd := updated.Update(submitDoneMsg{message: "done"})
	updated = next.(*uiModel)
	if !updated.pendingQueuedDrainAfterHydration {
		t.Fatal("expected queued /review drain deferred until transcript hydration completes")
	}
	if updated.busy {
		t.Fatal("did not expect queued /review prompt submission before hydration settles")
	}
	if updated.activeSubmit.text != "" {
		t.Fatalf("did not expect queued /review to submit before hydration, got %q", updated.activeSubmit.text)
	}
	if client.submitText != "" {
		t.Fatalf("did not expect queued /review submit before hydration, got %q", client.submitText)
	}
	msgs := collectCmdMessages(t, cmd)
	var refresh runtimeTranscriptRefreshedMsg
	refreshFound := false
	for _, msg := range msgs {
		if typed, ok := msg.(runtimeTranscriptRefreshedMsg); ok {
			refresh = typed
			refreshFound = true
		}
	}
	if !refreshFound {
		t.Fatalf("expected runtime transcript refresh before queued /review drains, got %+v", msgs)
	}
	if refresh.syncCause != runtimeTranscriptSyncCauseQueuedDrain {
		t.Fatalf("queued /review sync cause = %q, want %q", refresh.syncCause, runtimeTranscriptSyncCauseQueuedDrain)
	}

	next, drainCmd := updated.Update(refresh)
	updated = next.(*uiModel)
	if !updated.busy {
		t.Fatal("expected queued /review to start prompt submission after hydration completes")
	}
	if updated.pendingQueuedDrainAfterHydration {
		t.Fatal("expected queued /review hydration deferral cleared once drained")
	}
	if updated.activeSubmit.text == "" {
		t.Fatal("expected queued /review generated prompt to submit")
	}
	if strings.Contains(updated.activeSubmit.text, "/review cli/app") {
		t.Fatalf("expected queued /review to submit generated prompt content, got %q", updated.activeSubmit.text)
	}
	if got := stripANSIAndTrimRight(updated.view.OngoingSnapshot()); !strings.Contains(got, "final answer") {
		t.Fatalf("expected hydration to commit final answer before queued /review drains, got %q", got)
	}
	if drainCmd == nil {
		t.Fatal("expected queued /review submit command after hydration")
	}
	_ = collectCmdMessages(t, drainCmd)
	if client.submitText == "" {
		t.Fatal("expected queued /review submit after hydration")
	}
}

func TestQueuedReviewUsesEngineConversationFreshnessWhenUIDidNotReceiveRuntimeUpdateYet(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	eng, err := runtime.New(store, &runtimeAdapterFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	m := newProjectedEngineUIModel(eng)
	m.busy = true
	m.activity = uiActivityRunning
	m.input = "/review cli/app"

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated := next.(*uiModel)
	if len(updated.queued) != 1 || updated.queued[0].Text != "/review cli/app" {
		t.Fatalf("expected queued /review command, got %+v", updated.queued)
	}
	if updated.conversationFreshness != clientui.ConversationFreshnessFresh {
		t.Fatalf("expected UI freshness to remain fresh before runtime sync, got %v", updated.conversationFreshness)
	}
	if _, err := store.AppendEvent("s1", "message", llm.Message{Role: llm.RoleUser, Content: "first prompt"}); err != nil {
		t.Fatalf("append user message: %v", err)
	}
	if got := eng.ConversationFreshness(); got != session.ConversationFreshnessEstablished {
		t.Fatalf("expected engine freshness established after first prompt, got %v", got)
	}

	next, cmd := updated.Update(submitDoneMsg{message: "done"})
	updated = next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected hydration command for queued /review handoff")
	}
	if !updated.pendingQueuedDrainAfterHydration {
		t.Fatal("expected queued /review handoff deferred until hydration completes")
	}
	msgs := collectCmdMessages(t, cmd)
	var refresh runtimeTranscriptRefreshedMsg
	refreshFound := false
	for _, msg := range msgs {
		if typed, ok := msg.(runtimeTranscriptRefreshedMsg); ok {
			refresh = typed
			refreshFound = true
		}
	}
	if !refreshFound {
		t.Fatalf("expected transcript refresh before queued /review handoff, got %+v", msgs)
	}
	if refresh.syncCause != runtimeTranscriptSyncCauseQueuedDrain {
		t.Fatalf("queued /review handoff sync cause = %q, want %q", refresh.syncCause, runtimeTranscriptSyncCauseQueuedDrain)
	}

	next, followCmd := updated.Update(refresh)
	updated = next.(*uiModel)
	if followCmd == nil {
		t.Fatal("expected queued /review drain after hydration")
	}
	if updated.Action() != UIActionNewSession {
		t.Fatalf("expected UIActionNewSession, got %q", updated.Action())
	}
	if strings.TrimSpace(updated.nextSessionInitialPrompt) == "" {
		t.Fatal("expected queued /review to populate the next-session prompt")
	}
	if updated.conversationFreshness != clientui.ConversationFreshnessEstablished {
		t.Fatalf("expected UI freshness synced from engine during drain, got %v", updated.conversationFreshness)
	}
}

func TestBackSlashCommandCopiesLatestAssistantOutputWhenAvailable(t *testing.T) {
	tests := []struct {
		name       string
		activity   uiActivity
		transcript []llm.Message
		localEntry string
		ongoing    string
		want       string
	}{
		{name: "idle committed final assistant reply", activity: uiActivityIdle, transcript: []llm.Message{{Role: llm.RoleAssistant, Content: "test", Phase: llm.MessagePhaseFinal}}, want: "test"},
		{name: "interrupted streaming reply ignored", activity: uiActivityInterrupted, ongoing: "review findings", want: ""},
		{name: "last committed entry must be assistant final", activity: uiActivityIdle, transcript: []llm.Message{{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal}, {Role: llm.RoleTool, Name: "shell", ToolCallID: "call-1", Content: `{"ok":true}`}}, want: ""},
		{name: "commentary assistant reply ignored", activity: uiActivityIdle, transcript: []llm.Message{{Role: llm.RoleAssistant, Content: "commentary", Phase: llm.MessagePhaseCommentary}}, want: ""},
		{name: "local entry after final assistant does not block copy", activity: uiActivityIdle, transcript: []llm.Message{{Role: llm.RoleAssistant, Content: "test", Phase: llm.MessagePhaseFinal}}, localEntry: "Supervisor ran: ok", want: "test"},
		{name: "no assistant response", activity: uiActivityIdle, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := session.Create(dir, "ws", dir)
			if err != nil {
				t.Fatalf("create store: %v", err)
			}
			if err := store.SetParentSessionID("parent-1"); err != nil {
				t.Fatalf("set parent session id: %v", err)
			}
			for idx, message := range tt.transcript {
				if _, err := store.AppendEvent("step-1", "message", message); err != nil {
					t.Fatalf("append transcript message %d: %v", idx, err)
				}
			}
			eng, err := runtime.New(store, &runtimeAdapterFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
			if err != nil {
				t.Fatalf("new engine: %v", err)
			}
			if tt.localEntry != "" {
				eng.AppendLocalEntry("reviewer_status", tt.localEntry)
			}

			m := newProjectedEngineUIModel(eng)
			m.activity = tt.activity
			m.forwardToView(tui.SetConversationMsg{Ongoing: tt.ongoing})
			m.input = "/back"

			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			updated := next.(*uiModel)
			if cmd == nil {
				t.Fatal("expected quit cmd for /back teleport")
			}
			if updated.Action() != UIActionOpenSession {
				t.Fatalf("expected UIActionOpenSession, got %q", updated.Action())
			}
			if updated.nextSessionID != "parent-1" {
				t.Fatalf("expected parent target session, got %q", updated.nextSessionID)
			}
			if updated.nextSessionInitialInput != tt.want {
				t.Fatalf("expected copied input %q, got %q", tt.want, updated.nextSessionInitialInput)
			}
		})
	}
}

func TestBackSlashCommandIgnoresRestoredPromptHistoryDraftInChildSession(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.SetParentSessionID("parent-1"); err != nil {
		t.Fatalf("set parent session id: %v", err)
	}
	if _, err := store.AppendEvent("step-1", "message", llm.Message{Role: llm.RoleAssistant, Content: "latest reply", Phase: llm.MessagePhaseFinal}); err != nil {
		t.Fatalf("append assistant reply: %v", err)
	}
	eng, err := runtime.New(store, &runtimeAdapterFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	m := newProjectedEngineUIModel(eng)
	m.promptHistory = []string{"/back"}
	m.promptHistoryDraft = "parked child draft"
	m.promptHistoryDraftCursor = -1
	m.promptHistorySelection = 0
	m.input = "/back"
	m.inputCursor = -1

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected quit cmd for /back teleport")
	}
	if updated.Action() != UIActionOpenSession {
		t.Fatalf("expected UIActionOpenSession, got %q", updated.Action())
	}
	if updated.input != "parked child draft" {
		t.Fatalf("expected parked child draft restored locally, got %q", updated.input)
	}
	if updated.nextSessionInitialInput != "latest reply" {
		t.Fatalf("expected /back to still copy assistant reply, got %q", updated.nextSessionInitialInput)
	}
}

func TestUnknownSlashCommandIsSubmittedAsPrompt(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.input = "/nope"

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if !updated.busy {
		t.Fatal("expected submission to start for unknown slash command")
	}
	plain := stripANSIAndTrimRight(updated.view.OngoingSnapshot())
	if !strings.Contains(plain, "/nope") {
		t.Fatalf("expected unknown slash command in user transcript, got %q", plain)
	}
}

func TestFileSlashCommandSubmitsInjectedUserPrompt(t *testing.T) {
	r := commands.NewRegistry()
	r.Register("prompt:review", "", func(string) commands.Result {
		return commands.Result{Handled: true, SubmitUser: true, User: "# review\nexact body\n"}
	})
	m := newProjectedStaticUIModel(
		WithUICommandRegistry(r),
	)
	m.input = "/prompt:review"

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if !updated.busy {
		t.Fatal("expected submission to start for file slash command")
	}
	plain := stripANSIAndTrimRight(updated.view.OngoingSnapshot())
	if strings.Contains(plain, "/prompt:review") {
		t.Fatalf("expected command text to be replaced by file prompt content, got %q", plain)
	}
	if !strings.Contains(plain, "review") || !strings.Contains(plain, "exact body") {
		t.Fatalf("expected file prompt content in transcript, got %q", plain)
	}
}

func TestBuiltInReviewSlashCommandSubmitsInjectedUserPrompt(t *testing.T) {
	r := commands.NewDefaultRegistry()
	m := newProjectedStaticUIModel(
		WithUICommandRegistry(r),
	)
	m.input = "/review cli/app"
	if got := r.Execute("/review cli/app"); !got.Handled || !got.SubmitUser {
		t.Fatalf("expected /review command to submit injected user prompt, got %+v", got)
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected submission cmd for /review")
	}
	if updated.Action() != UIActionNone {
		t.Fatalf("expected no session transition for empty-session /review, got %q", updated.Action())
	}
	if !updated.busy {
		t.Fatal("expected /review to submit in place for an empty session")
	}
	if updated.nextSessionInitialPrompt != "" {
		t.Fatalf("expected no handoff payload for empty-session /review, got %q", updated.nextSessionInitialPrompt)
	}
}

func TestBuiltInInitSlashCommandSubmitsInjectedUserPrompt(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.input = "/init starter repo"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected submission cmd for /init")
	}
	if updated.Action() != UIActionNone {
		t.Fatalf("expected no session transition for empty-session /init, got %q", updated.Action())
	}
	if !updated.busy {
		t.Fatal("expected /init to submit in place for an empty session")
	}
	if updated.nextSessionInitialPrompt != "" {
		t.Fatalf("expected no handoff payload for empty-session /init, got %q", updated.nextSessionInitialPrompt)
	}
}

func TestBuiltInReviewSlashCommandStartsFreshSessionWhenCurrentSessionHasVisibleUserPrompt(t *testing.T) {
	r := commands.NewDefaultRegistry()
	m := newProjectedStaticUIModel(
		WithUICommandRegistry(r),
		WithUIConversationFreshness(session.ConversationFreshnessEstablished),
	)
	m.input = "/review cli/app"
	expected := r.Execute("/review cli/app")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected quit cmd for non-empty-session /review handoff")
	}
	if updated.Action() != UIActionNewSession {
		t.Fatalf("expected UIActionNewSession, got %q", updated.Action())
	}
	if updated.nextSessionInitialPrompt != expected.User {
		t.Fatalf("expected handoff payload to match /review command output\nwant: %q\n got: %q", expected.User, updated.nextSessionInitialPrompt)
	}
}

func TestBuiltInInitSlashCommandStartsFreshSessionWhenCurrentSessionHasVisibleUserPrompt(t *testing.T) {
	r := commands.NewDefaultRegistry()
	m := newProjectedStaticUIModel(
		WithUICommandRegistry(r),
		WithUIConversationFreshness(session.ConversationFreshnessEstablished),
	)
	m.input = "/init starter repo"
	expected := r.Execute("/init starter repo")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected quit cmd for non-empty-session /init handoff")
	}
	if updated.Action() != UIActionNewSession {
		t.Fatalf("expected UIActionNewSession, got %q", updated.Action())
	}
	if updated.nextSessionInitialPrompt != expected.User {
		t.Fatalf("expected handoff payload to match /init command output\nwant: %q\n got: %q", expected.User, updated.nextSessionInitialPrompt)
	}
}

func TestBusySlashNameExecutesImmediatelyWithoutQueueing(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.busy = true
	m.activity = uiActivityRunning
	m.input = "/name incident triage"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected window title update cmd from /name")
	}
	if !updated.busy {
		t.Fatal("expected busy state unchanged while command executes")
	}
	if updated.sessionName != "incident triage" {
		t.Fatalf("expected session name update, got %q", updated.sessionName)
	}
	if len(updated.queued) != 0 {
		t.Fatalf("expected no queued messages, got %d", len(updated.queued))
	}
	if len(updated.pendingInjected) != 0 {
		t.Fatalf("expected no pending injected messages, got %d", len(updated.pendingInjected))
	}
	if updated.input != "" {
		t.Fatalf("expected input cleared after /name, got %q", updated.input)
	}
}

func TestBusySlashThinkingExecutesImmediatelyWithoutQueueing(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.busy = true
	m.activity = uiActivityRunning
	m.thinkingLevel = "high"
	m.input = "/thinking low"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd != nil {
		t.Fatal("did not expect extra command from /thinking")
	}
	if !updated.busy {
		t.Fatal("expected busy state unchanged while command executes")
	}
	if updated.thinkingLevel != "low" {
		t.Fatalf("expected thinking level update, got %q", updated.thinkingLevel)
	}
	if len(updated.queued) != 0 {
		t.Fatalf("expected no queued messages, got %d", len(updated.queued))
	}
	if len(updated.pendingInjected) != 0 {
		t.Fatalf("expected no pending injected messages, got %d", len(updated.pendingInjected))
	}
	if updated.input != "" {
		t.Fatalf("expected input cleared after /thinking, got %q", updated.input)
	}
}

func TestSlashFastTogglesAndShowsStatus(t *testing.T) {
	m := newProjectedStaticUIModel(WithUIFastModeAvailable(true))
	m.termWidth = 100
	m.termHeight = 24
	m.windowSizeKnown = true
	m.syncViewport()
	m.input = "/fast"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected transient status clear timer cmd")
	}
	if !updated.fastModeEnabled {
		t.Fatal("expected fast mode enabled after toggle")
	}
	if !strings.Contains(updated.transientStatus, "Fast mode enabled") {
		t.Fatalf("expected transient status for /fast toggle, got %q", updated.transientStatus)
	}
	plain := stripANSIAndTrimRight(updated.View())
	if !strings.Contains(plain, "Fast mode enabled") {
		t.Fatalf("expected transcript notice for /fast toggle, got %q", plain)
	}
	for _, msg := range collectCmdMessages(t, cmd) {
		next, _ = updated.Update(msg)
		updated = next.(*uiModel)
	}

	updated.input = "/fast off"
	next, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected transient status clear timer cmd")
	}
	if updated.fastModeEnabled {
		t.Fatal("expected fast mode disabled")
	}
	if !strings.Contains(updated.transientStatus, "Fast mode disabled") {
		t.Fatalf("expected disable transient status, got %q", updated.transientStatus)
	}

	updated.input = "/fast status"
	next, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected transcript sync cmd for /fast status")
	}
	flush, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg for /fast status, got %T", cmd())
	}
	if !strings.Contains(stripANSIAndTrimRight(flush.Text), "Fast mode is off") {
		t.Fatalf("expected /fast status flush to include feedback, got %q", flush.Text)
	}
	plain = stripANSIAndTrimRight(updated.view.OngoingSnapshot())
	if !strings.Contains(plain, "Fast mode is off") {
		t.Fatalf("expected status transcript entry, got %q", plain)
	}
	if updated.transientStatus != "Fast mode disabled" {
		t.Fatalf("did not expect /fast status to overwrite transient status, got %q", updated.transientStatus)
	}
}

func TestSlashFastStatusNoticeReplacesWithoutWaitingForClear(t *testing.T) {
	m := newProjectedStaticUIModel(WithUIFastModeAvailable(true))
	m.termWidth = 100
	m.termHeight = 24
	m.windowSizeKnown = true
	m.syncViewport()
	m.input = "/fast on"

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if !updated.fastModeEnabled {
		t.Fatal("expected fast mode enabled")
	}
	if !strings.Contains(updated.transientStatus, "Fast mode enabled") {
		t.Fatalf("expected enable status, got %q", updated.transientStatus)
	}

	updated.input = "/fast off"
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	if updated.fastModeEnabled {
		t.Fatal("expected fast mode disabled")
	}
	if !strings.Contains(updated.transientStatus, "Fast mode disabled") {
		t.Fatalf("expected immediate replacement disable status, got %q", updated.transientStatus)
	}
}

func TestSlashFastUnavailableShowsError(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 24
	m.windowSizeKnown = true
	m.syncViewport()
	m.input = "/fast on"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected transient status clear timer cmd")
	}
	if updated.fastModeEnabled {
		t.Fatal("did not expect fast mode enabled")
	}
	if !strings.Contains(updated.transientStatus, "OpenAI-based Responses providers") {
		t.Fatalf("expected availability error status, got %q", updated.transientStatus)
	}
	plain := stripANSIAndTrimRight(updated.View())
	if !strings.Contains(plain, "Fast mode is only available for OpenAI-based Responses providers") {
		t.Fatalf("expected transcript error for unavailable fast mode, got %q", plain)
	}
}

func TestSlashFastWithEngineTogglesRuntime(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	eng, err := runtime.New(store, statusLineFastClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5.3-codex"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	m := newProjectedEngineUIModel(eng)
	m.input = "/fast on"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected transient status clear timer cmd")
	}
	if !eng.FastModeEnabled() {
		t.Fatal("expected runtime fast mode enabled")
	}
	if !updated.fastModeEnabled {
		t.Fatal("expected ui fast mode enabled")
	}

	updated.input = "/fast off"
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	if eng.FastModeEnabled() {
		t.Fatal("expected runtime fast mode disabled")
	}
	if updated.fastModeEnabled {
		t.Fatal("expected ui fast mode disabled")
	}
}

func TestSlashSupervisorTogglesReviewerInvocationAndShowsStatus(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 24
	m.windowSizeKnown = true
	m.syncViewport()
	m.input = "/supervisor"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected transient status clear timer cmd")
	}
	if !updated.reviewerEnabled {
		t.Fatal("expected reviewer invocation enabled after toggle")
	}
	if updated.reviewerMode != "edits" {
		t.Fatalf("expected reviewer mode edits after toggle, got %q", updated.reviewerMode)
	}
	if updated.input != "" {
		t.Fatalf("expected input cleared after /supervisor, got %q", updated.input)
	}
	if !strings.Contains(updated.transientStatus, "Supervisor invocation enabled") {
		t.Fatalf("expected transient status for /supervisor toggle, got %q", updated.transientStatus)
	}
	plain := stripANSIAndTrimRight(updated.View())
	if !strings.Contains(plain, "Supervisor invocation enabled") {
		t.Fatalf("expected transcript notice for /supervisor toggle, got %q", plain)
	}
	for _, msg := range collectCmdMessages(t, cmd) {
		next, _ = updated.Update(msg)
		updated = next.(*uiModel)
	}

	updated.input = "/supervisor off"
	next, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected transient status clear timer cmd")
	}
	if updated.reviewerEnabled {
		t.Fatal("expected reviewer invocation disabled")
	}
	if updated.reviewerMode != "off" {
		t.Fatalf("expected reviewer mode off after disable, got %q", updated.reviewerMode)
	}
	if !strings.Contains(updated.transientStatus, "Supervisor invocation disabled") {
		t.Fatalf("expected disable transient status, got %q", updated.transientStatus)
	}
}

func TestBusySlashSupervisorExecutesImmediatelyWithoutQueueing(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.busy = true
	m.activity = uiActivityRunning
	m.input = "/supervisor on"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected transient status clear timer cmd")
	}
	if !updated.busy {
		t.Fatal("expected busy state unchanged while command executes")
	}
	if !updated.reviewerEnabled || updated.reviewerMode != "edits" {
		t.Fatalf("expected reviewer enabled in edits mode, got enabled=%v mode=%q", updated.reviewerEnabled, updated.reviewerMode)
	}
	if len(updated.queued) != 0 {
		t.Fatalf("expected no queued messages, got %d", len(updated.queued))
	}
	if len(updated.pendingInjected) != 0 {
		t.Fatalf("expected no pending injected messages, got %d", len(updated.pendingInjected))
	}
	if updated.input != "" {
		t.Fatalf("expected input cleared after /supervisor, got %q", updated.input)
	}
}

func TestBusySlashSupervisorOffAppliesToInFlightRunCompletion(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	mainClient := &busyToggleFakeClient{
		delay: 80 * time.Millisecond,
		responses: []llm.Response{{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
			Usage:     llm.Usage{WindowTokens: 200000},
		}},
	}
	reviewerClient := &busyToggleFakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: `{"suggestions":[]}`},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
	eng, err := runtime.New(store, mainClient, tools.NewRegistry(), runtime.Config{
		Model: "gpt-5",
		Reviewer: runtime.ReviewerConfig{
			Frequency:     "all",
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
		_, submitErr := eng.SubmitUserMessage(context.Background(), "hello")
		submitDone <- submitErr
	}()
	time.Sleep(10 * time.Millisecond)

	m.input = "/supervisor off"
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if updated.reviewerEnabled || updated.reviewerMode != "off" {
		t.Fatalf("expected ui reviewer disabled after /supervisor off, got enabled=%v mode=%q", updated.reviewerEnabled, updated.reviewerMode)
	}
	if got := eng.ReviewerFrequency(); got != "off" {
		t.Fatalf("expected runtime reviewer mode off, got %q", got)
	}

	if err := <-submitDone; err != nil {
		t.Fatalf("submit user message: %v", err)
	}
	if got := reviewerClient.CallCount(); got != 0 {
		t.Fatalf("expected no reviewer call for in-flight run after /supervisor off, got %d", got)
	}
}
