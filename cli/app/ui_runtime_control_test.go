package app

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"testing"

	"builder/cli/tui"
	"builder/server/llm"
	"builder/server/primaryrun"
	"builder/shared/clientui"
	"builder/shared/transcript"
)

type runtimeControlFakeClient struct {
	status                 clientui.RuntimeStatus
	sessionView            clientui.RuntimeSessionView
	mainView               clientui.RuntimeMainView
	cachedMainView         clientui.RuntimeMainView
	hasCachedMainView      bool
	transcript             clientui.TranscriptPage
	setSessionNameArg      string
	setThinkingLevelArg    string
	setFastModeArg         bool
	setFastModeCalls       int
	setReviewerArg         bool
	setAutoCompactArg      bool
	goal                   *clientui.RuntimeGoal
	setGoalArg             string
	pauseGoalCalls         int
	resumeGoalCalls        int
	clearGoalCalls         int
	appendedRole           string
	appendedText           string
	shouldCompactText      string
	shouldCompactCalls     int
	shouldCompactResult    bool
	submitText             string
	submitResult           string
	submitShellCommand     string
	compactArgs            string
	hasQueuedUserWork      bool
	hasQueuedUserWorkCalls int
	submitQueuedResult     string
	submitQueuedCalls      int
	interruptCalls         int
	queuedText             string
	queueUserMessageCalls  int
	queueUserMessageErr    error
	queueUserMessageID     string
	discardQueuedID        string
	discardQueuedCalls     int
	discardQueuedResult    bool
	discardQueuedCount     int
	recordedPromptHistory  string
	refreshMainViewCalls   int
	refreshTranscriptCalls int
	loadTranscriptCalls    int
	err                    error
	appendErr              error
	shouldCompactErr       error
	submitErr              error
	submitShellErr         error
	compactErr             error
	hasQueuedUserWorkErr   error
	submitQueuedErr        error
	interruptErr           error
	recordPromptHistoryErr error
}

type timeoutNetError struct{}

func (timeoutNetError) Error() string   { return "timeout" }
func (timeoutNetError) Timeout() bool   { return true }
func (timeoutNetError) Temporary() bool { return false }

