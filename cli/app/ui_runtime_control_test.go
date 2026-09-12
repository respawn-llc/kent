package app

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"core/server/llm"
	"core/shared/clientui"
	chatcontextpb "core/shared/protoapi/gen/kent/api/chat_context"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/serverapi"
	"core/shared/textutil"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type runtimeControlFakeClient struct {
	status                *runtimepb.Status
	sessionView           *runtimepb.SessionView
	mainView              *runtimepb.MainView
	cachedMainView        *runtimepb.MainView
	hasCachedMainView     bool
	setSessionNameArg     string
	goal                  *runtimepb.GoalView
	showGoalCalls         int
	setGoalArg            string
	pauseGoalCalls        int
	resumeGoalCalls       int
	clearGoalCalls        int
	appendCalls           int
	appendedRole          string
	appendedText          string
	submitText            string
	submitInput           runtimeinput.Input
	submitCalls           int
	submitResult          string
	interruptCalls        int
	submitQueuedID        string
	discardQueuedID       string
	discardQueuedCalls    int
	discardQueuedResult   bool
	recordedPromptHistory string
	refreshMainViewCalls  int
	compactRequest        clientui.RuntimeCompactRequest
	err                   error
	appendErr             error
	submitErr             error
	interruptErr          error
	collaborative         bool
}

func TestUserTurnSubmissionFromResponsePreservesMessagePresence(t *testing.T) {
	t.Parallel()
	blank := ""
	withBlank := userTurnSubmissionFromResponse(&runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_SilentFinal{SilentFinal: &runtimepb.SubmitUserTurnSilentFinal{Message: blank}}}, "turn")
	if withBlank.Message == nil || *withBlank.Message != "" {
		t.Fatalf("blank submission message = %v, want present empty message", withBlank.Message)
	}
	if withBlank.ResultKind != clientui.UserTurnResultKindSilentFinal {
		t.Fatalf("blank submission result kind = %v, want silent final", withBlank.ResultKind)
	}

	withoutMessage := userTurnSubmissionFromResponse(&runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_NoFinal{NoFinal: &runtimepb.SubmitUserTurnNoFinal{}}}, "turn")
	if withoutMessage.Message != nil {
		t.Fatalf("omitted submission message = %v, want absent", withoutMessage.Message)
	}
}

type timeoutNetError struct{}

func (timeoutNetError) Error() string   { return "timeout" }
func (timeoutNetError) Timeout() bool   { return true }
func (timeoutNetError) Temporary() bool { return false }

func (f *runtimeControlFakeClient) MainView() *runtimepb.MainView {
	if f.mainView != nil {
		view := proto.Clone(f.mainView).(*runtimepb.MainView)
		if view.Status == nil {
			view.Status = f.Status()
		}
		if view.Session == nil {
			view.Session = f.SessionView()
		}
		return view
	}
	return &runtimepb.MainView{Status: f.Status(), Session: f.SessionView()}
}
func (f *runtimeControlFakeClient) IsCollaborativeRuntime() bool { return f.collaborative }
func (f *runtimeControlFakeClient) CachedMainView() (*runtimepb.MainView, bool) {
	if f.hasCachedMainView {
		return proto.Clone(f.cachedMainView).(*runtimepb.MainView), true
	}
	return f.MainView(), true
}
func (f *runtimeControlFakeClient) RefreshMainView() (*runtimepb.MainView, error) {
	f.refreshMainViewCalls++
	return f.MainView(), f.err
}
func (f *runtimeControlFakeClient) Status() *runtimepb.Status {
	if f.status == nil {
		return &runtimepb.Status{}
	}
	return proto.Clone(f.status).(*runtimepb.Status)
}
func (f *runtimeControlFakeClient) SessionView() *runtimepb.SessionView {
	if f.sessionView == nil {
		return &runtimepb.SessionView{}
	}
	return proto.Clone(f.sessionView).(*runtimepb.SessionView)
}
func (f *runtimeControlFakeClient) SetSessionName(name string) error {
	f.setSessionNameArg = name
	return f.err
}
func (f *runtimeControlFakeClient) ReadChatSettings() (*chatsettingspb.Settings, error) {
	return runtimeControlFakeChatSettings(), f.err
}
func (f *runtimeControlFakeClient) MutateChatSettings(operation *chatsettingspb.MutationOperation) (*chatsettingspb.MutationSuccess, error) {
	settings := runtimeControlFakeChatSettings()
	switch operation := operation.Operation.(type) {
	case *chatsettingspb.MutationOperation_Thinking:
		settings.SelectedAgent.Thinking = operation.Thinking
		if f.status == nil {
			f.status = &runtimepb.Status{}
		}
		f.status.ThinkingLevel = settings.SelectedAgent.Thinking
	case *chatsettingspb.MutationOperation_Supervisor:
		settings.Supervisor.Value = operation.Supervisor
	case *chatsettingspb.MutationOperation_FastEnabled:
		settings.Fast = &chatsettingspb.Fast{Value: operation.FastEnabled}
	case *chatsettingspb.MutationOperation_QuestionsEnabled:
		settings.Questions.Enabled = operation.QuestionsEnabled
	case *chatsettingspb.MutationOperation_AutoCompactionEnabled:
		settings.AutoCompaction.Stored = operation.AutoCompactionEnabled
		settings.AutoCompaction.Effective = operation.AutoCompactionEnabled
	}
	return &chatsettingspb.MutationSuccess{
		Context:  &chatcontextpb.Context{},
		Result:   &chatsettingspb.MutationResult{Outcome: &chatsettingspb.MutationResult_Applied{Applied: &chatsettingspb.MutationApplied{Changed: true}}},
		Settings: settings}, f.err
}