func (f *runtimeControlFakeClient) MainView() clientui.RuntimeMainView {
	if f.mainView.Session.SessionID != "" || f.mainView.Status.ThinkingLevel != "" || f.mainView.ActiveRun != nil {
		return f.mainView
	}
	return clientui.RuntimeMainView{Status: f.status, Session: f.sessionView}
}
func (f *runtimeControlFakeClient) CachedMainView() (clientui.RuntimeMainView, bool) {
	if f.hasCachedMainView {
		return f.cachedMainView, true
	}
	return f.MainView(), true
}
func (f *runtimeControlFakeClient) RefreshMainView() (clientui.RuntimeMainView, error) {
	f.refreshMainViewCalls++
	return f.MainView(), f.err
}
func (f *runtimeControlFakeClient) Transcript() clientui.TranscriptPage {
	if f.transcript.SessionID != "" || len(f.transcript.Entries) > 0 {
		return f.transcript
	}
	view := f.SessionView()
	return transcriptPageFromSessionView(view)
}
func (f *runtimeControlFakeClient) RefreshTranscript() (clientui.TranscriptPage, error) {
	return f.Transcript(), f.err
}
func (f *runtimeControlFakeClient) RefreshTranscriptPage(req clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	f.refreshTranscriptCalls++
	return f.LoadTranscriptPage(req)
}
func (f *runtimeControlFakeClient) LoadTranscriptPage(req clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	_ = req
	f.loadTranscriptCalls++
	return f.Transcript(), f.err
}
func (f *runtimeControlFakeClient) Status() clientui.RuntimeStatus { return f.status }
func (f *runtimeControlFakeClient) SessionView() clientui.RuntimeSessionView {
	return f.sessionView
}
func (f *runtimeControlFakeClient) SetSessionName(name string) error {
	f.setSessionNameArg = name
	return f.err
}
func (f *runtimeControlFakeClient) SetThinkingLevel(level string) error {
	f.setThinkingLevelArg = level
	return f.err
}
func (f *runtimeControlFakeClient) SetFastModeEnabled(enabled bool) (bool, error) {
	f.setFastModeArg = enabled
	f.setFastModeCalls++
	f.status.FastModeEnabled = enabled
	return true, f.err
}
func (f *runtimeControlFakeClient) SetReviewerEnabled(enabled bool) (bool, string, error) {
	f.setReviewerArg = enabled
	return true, "edits", f.err
}
func (f *runtimeControlFakeClient) SetAutoCompactionEnabled(enabled bool) (bool, bool, error) {
	f.setAutoCompactArg = enabled
	return true, enabled, f.err
}
func (f *runtimeControlFakeClient) SetQuestionsEnabled(enabled bool) (bool, error) {
	return true, f.err
}
func (f *runtimeControlFakeClient) ShowGoal() (*clientui.RuntimeGoal, error) {
	return cloneRuntimeGoal(f.goal), f.err
}
func (f *runtimeControlFakeClient) SetGoal(objective string) (*clientui.RuntimeGoal, error) {
	f.setGoalArg = objective
	f.goal = &clientui.RuntimeGoal{ID: "goal-1", Objective: objective, Status: "active"}
	return cloneRuntimeGoal(f.goal), f.err
}
func (f *runtimeControlFakeClient) PauseGoal() (*clientui.RuntimeGoal, error) {
	f.pauseGoalCalls++
	if f.goal == nil {
		f.goal = &clientui.RuntimeGoal{ID: "goal-1", Objective: "objective"}
	}
	f.goal.Status = "paused"
	return cloneRuntimeGoal(f.goal), f.err
}
func (f *runtimeControlFakeClient) ResumeGoal() (*clientui.RuntimeGoal, error) {
	f.resumeGoalCalls++
	if f.goal == nil {
		f.goal = &clientui.RuntimeGoal{ID: "goal-1", Objective: "objective"}
	}
	f.goal.Status = "active"
	return cloneRuntimeGoal(f.goal), f.err
}
func (f *runtimeControlFakeClient) ClearGoal() (*clientui.RuntimeGoal, error) {
	f.clearGoalCalls++
	f.goal = nil
	return nil, f.err
}
func (f *runtimeControlFakeClient) AppendLocalEntry(role, text string) error {
	return f.AppendLocalEntryWithNoticeID(role, text, "")
}
func (f *runtimeControlFakeClient) AppendLocalEntryWithNoticeID(role, text, noticeID string) error {
	f.appendedRole = role
	f.appendedText = text
	if f.appendErr != nil {
		return f.appendErr
	}
	return f.err
}
func (f *runtimeControlFakeClient) ShouldCompactBeforeUserMessage(_ context.Context, text string) (bool, error) {
	f.shouldCompactText = text
	f.shouldCompactCalls++
	if f.shouldCompactErr != nil {
		return f.shouldCompactResult, f.shouldCompactErr
	}
	return f.shouldCompactResult, f.err
}
func (f *runtimeControlFakeClient) SubmitUserMessage(_ context.Context, text string) (string, error) {
	f.submitText = text
	if f.submitErr != nil {
		return f.submitResult, f.submitErr
	}
	return f.submitResult, f.err
}
func (f *runtimeControlFakeClient) SubmitUserShellCommand(_ context.Context, command string) error {
	f.submitShellCommand = command
	if f.submitShellErr != nil {
		return f.submitShellErr
	}
	return f.err
}
func (f *runtimeControlFakeClient) CompactContext(_ context.Context, args string) error {
	f.compactArgs = args
	if f.compactErr != nil {
		return f.compactErr
	}
	return f.err
}
func (f *runtimeControlFakeClient) CompactContextForPreSubmit(context.Context) error {
	f.compactArgs = "__pre_submit__"
	if f.compactErr != nil {
		return f.compactErr
	}
	return f.err
}
func (f *runtimeControlFakeClient) HasQueuedUserWork() (bool, error) {
	f.hasQueuedUserWorkCalls++
	if f.hasQueuedUserWorkErr != nil {
		return f.hasQueuedUserWork, f.hasQueuedUserWorkErr
	}
	return f.hasQueuedUserWork, f.err
}
func (f *runtimeControlFakeClient) SubmitQueuedUserMessages(context.Context) (string, error) {
	f.submitQueuedCalls++
	if f.submitQueuedErr != nil {
		return f.submitQueuedResult, f.submitQueuedErr
	}
	return f.submitQueuedResult, f.err
}
func (f *runtimeControlFakeClient) Interrupt() error {
	f.interruptCalls++
	if f.interruptErr != nil {
		return f.interruptErr
	}
	return f.err
}
func (f *runtimeControlFakeClient) QueueUserMessage(text string) (clientui.QueuedUserMessage, error) {
	f.queueUserMessageCalls++
	f.queuedText = text
	if f.queueUserMessageErr != nil {
		return clientui.QueuedUserMessage{}, f.queueUserMessageErr
	}
	id := strings.TrimSpace(f.queueUserMessageID)
	if id == "" {
		id = "queue-1"
	}
	return clientui.QueuedUserMessage{ID: id, Text: text}, nil
}
func (f *runtimeControlFakeClient) DiscardQueuedUserMessage(queueItemID string) bool {
	f.discardQueuedCalls++
	f.discardQueuedID = queueItemID
	if f.discardQueuedResult {
		return true
	}
	return f.discardQueuedCount > 0
}
func (f *runtimeControlFakeClient) RecordPromptHistory(text string) error {
	f.recordedPromptHistory = text
	if f.recordPromptHistoryErr != nil {
		return f.recordPromptHistoryErr
	}
	return f.err
}

func TestRuntimeControlHelpersDelegateToRuntimeClient(t *testing.T) {
	client := &runtimeControlFakeClient{
		shouldCompactResult: true,
		submitResult:        "assistant",
		hasQueuedUserWork:   true,
		submitQueuedResult:  "queued assistant",
		discardQueuedCount:  2,
	}
	m := newProjectedStaticUIModel()
	m.engine = client

	if err := m.setRuntimeSessionName("incident triage"); err != nil {
		t.Fatalf("set runtime session name: %v", err)
	}
	if err := m.setRuntimeThinkingLevel("high"); err != nil {
		t.Fatalf("set runtime thinking level: %v", err)
	}
	if changed, err := m.setRuntimeFastModeEnabled(true); !changed || err != nil {
		t.Fatalf("set runtime fast mode = (%t, %v), want (true, nil)", changed, err)
	}
	if changed, mode, err := m.setRuntimeReviewerEnabled(true); !changed || mode != "edits" || err != nil {
		t.Fatalf("set runtime reviewer = (%t, %q, %v)", changed, mode, err)
	}
	if changed, enabled, err := m.setRuntimeAutoCompactionEnabled(false); !changed || enabled || err != nil {
		t.Fatalf("set runtime autocompaction = (%t, %t, %v), want (true, false, nil)", changed, enabled, err)
	}
	if goal, err := m.setRuntimeGoal("ship goal"); err != nil || goal == nil || goal.Objective != "ship goal" || goal.Status != "active" {
		t.Fatalf("set runtime goal = (%+v, %v), want active ship goal", goal, err)
	}
	if goal, err := m.pauseRuntimeGoal(); err != nil || goal == nil || goal.Status != "paused" {
		t.Fatalf("pause runtime goal = (%+v, %v), want paused goal", goal, err)
	}
	if goal, err := m.resumeRuntimeGoal(); err != nil || goal == nil || goal.Status != "active" {
		t.Fatalf("resume runtime goal = (%+v, %v), want active goal", goal, err)
	}
	if goal, err := m.showRuntimeGoal(); err != nil || goal == nil || goal.Status != "active" {
		t.Fatalf("show runtime goal = (%+v, %v), want active goal", goal, err)
	}
	if goal, err := m.clearRuntimeGoal(); err != nil || goal != nil {
		t.Fatalf("clear runtime goal = (%+v, %v), want nil goal", goal, err)
	}
	m.appendRuntimeLocalEntryWithNoticeID("system", "hello", "")
	message, err := m.submitRuntimeUserMessage(context.Background(), "prompt")
	if err != nil || message != "assistant" {
		t.Fatalf("submit runtime user message = (%q, %v), want (assistant, nil)", message, err)
	}
	if err := m.submitRuntimeUserShellCommand(context.Background(), "echo hi"); err != nil {
		t.Fatalf("submit runtime shell command: %v", err)
	}
	if err := m.compactRuntimeContext(context.Background(), "--force"); err != nil {
		t.Fatalf("compact runtime context: %v", err)
	}
	queuedWork, err := m.hasQueuedRuntimeUserWork()
	if err != nil || !queuedWork {
		t.Fatal("expected queued runtime user work")
	}
	queuedMessage, err := m.submitQueuedRuntimeUserMessages(context.Background())
	if err != nil || queuedMessage != "queued assistant" {
		t.Fatalf("submit queued runtime user messages = (%q, %v)", queuedMessage, err)
	}
	if err := m.interruptRuntime(); err != nil {
		t.Fatalf("interrupt runtime: %v", err)
	}
	queued, err := m.queueRuntimeUserMessage("queued text")
	if err != nil {
		t.Fatalf("queue runtime user message: %v", err)
	}
	if discarded := m.discardQueuedRuntimeUserMessage(queued.ID); !discarded {
		t.Fatal("expected queued runtime user message discarded")
	}
	if err := m.recordRuntimePromptHistory("prompt history"); err != nil {
		t.Fatalf("record runtime prompt history: %v", err)
	}

	if client.setSessionNameArg != "incident triage" || client.setThinkingLevelArg != "high" {
		t.Fatalf("unexpected set args: session=%q thinking=%q", client.setSessionNameArg, client.setThinkingLevelArg)
	}
	if !client.setFastModeArg || !client.setReviewerArg || client.setAutoCompactArg {
		t.Fatalf("unexpected toggle args: fast=%t reviewer=%t autocompact=%t", client.setFastModeArg, client.setReviewerArg, client.setAutoCompactArg)
	}
	if client.setGoalArg != "ship goal" || client.pauseGoalCalls != 1 || client.resumeGoalCalls != 1 || client.clearGoalCalls != 1 {
		t.Fatalf("unexpected goal helper side effects: set=%q pause=%d resume=%d clear=%d", client.setGoalArg, client.pauseGoalCalls, client.resumeGoalCalls, client.clearGoalCalls)
	}
	if client.appendedRole != "system" || client.appendedText != "hello" {
		t.Fatalf("unexpected appended local entry: role=%q text=%q", client.appendedRole, client.appendedText)
	}
	if client.submitText != "prompt" || client.submitShellCommand != "echo hi" {
		t.Fatalf("unexpected submission args: submit=%q shell=%q", client.submitText, client.submitShellCommand)
	}
	if client.compactArgs != "--force" {
		t.Fatalf("unexpected compact arg marker: %q", client.compactArgs)
	}
	if client.interruptCalls != 1 || client.queuedText != "queued text" || client.discardQueuedID != queued.ID || client.recordedPromptHistory != "prompt history" {
		t.Fatalf("unexpected runtime helper side effects: interrupts=%d queued=%q discard=%q history=%q", client.interruptCalls, client.queuedText, client.discardQueuedID, client.recordedPromptHistory)
	}
}