func runtimeControlFakeChatSettings() *chatsettingspb.Settings {
	return &chatsettingspb.Settings{
		SelectedAgent: &chatsettingspb.AgentSummary{
			Role:     "default",
			Model:    "gpt-5",
			Thinking: "medium"},
		Supervisor: &chatsettingspb.Supervisor{
			Value:    chatsettingspb.SupervisorValue_SUPERVISOR_VALUE_OFF,
			Baseline: chatsettingspb.SupervisorValue_SUPERVISOR_VALUE_AFTER_EDITS},
		Questions: &chatsettingspb.Questions{Enabled: true},
		AutoCompaction: &chatsettingspb.AutoCompaction{
			Stored:    true,
			Effective: true}}
}
func (f *runtimeControlFakeClient) ShowGoal() (*runtimepb.GoalView, error) {
	f.showGoalCalls++
	return cloneRuntimeGoal(f.goal), f.err
}
func (f *runtimeControlFakeClient) SetGoal(objective string) (clientui.GoalMutationResult, error) {
	f.setGoalArg = objective
	f.goal = runtimeControlTestGoal(objective, runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE)
	return clientui.GoalMutationResult{
		Kind: runtimepb.GoalMutationResultKind_GOAL_MUTATION_RESULT_KIND_AUTHORITATIVE_GOAL,
		Goal: f.goal.Goal}, f.err
}
func (f *runtimeControlFakeClient) PauseGoal() (clientui.GoalMutationResult, error) {
	f.pauseGoalCalls++
	if f.goal == nil {
		f.goal = runtimeControlTestGoal("objective", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE)
	}
	f.goal.Goal.Status = runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_PAUSED
	return clientui.GoalMutationResult{
		Kind: runtimepb.GoalMutationResultKind_GOAL_MUTATION_RESULT_KIND_AUTHORITATIVE_GOAL,
		Goal: f.goal.Goal}, f.err
}
func (f *runtimeControlFakeClient) ResumeGoal() (clientui.GoalMutationResult, error) {
	f.resumeGoalCalls++
	if f.goal == nil {
		f.goal = runtimeControlTestGoal("objective", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE)
	}
	f.goal.Goal.Status = runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE
	return clientui.GoalMutationResult{
		Kind: runtimepb.GoalMutationResultKind_GOAL_MUTATION_RESULT_KIND_AUTHORITATIVE_GOAL,
		Goal: f.goal.Goal}, f.err
}
func (f *runtimeControlFakeClient) CompleteGoal() (clientui.GoalMutationResult, error) {
	if f.goal == nil {
		f.goal = runtimeControlTestGoal("objective", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE)
	}
	f.goal.Goal.Status = runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_COMPLETE
	return clientui.GoalMutationResult{
		Kind: runtimepb.GoalMutationResultKind_GOAL_MUTATION_RESULT_KIND_AUTHORITATIVE_GOAL,
		Goal: f.goal.Goal}, f.err
}
func (f *runtimeControlFakeClient) ClearGoal() (clientui.GoalMutationResult, error) {
	f.clearGoalCalls++
	f.goal = nil
	return clientui.GoalMutationResult{Kind: runtimepb.GoalMutationResultKind_GOAL_MUTATION_RESULT_KIND_AUTHORITATIVE_CLEAR}, f.err
}