func TestRuntimeControlCompletionsAreScopedPerOperation(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.startupCmds = nil

	sessionCmd := m.runtimeControlCommand(runtimeControlSetSessionName, "incident triage", false, "")
	thinkingCmd := m.runtimeControlCommand(runtimeControlSetThinkingLevel, "high", false, "")
	sessionMsgs := collectCmdMessages(t, sessionCmd)
	thinkingMsgs := collectCmdMessages(t, thinkingCmd)

	var sessionDone runtimeControlDoneMsg
	for _, msg := range sessionMsgs {
		if typed, ok := msg.(runtimeControlDoneMsg); ok {
			sessionDone = typed
		}
	}
	var thinkingDone runtimeControlDoneMsg
	for _, msg := range thinkingMsgs {
		if typed, ok := msg.(runtimeControlDoneMsg); ok {
			thinkingDone = typed
		}
	}

	next, _ := m.Update(thinkingDone)
	updated := next.(*uiModel)
	next, _ = updated.Update(sessionDone)
	updated = next.(*uiModel)
	if updated.thinkingLevel != "high" || updated.sessionName != "incident triage" {
		t.Fatalf("expected independent completions to apply, session=%q thinking=%q", updated.sessionName, updated.thinkingLevel)
	}
}

func TestRuntimeControlTextMutationsCoalesceAfterApplyingInFlightCompletion(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.startupCmds = nil

	firstCmd := m.runtimeControlCommand(runtimeControlSetThinkingLevel, "high", false, "")
	if firstCmd == nil {
		t.Fatal("expected first thinking-level command")
	}
	secondCmd := m.runtimeControlCommand(runtimeControlSetThinkingLevel, "low", false, "")
	if secondCmd != nil {
		t.Fatal("did not expect second thinking-level command while first is in flight")
	}
	firstMsgs := collectCmdMessages(t, firstCmd)
	if client.setThinkingLevelArg != "high" {
		t.Fatalf("first thinking-level RPC = %q, want high", client.setThinkingLevelArg)
	}

	var firstDone runtimeControlDoneMsg
	for _, msg := range firstMsgs {
		if typed, ok := msg.(runtimeControlDoneMsg); ok {
			firstDone = typed
		}
	}
	next, followUpCmd := m.Update(firstDone)
	updated := next.(*uiModel)
	if updated.thinkingLevel != "high" {
		t.Fatalf("expected first thinking-level completion to update UI before follow-up, got %q", updated.thinkingLevel)
	}
	if followUpCmd == nil {
		t.Fatal("expected follow-up command for coalesced thinking-level target")
	}
	followUpMsgs := collectCmdMessages(t, followUpCmd)
	if client.setThinkingLevelArg != "low" {
		t.Fatalf("follow-up thinking-level RPC = %q, want low", client.setThinkingLevelArg)
	}

	var followUpDone runtimeControlDoneMsg
	for _, msg := range followUpMsgs {
		if typed, ok := msg.(runtimeControlDoneMsg); ok {
			followUpDone = typed
		}
	}
	next, _ = updated.Update(followUpDone)
	updated = next.(*uiModel)
	if updated.thinkingLevel != "low" {
		t.Fatalf("thinking level = %q, want low", updated.thinkingLevel)
	}
}

func TestRuntimeControlRapidFastToggleUsesPendingTargetAfterApplyingOlderCompletion(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.startupCmds = nil
	m.fastModeAvailable = true
	m.fastModeEnabled = false

	_, firstCmd := m.inputController().handleFastModeCommand("")
	if firstCmd == nil {
		t.Fatal("expected first fast toggle command")
	}
	_, secondCmd := m.inputController().handleFastModeCommand("")
	if secondCmd != nil {
		t.Fatal("did not expect second fast toggle command while first is in flight")
	}

	firstMsgs := collectCmdMessages(t, firstCmd)
	if client.setFastModeCalls != 1 || client.setFastModeArg != true {
		t.Fatalf("first fast target calls=%d arg=%t, want one true", client.setFastModeCalls, client.setFastModeArg)
	}

	var firstDone runtimeControlDoneMsg
	for _, msg := range firstMsgs {
		if typed, ok := msg.(runtimeControlDoneMsg); ok {
			firstDone = typed
		}
	}
	next, followUpCmd := m.Update(firstDone)
	updated := next.(*uiModel)
	if !updated.fastModeEnabled {
		t.Fatal("expected first fast toggle completion to apply before follow-up")
	}
	if followUpCmd == nil {
		t.Fatal("expected follow-up command for coalesced fast target")
	}
	followUpMsgs := collectCmdMessages(t, followUpCmd)
	if client.setFastModeCalls != 2 || client.setFastModeArg != false {
		t.Fatalf("follow-up fast target calls=%d arg=%t, want second false", client.setFastModeCalls, client.setFastModeArg)
	}
	var followUpDone runtimeControlDoneMsg
	for _, msg := range followUpMsgs {
		if typed, ok := msg.(runtimeControlDoneMsg); ok {
			followUpDone = typed
		}
	}
	next, _ = updated.Update(followUpDone)
	updated = next.(*uiModel)
	if updated.fastModeEnabled {
		t.Fatal("expected rapid double-toggle to end disabled")
	}
}

func TestRuntimeControlStaleSessionCompletionClearsPendingToggle(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.startupCmds = nil
	m.sessionID = "session-old"
	m.fastModeAvailable = true

	cmd := m.runtimeControlCommand(runtimeControlSetFastMode, "", true, "")
	msgs := collectCmdMessages(t, cmd)
	var done runtimeControlDoneMsg
	for _, msg := range msgs {
		if typed, ok := msg.(runtimeControlDoneMsg); ok {
			done = typed
		}
	}

	m.sessionID = "session-new"
	_, blockedCmd := m.inputController().handleFastModeCommand("on")
	if blockedCmd == nil {
		t.Fatal("expected new-session fast toggle to start even while old-session command is in flight")
	}
	_ = collectCmdMessages(t, blockedCmd)
	if client.setFastModeArg != true {
		t.Fatalf("new-session bare fast toggle should target true from cached state, got %t", client.setFastModeArg)
	}

	next, _ := m.Update(done)
	updated := next.(*uiModel)
	pending, exists := updated.runtimeControlPending[runtimeControlSetFastMode]
	if !exists || pending.sessionID != "session-new" {
		t.Fatalf("expected stale-session completion to preserve new-session pending toggle, got %+v", updated.runtimeControlPending)
	}
	_, nextCmd := updated.inputController().handleFastModeCommand("off")
	if nextCmd != nil {
		t.Fatal("expected new-session follow-up target to coalesce without a concurrent command")
	}
	if pending := updated.runtimeControlPending[runtimeControlSetFastMode]; pending.desiredEnabled {
		t.Fatalf("expected coalesced new-session desired target to be false, got %+v", pending)
	}
}