func runtimeControlTestGoal(objective string, status runtimepb.GoalStatus) *runtimepb.GoalView {
	now := time.Unix(1, 0)
	return &runtimepb.GoalView{Goal: &runtimepb.Goal{Id: "goal-1",
		Objective: objective,
		Status:    status,
		CreatedAt: timestamppb.New(now),
		UpdatedAt: timestamppb.New(now)}}
}
func (f *runtimeControlFakeClient) AppendCommittedEntry(role, text string) error {
	return f.AppendCommittedEntryWithNoticeID(role, text, "")
}
func (f *runtimeControlFakeClient) AppendCommittedEntryWithNoticeID(role, text, noticeID string) error {
	f.appendCalls++
	f.appendedRole = role
	f.appendedText = text
	if f.appendErr != nil {
		return f.appendErr
	}
	return f.err
}
func (f *runtimeControlFakeClient) submitUserMessage(_ context.Context, text string) (clientui.UserTurnSubmission, error) {
	f.submitText = text
	result := clientui.UserTurnSubmission{Message: textutil.Value(f.submitResult)}
	if f.submitErr != nil {
		return result, f.submitErr
	}
	return result, f.err
}
func (f *runtimeControlFakeClient) SubmitRuntimeInput(ctx context.Context, req clientui.RuntimeSubmitRequest) (clientui.UserTurnSubmission, error) {
	f.submitCalls++
	f.submitInput = req.Input
	text := runtimeSubmitInputText(req)
	submission, err := f.submitUserMessage(ctx, text)
	if err == nil && strings.TrimSpace(f.submitQueuedID) != "" {
		submission.Queued = clientui.QueuedUserMessage{
			ID:   strings.TrimSpace(f.submitQueuedID),
			Text: text}
	}
	return submission, err
}
func (f *runtimeControlFakeClient) submitUserShellCommand(_ context.Context, command string) error {
	return f.err
}
func (f *runtimeControlFakeClient) RunUserShell(ctx context.Context, req clientui.RuntimeShellRequest) error {
	return f.submitUserShellCommand(ctx, req.Command)
}
func (f *runtimeControlFakeClient) compactContext(_ context.Context, args string) error {
	_ = args
	return f.err
}
func (f *runtimeControlFakeClient) CompactRuntime(ctx context.Context, req clientui.RuntimeCompactRequest) error {
	f.compactRequest = req
	input, err := req.Admission.CanonicalInput()
	if err != nil {
		return err
	}
	return f.compactContext(ctx, input)
}
func (f *runtimeControlFakeClient) Interrupt() error {
	f.interruptCalls++
	if f.interruptErr != nil {
		return f.interruptErr
	}
	return f.err
}
func (f *runtimeControlFakeClient) DiscardQueuedUserMessage(queueItemID string) bool {
	f.discardQueuedCalls++
	f.discardQueuedID = queueItemID
	if f.discardQueuedResult {
		return true
	}
	return false
}
func (f *runtimeControlFakeClient) RecordPromptHistory(text string) error {
	f.recordedPromptHistory = text
	return f.err
}

func TestGoalShowSupersededByMutationDoesNotOverwriteMutationResult(t *testing.T) {
	m := newProjectedClosedUIModel(&runtimeControlFakeClient{})
	m.sessionID = "session-1"

	if cmd := m.goalRuntimeCommand(goalRuntimePause, ""); cmd == nil {
		t.Fatal("initial Goal mutation did not start")
	}
	mutationToken := m.goalRuntimePending.token
	if cmd := m.goalRuntimeCommand(goalRuntimeShow, ""); cmd == nil {
		t.Fatal("Goal read did not start")
	}
	showToken := m.goalRuntimeToken
	showMutationSerial := m.goalRuntimeMutationSerial
	if cmd := m.goalRuntimeCommand(goalRuntimePause, ""); cmd != nil {
		t.Fatal("matching Goal mutation did not coalesce")
	}

	paused := &runtimepb.GoalView{
		Goal: &runtimepb.Goal{Id: "goal-1", Objective: "latest", Status: runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_PAUSED}}
	m.applyGoalRuntimeDone(goalRuntimeDoneMsg{
		token:          mutationToken,
		sessionID:      m.sessionID,
		mutationSerial: m.goalRuntimeMutationSerial,
		operation:      goalRuntimePause,
		mutation: clientui.GoalMutationResult{
			Kind: runtimepb.GoalMutationResultKind_GOAL_MUTATION_RESULT_KIND_AUTHORITATIVE_GOAL,
			Goal: paused.Goal}})
	stale := &runtimepb.GoalView{
		Goal: &runtimepb.Goal{Id: "goal-1", Objective: "stale", Status: runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE}}
	m.applyGoalRuntimeDone(goalRuntimeDoneMsg{
		token:          showToken,
		sessionID:      m.sessionID,
		mutationSerial: showMutationSerial,
		operation:      goalRuntimeShow,
		goal:           stale})

	if m.goal.goal == nil ||
		m.goal.goal.Objective != "latest" ||
		m.goal.goal.Status != runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_PAUSED {
		t.Fatalf("Goal projection = %+v, want latest paused mutation result", m.goal.goal)
	}
}

func TestRuntimeInterruptNotAcceptedClearsPendingAttempt(t *testing.T) {
	client := &runtimeControlFakeClient{
		interruptErr: serverapi.NewRuntimeCommandNotAcceptedError(errors.New("no active Agent Turn"))}
	m := newProjectedClosedUIModel(client)
	m.sessionID = "session-1"
	m.setRuntimeActivityBusyForTest(true)
	m.activeSubmit = activeSubmitState{
		token: 1,
		text:  "keep me"}

	cmd := m.inputController().interruptBusyRuntime()
	if !m.hasPendingInterrupt() {
		t.Fatal("interrupt attempt was not marked pending")
	}
	done := cmd().(runtimeControlDoneMsg)
	next, _ := m.Update(done)
	updated := next.(*uiModel)

	if updated.hasPendingInterrupt() {
		t.Fatal("not-accepted interrupt remained pending")
	}
	if updated.activeSubmit.token != 1 || updated.activeSubmit.text != "keep me" {
		t.Fatalf("not-accepted interrupt changed active submit: %+v", updated.activeSubmit)
	}
}

func TestInterruptedHumanInputRestoresServerOrderBeforeComposer(t *testing.T) {
	m := newProjectedClosedUIModel(&runtimeControlFakeClient{})
	firstID := runtimeids.NewQueueItemID()
	secondID := runtimeids.NewQueueItemID()
	m.injectedQueue = []injectedRuntimeQueueItem{
		{LocalID: firstID.String(), ServerID: firstID.String(), Text: "local stale first", State: injectedRuntimeQueueEnqueued},
		{LocalID: secondID.String(), ServerID: secondID.String(), Text: "local stale second", State: injectedRuntimeQueueEnqueued}}
	testSetMainInput(m, "composer")

	cmd := m.applyTranscriptHumanInputInterrupted(&transcriptpb.HumanInputInterrupted{
		Items: []*transcriptpb.InterruptedHumanInputItem{
			{QueueItemId: firstID.String(), Text: "  server first  "},
			{QueueItemId: secondID.String(), Text: "server second\nline"}}})
	if cmd == nil {
		t.Fatal("interruption event returned no status command")
	}
	if got, want := testMainInput(m), "  server first  \n\nserver second\nline\n\ncomposer"; got != want {
		t.Fatalf("composer = %q, want %q", got, want)
	}
	if len(m.injectedQueue) != 0 {
		t.Fatalf("interrupted items remain queued: %+v", m.injectedQueue)
	}
}

func TestPendingWorkTechnicalRestorationMergesBeforeComposer(t *testing.T) {
	m := newProjectedClosedUIModel(&runtimeControlFakeClient{})
	testSetMainInput(m, "composer")

	m.applyAdmittedTranscriptMessageState(transcriptTestMessage(2, &transcriptpb.PendingWorkRestored{
		Restoration: &transcriptpb.PendingWorkTechnicalRestoration{ItemId: runtimeids.NewQueueItemID().String(),
			Kind:           runtimepb.PendingWorkItemKind_PENDING_WORK_ITEM_KIND_WORKTREE_TRANSITION,
			CanonicalInput: "/wt leave"}}),
		runtimeTupleMergeResult{})
	if got, want := testMainInput(m), "/wt leave\n\ncomposer"; got != want {
		t.Fatalf("composer = %q, want %q", got, want)
	}
}