func TestRuntimeControlHelpersFallbackWithoutRuntimeClient(t *testing.T) {
	m := newProjectedStaticUIModel()

	if err := m.setRuntimeSessionName("name"); err != nil {
		t.Fatalf("set runtime session name without client: %v", err)
	}
	if err := m.setRuntimeThinkingLevel("high"); err != nil {
		t.Fatalf("set runtime thinking level without client: %v", err)
	}
	if changed, err := m.setRuntimeFastModeEnabled(true); changed || err != nil {
		t.Fatalf("set runtime fast mode without client = (%t, %v), want (false, nil)", changed, err)
	}
	if changed, mode, err := m.setRuntimeReviewerEnabled(true); changed || mode != "" || err != nil {
		t.Fatalf("set runtime reviewer without client = (%t, %q, %v)", changed, mode, err)
	}
	if changed, enabled, err := m.setRuntimeAutoCompactionEnabled(true); changed || enabled || err != nil {
		t.Fatalf("set runtime autocompaction without client = (%t, %t, %v), want (false, false, nil)", changed, enabled, err)
	}
	if goal, err := m.showRuntimeGoal(); goal != nil || err != nil {
		t.Fatalf("show runtime goal without client = (%+v, %v), want (nil, nil)", goal, err)
	}
	if goal, err := m.setRuntimeGoal("goal"); goal != nil || err != nil {
		t.Fatalf("set runtime goal without client = (%+v, %v), want (nil, nil)", goal, err)
	}
	if goal, err := m.pauseRuntimeGoal(); goal != nil || err != nil {
		t.Fatalf("pause runtime goal without client = (%+v, %v), want (nil, nil)", goal, err)
	}
	if goal, err := m.resumeRuntimeGoal(); goal != nil || err != nil {
		t.Fatalf("resume runtime goal without client = (%+v, %v), want (nil, nil)", goal, err)
	}
	if goal, err := m.clearRuntimeGoal(); goal != nil || err != nil {
		t.Fatalf("clear runtime goal without client = (%+v, %v), want (nil, nil)", goal, err)
	}
	if message, err := m.submitRuntimeUserMessage(context.Background(), "prompt"); message != "" || err != nil {
		t.Fatalf("submit runtime user message without client = (%q, %v), want (empty, nil)", message, err)
	}
	if err := m.submitRuntimeUserShellCommand(context.Background(), "echo hi"); err != nil {
		t.Fatalf("submit runtime shell command without client: %v", err)
	}
	if err := m.compactRuntimeContext(context.Background(), "--force"); err != nil {
		t.Fatalf("compact runtime context without client: %v", err)
	}
	queuedWork, err := m.hasQueuedRuntimeUserWork()
	if err != nil {
		t.Fatalf("has queued runtime user work without client: %v", err)
	}
	if queuedWork {
		t.Fatal("did not expect queued runtime user work without client")
	}
	if message, err := m.submitQueuedRuntimeUserMessages(context.Background()); message != "" || err != nil {
		t.Fatalf("submit queued runtime user messages without client = (%q, %v), want (empty, nil)", message, err)
	}
	if err := m.interruptRuntime(); err != nil {
		t.Fatalf("interrupt runtime without client: %v", err)
	}
	queued, err := m.queueRuntimeUserMessage("queued text")
	if err != nil || queued.ID == "" || queued.Text != "queued text" {
		t.Fatalf("queue runtime user message without client = (%+v, %v), want generated item", queued, err)
	}
	if discarded := m.discardQueuedRuntimeUserMessage(queued.ID); discarded {
		t.Fatal("did not expect queued runtime user message discarded without client")
	}
	if err := m.recordRuntimePromptHistory("prompt history"); err != nil {
		t.Fatalf("record runtime prompt history without client: %v", err)
	}
}

func TestSubmitErrorWithRuntimeClientAppendsActivePrimaryRunEntry(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedStaticUIModel()
	m.engine = client
	m.setBusy(true)

	next, cmd := m.Update(submitDoneMsg{err: primaryrun.ErrActivePrimaryRun})
	updated := next.(*uiModel)
	if updated.isBusy() {
		t.Fatal("did not expect busy after active primary run error")
	}
	if updated.activity != uiActivityError {
		t.Fatalf("expected error activity, got %v", updated.activity)
	}
	if len(updated.transcriptEntries) != 1 {
		t.Fatalf("expected one immediate local transcript entry, got %+v", updated.transcriptEntries)
	}
	entry := updated.transcriptEntries[0]
	if entry.Role != tui.TranscriptRoleDeveloperErrorFeedback || entry.Text != primaryrun.ErrActivePrimaryRun.Error() {
		t.Fatalf("unexpected immediate local transcript entry: %+v", entry)
	}
	if client.appendedRole != "" || client.appendedText != "" {
		t.Fatalf("did not expect runtime append during Update, got role=%q text=%q", client.appendedRole, client.appendedText)
	}
	_ = collectCmdMessages(t, cmd)
	if client.appendedRole != string(transcript.EntryRoleDeveloperErrorFeedback) || client.appendedText != primaryrun.ErrActivePrimaryRun.Error() {
		t.Fatalf("unexpected runtime local entry: role=%q text=%q", client.appendedRole, client.appendedText)
	}
}