func TestTranscriptSessionSettingFeedbackUsesTransientStatusWithoutTranscriptRows(t *testing.T) {
	disableTransientStatusClearForTest(t)
	client := &runtimeControlFakeClient{}
	model := newProjectedClosedUIModel(client)
	enabled := true
	cmd := model.applyAdmittedTranscriptMessageState(transcriptTestMessage(2, &transcriptpb.SessionSettingFeedback{
		Kind: transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_FAST_MODE, Changed: true, Value: &transcriptpb.SessionSettingFeedback_FastMode{FastMode: enabled}}),
		runtimeTupleMergeResult{})
	for _, msg := range collectCmdMessages(t, cmd) {
		next, _ := model.Update(msg)
		model = next.(*uiModel)
	}
	if model.transientStatus == "" || model.transientStatusKind != uiStatusNoticeSuccess {
		t.Fatalf("setting feedback status = %q kind=%v, want success notice", model.transientStatus, model.transientStatusKind)
	}
	if client.appendCalls != 0 {
		t.Fatalf("setting feedback appended %d transcript rows", client.appendCalls)
	}

	model.transientStatus = ""
	model.applyAdmittedTranscriptMessageState(transcriptTestMessage(3, &transcriptpb.SessionSettingFeedback{
		Kind: transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_FAST_MODE, Changed: false, Value: &transcriptpb.SessionSettingFeedback_FastMode{FastMode: enabled}}),
		runtimeTupleMergeResult{})
	if model.transientStatus != "" {
		t.Fatalf("unchanged setting emitted success notice %q", model.transientStatus)
	}
}

func TestThinkingQueryUsesStatusOnly(t *testing.T) {
	disableTransientStatusClearForTest(t)

	client := &runtimeControlFakeClient{status: &runtimepb.Status{ThinkingLevel: "medium"}}
	m := newProjectedTestUIModel(client)

	next, cmd := m.inputController().handleThinkingLevelCommand("")
	updated := next.(*uiModel)
	for _, msg := range collectCmdMessages(t, cmd) {
		next, _ = updated.Update(msg)
		updated = next.(*uiModel)
	}

	if client.appendCalls != 0 {
		t.Fatalf("thinking query must not append transcript entries, got %d append calls", client.appendCalls)
	}
	if updated.transientStatus == "" || updated.transientStatusKind != uiStatusNoticeInfo {
		t.Fatalf("thinking query should surface an info status notice, got status=%q kind=%v", updated.transientStatus, updated.transientStatusKind)
	}
}

func TestThinkingSetUsesChatSettingsService(t *testing.T) {
	disableTransientStatusClearForTest(t)

	m := newProjectedTestUIModel(&runtimeControlFakeClient{})

	next, cmd := m.inputController().handleThinkingLevelCommand("low")
	updated := next.(*uiModel)
	for _, msg := range collectCmdMessages(t, cmd) {
		next, _ = updated.Update(msg)
		updated = next.(*uiModel)
	}

	if updated.thinkingLevel != "low" {
		t.Fatalf("thinking level = %q, want low", updated.thinkingLevel)
	}
	if updated.transientStatus == "" || updated.transientStatusKind != uiStatusNoticeSuccess {
		t.Fatalf("thinking set should surface a success status notice, got status=%q kind=%v", updated.transientStatus, updated.transientStatusKind)
	}
}

func TestThinkingRuntimeCompletionUsesStatusOnly(t *testing.T) {
	disableTransientStatusClearForTest(t)

	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client)
	cmd := m.chatSettingsMutationCommand(&chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_Thinking{Thinking: "high"}})
	msgs := collectCmdMessages(t, cmd)

	var done chatSettingsDoneMsg
	for _, msg := range msgs {
		if typed, ok := msg.(chatSettingsDoneMsg); ok {
			done = typed
		}
	}
	next, cmd := m.Update(done)
	updated := next.(*uiModel)
	for _, msg := range collectCmdMessages(t, cmd) {
		next, _ = updated.Update(msg)
		updated = next.(*uiModel)
	}

	if client.appendCalls != 0 {
		t.Fatalf("thinking runtime completion must not append transcript entries, got %d append calls", client.appendCalls)
	}
	if updated.thinkingLevel != "high" {
		t.Fatalf("thinking level = %q, want high", updated.thinkingLevel)
	}
	if updated.transientStatus == "" || updated.transientStatusKind != uiStatusNoticeSuccess {
		t.Fatalf("thinking runtime completion should surface a success status notice, got status=%q kind=%v", updated.transientStatus, updated.transientStatusKind)
	}
}