func TestActiveSubmitErrorFallsBackToVisibleTranscriptWhenRuntimeAppendFails(t *testing.T) {
	client := &runtimeControlFakeClient{appendErr: errors.New("append failed")}
	m := newProjectedStaticUIModel()
	m.engine = client
	m.setBusy(true)

	next, cmd := m.Update(submitDoneMsg{err: primaryrun.ErrActivePrimaryRun})
	updated := next.(*uiModel)

	if updated.activity != uiActivityError {
		t.Fatalf("expected error activity, got %v", updated.activity)
	}
	if client.appendedRole != "" || client.appendedText != "" {
		t.Fatalf("did not expect runtime append during Update, got role=%q text=%q", client.appendedRole, client.appendedText)
	}
	_ = collectCmdMessages(t, cmd)
	if client.appendedRole != string(transcript.EntryRoleDeveloperErrorFeedback) || client.appendedText != primaryrun.ErrActivePrimaryRun.Error() {
		t.Fatalf("unexpected runtime local entry attempt: role=%q text=%q", client.appendedRole, client.appendedText)
	}
	if len(updated.transcriptEntries) != 1 {
		t.Fatalf("expected one fallback transcript entry, got %+v", updated.transcriptEntries)
	}
	entry := updated.transcriptEntries[0]
	if entry.Role != tui.TranscriptRoleDeveloperErrorFeedback || entry.Text != primaryrun.ErrActivePrimaryRun.Error() {
		t.Fatalf("unexpected fallback transcript entry: %+v", entry)
	}
	loaded := updated.view.LoadedTranscriptEntries()
	if len(loaded) != 1 {
		t.Fatalf("expected one loaded transcript entry, got %+v", loaded)
	}
	if loaded[0].Role != tui.TranscriptRoleDeveloperErrorFeedback || loaded[0].Text != primaryrun.ErrActivePrimaryRun.Error() {
		t.Fatalf("unexpected loaded transcript entry: %+v", loaded[0])
	}
}

func TestSubmitErrorFallsBackToVisibleTranscriptWhenRuntimeAppendFails(t *testing.T) {
	client := &runtimeControlFakeClient{appendErr: errors.New("append failed")}
	m := newProjectedStaticUIModel()
	m.engine = client
	m.setBusy(true)
	m.activeSubmit = activeSubmitState{token: 1, text: "prompt"}

	next, _ := m.Update(submitDoneMsg{token: 1, submittedText: "prompt", err: errors.New("submit failed")})
	updated := next.(*uiModel)

	if updated.activity != uiActivityError {
		t.Fatalf("expected error activity, got %v", updated.activity)
	}
	if len(updated.transcriptEntries) != 1 {
		t.Fatalf("expected one fallback transcript entry, got %+v", updated.transcriptEntries)
	}
	entry := updated.transcriptEntries[0]
	if entry.Role != tui.TranscriptRoleDeveloperErrorFeedback || entry.Text != "submit failed" {
		t.Fatalf("unexpected fallback transcript entry: %+v", entry)
	}
}

func TestRuntimeControlHelpersPropagateRuntimeErrors(t *testing.T) {
	boom := errors.New("boom")
	m := newProjectedStaticUIModel()
	m.engine = &runtimeControlFakeClient{err: boom}

	if _, err := m.showRuntimeGoal(); !errors.Is(err, boom) {
		t.Fatalf("show runtime goal error = %v, want boom", err)
	}
	if _, err := m.setRuntimeGoal("goal"); !errors.Is(err, boom) {
		t.Fatalf("set runtime goal error = %v, want boom", err)
	}
	if _, err := m.pauseRuntimeGoal(); !errors.Is(err, boom) {
		t.Fatalf("pause runtime goal error = %v, want boom", err)
	}
	if _, err := m.resumeRuntimeGoal(); !errors.Is(err, boom) {
		t.Fatalf("resume runtime goal error = %v, want boom", err)
	}
	if _, err := m.clearRuntimeGoal(); !errors.Is(err, boom) {
		t.Fatalf("clear runtime goal error = %v, want boom", err)
	}
	if err := m.setRuntimeSessionName("name"); !errors.Is(err, boom) {
		t.Fatalf("set runtime session name error = %v, want boom", err)
	}
	if _, err := m.setRuntimeFastModeEnabled(true); !errors.Is(err, boom) {
		t.Fatalf("set runtime fast mode error = %v, want boom", err)
	}
	if _, _, err := m.setRuntimeReviewerEnabled(true); !errors.Is(err, boom) {
		t.Fatalf("set runtime reviewer error = %v, want boom", err)
	}
	if _, err := m.submitRuntimeUserMessage(context.Background(), "prompt"); !errors.Is(err, boom) {
		t.Fatalf("submit runtime user message error = %v, want boom", err)
	}
	if err := m.submitRuntimeUserShellCommand(context.Background(), "echo hi"); !errors.Is(err, boom) {
		t.Fatalf("submit runtime shell command error = %v, want boom", err)
	}
	if err := m.compactRuntimeContext(context.Background(), "--force"); !errors.Is(err, boom) {
		t.Fatalf("compact runtime context error = %v, want boom", err)
	}
	if _, err := m.submitQueuedRuntimeUserMessages(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("submit queued runtime user messages error = %v, want boom", err)
	}
	if err := m.interruptRuntime(); !errors.Is(err, boom) {
		t.Fatalf("interrupt runtime error = %v, want boom", err)
	}
	if err := m.recordRuntimePromptHistory("prompt history"); !errors.Is(err, boom) {
		t.Fatalf("record runtime prompt history error = %v, want boom", err)
	}
}