func TestRuntimeControlCompletionsAreScopedPerOperation(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client)
	m.startupCmds = nil

	sessionCmd := m.runtimeControlCommand(runtimeControlSetSessionName, "incident triage", false, "")
	thinkingCmd := m.chatSettingsMutationCommand(&chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_Thinking{Thinking: "high"}})
	sessionMsgs := collectCmdMessages(t, sessionCmd)
	thinkingMsgs := collectCmdMessages(t, thinkingCmd)

	var sessionDone runtimeControlDoneMsg
	for _, msg := range sessionMsgs {
		if typed, ok := msg.(runtimeControlDoneMsg); ok {
			sessionDone = typed
		}
	}
	var thinkingDone chatSettingsDoneMsg
	for _, msg := range thinkingMsgs {
		if typed, ok := msg.(chatSettingsDoneMsg); ok {
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
func TestSubmitErrorShowsTransientStatusWithoutPersisting(t *testing.T) {
	disableTransientStatusClearForTest(t)

	client := &runtimeControlFakeClient{}
	m := newProjectedStaticUIModel()
	m.engine = client
	m.setRuntimeActivityBusyForTest(true)
	m.activeSubmit = activeSubmitState{token: 1, text: "prompt"}

	next, cmd := m.Update(submitDoneMsg{token: 1, submittedText: "prompt", err: errors.New("submit failed")})
	updated := next.(*uiModel)

	if updated.activity != uiActivityError {
		t.Fatalf("expected error activity, got %v", updated.activity)
	}
	for _, msg := range collectCmdMessages(t, cmd) {
		next, _ = updated.Update(msg)
		updated = next.(*uiModel)
	}
	if client.appendedRole != "" || client.appendedText != "" {
		t.Fatalf("engine is sole persister: client must not persist a run-error entry, got role=%q text=%q", client.appendedRole, client.appendedText)
	}
	if updated.transientStatus == "" || updated.transientStatusKind != uiStatusNoticeError {
		t.Fatalf("expected a transient error status for a submit failure, got status=%q kind=%v", updated.transientStatus, updated.transientStatusKind)
	}
}

func TestRuntimeControlMarksDisconnectOnTransportError(t *testing.T) {
	client := &runtimeControlFakeClient{submitErr: io.EOF}
	m := newProjectedTestUIModel(client)

	if _, err := m.submitRuntimeUserMessage(context.Background(), "prompt"); !errors.Is(err, io.EOF) {
		t.Fatalf("submit runtime user message err = %v, want EOF", err)
	}
	if !m.runtimeDisconnectStatusVisible() {
		t.Fatal("expected runtime disconnect notice after transport error")
	}
}

func TestRuntimeControlClearsDisconnectOnReachableServerError(t *testing.T) {
	client := &runtimeControlFakeClient{submitErr: &llm.APIStatusError{StatusCode: 429, Body: "rate limit"}}
	m := newProjectedTestUIModel(client)
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
	m := newProjectedTestUIModel(client)

	if _, err := m.submitRuntimeUserMessage(context.Background(), "prompt"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("submit runtime user message err = %v, want deadline exceeded", err)
	}
	if m.runtimeDisconnectStatusVisible() {
		t.Fatal("did not expect timeout to mark disconnect")
	}
}

func TestRuntimeControlTimeoutDoesNotClearExistingDisconnect(t *testing.T) {
	client := &runtimeControlFakeClient{submitErr: context.DeadlineExceeded}
	m := newProjectedTestUIModel(client)
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
	m := newProjectedTestUIModel(client)

	if _, err := m.submitRuntimeUserMessage(context.Background(), "prompt"); err == nil {
		t.Fatal("expected submit runtime user message error")
	}
	if m.runtimeDisconnectStatusVisible() {
		t.Fatal("did not expect URL timeout to mark disconnect")
	}
}

func TestRuntimeControlOpTimeoutDoesNotMarkDisconnect(t *testing.T) {
	client := &runtimeControlFakeClient{submitErr: &net.OpError{Op: "read", Net: "tcp", Err: timeoutNetError{}}}
	m := newProjectedTestUIModel(client)

	if _, err := m.submitRuntimeUserMessage(context.Background(), "prompt"); err == nil {
		t.Fatal("expected submit runtime user message error")
	}
	if m.runtimeDisconnectStatusVisible() {
		t.Fatal("did not expect op timeout to mark disconnect")
	}
}