func TestRuntimeControlMarksDisconnectOnTransportError(t *testing.T) {
	client := &runtimeControlFakeClient{submitErr: io.EOF}
	m := newProjectedTestUIModel(client, nil, nil)

	if _, err := m.submitRuntimeUserMessage(context.Background(), "prompt"); !errors.Is(err, io.EOF) {
		t.Fatalf("submit runtime user message err = %v, want EOF", err)
	}
	if !m.runtimeDisconnectStatusVisible() {
		t.Fatal("expected runtime disconnect notice after transport error")
	}
}

func TestRuntimeControlClearsDisconnectOnReachableServerError(t *testing.T) {
	client := &runtimeControlFakeClient{submitErr: &llm.APIStatusError{StatusCode: 429, Body: "rate limit"}}
	m := newProjectedTestUIModel(client, nil, nil)
	m.setRuntimeDisconnected(true)

	if _, err := m.submitRuntimeUserMessage(context.Background(), "prompt"); err == nil {
		t.Fatal("expected submit runtime user message error")
	}
	if m.runtimeDisconnectStatusVisible() {
		t.Fatal("expected reachable server error to clear disconnect notice")
	}
}

func TestRuntimeControlTimeoutDoesNotMarkDisconnect(t *testing.T) {
	client := &runtimeControlFakeClient{submitErr: context.DeadlineExceeded}
	m := newProjectedTestUIModel(client, nil, nil)

	if _, err := m.submitRuntimeUserMessage(context.Background(), "prompt"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("submit runtime user message err = %v, want deadline exceeded", err)
	}
	if m.runtimeDisconnectStatusVisible() {
		t.Fatal("did not expect timeout to mark disconnect")
	}
}

func TestRuntimeControlTimeoutDoesNotClearExistingDisconnect(t *testing.T) {
	client := &runtimeControlFakeClient{submitErr: context.DeadlineExceeded}
	m := newProjectedTestUIModel(client, nil, nil)
	m.setRuntimeDisconnected(true)

	if _, err := m.submitRuntimeUserMessage(context.Background(), "prompt"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("submit runtime user message err = %v, want deadline exceeded", err)
	}
	if !m.runtimeDisconnectStatusVisible() {
		t.Fatal("expected timeout not to clear existing disconnect notice")
	}
}

func TestRuntimeControlURLTimeoutDoesNotMarkDisconnect(t *testing.T) {
	client := &runtimeControlFakeClient{submitErr: &url.Error{Op: "Get", URL: "http://example.test", Err: timeoutNetError{}}}
	m := newProjectedTestUIModel(client, nil, nil)

	if _, err := m.submitRuntimeUserMessage(context.Background(), "prompt"); err == nil {
		t.Fatal("expected submit runtime user message error")
	}
	if m.runtimeDisconnectStatusVisible() {
		t.Fatal("did not expect URL timeout to mark disconnect")
	}
}

func TestRuntimeControlOpTimeoutDoesNotMarkDisconnect(t *testing.T) {
	client := &runtimeControlFakeClient{submitErr: &net.OpError{Op: "read", Net: "tcp", Err: timeoutNetError{}}}
	m := newProjectedTestUIModel(client, nil, nil)

	if _, err := m.submitRuntimeUserMessage(context.Background(), "prompt"); err == nil {
		t.Fatal("expected submit runtime user message error")
	}
	if m.runtimeDisconnectStatusVisible() {
		t.Fatal("did not expect op timeout to mark disconnect")
	}
}
