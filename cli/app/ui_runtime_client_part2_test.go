package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"core/shared/clientui"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeinput"
	"core/shared/serverapi"

	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRuntimeClientMainViewDoesNotRefreshCachedSnapshotBehindUIBack(t *testing.T) {
	reads := &countingSessionViewClient{view: &runtimepb.MainView{Session: &runtimepb.SessionView{SessionId: "session-1"}, Status: &runtimepb.Status{}}}
	controls := newUnavailableRuntimeControlService()
	runtimeClient := newTestSessionRuntimeClient(reads, controls)
	runtimeClient.storeMainView(&runtimepb.MainView{Session: &runtimepb.SessionView{SessionId: "session-1"}, Status: &runtimepb.Status{}})
	notified := make(chan error, 1)
	runtimeClient.SetConnectionStateObserver(func(err error) {
		notified <- err
	})

	_ = runtimeClient.MainView()

	if got := reads.count.Load(); got != 0 {
		t.Fatalf("main view read count = %d, want 0", got)
	}
	select {
	case err := <-notified:
		t.Fatalf("did not expect synchronous main-view refresh notification, got %v", err)
	default:
	}
}

type reconnectRetryRuntimeControlClient struct {
	mu               sync.Mutex
	firstSubmitErr   error
	firstRecordErr   error
	appendErr        error
	compactErr       error
	compactCalls     int
	showGoalErr      error
	showGoalCalls    int
	submitCalls      int
	recordCalls      int
	submitRequestID  []string
	recordRequestID  []string
	localEntries     []*transcriptpb.AppendCommittedEntryRequest
	showGoalResp     *runtimepb.GoalShowSuccess
	setGoalResp      *runtimepb.GoalMutationSuccess
	pauseGoalResp    *runtimepb.GoalMutationSuccess
	resumeGoalResp   *runtimepb.GoalMutationSuccess
	completeGoalResp *runtimepb.GoalMutationSuccess
	clearGoalResp    *runtimepb.GoalMutationSuccess
	interruptResp    *runtimepb.ReadModelUpdate
	interruptReq     *runtimepb.InterruptRequest
}

func (c *reconnectRetryRuntimeControlClient) submitRequestIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.submitRequestID...)
}

func (c *reconnectRetryRuntimeControlClient) recordRequestIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.recordRequestID...)
}

func (c *reconnectRetryRuntimeControlClient) appendedLocalEntries() []*transcriptpb.AppendCommittedEntryRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*transcriptpb.AppendCommittedEntryRequest(nil), c.localEntries...)
}

func (c *reconnectRetryRuntimeControlClient) SetSessionName(context.Context, *runtimepb.SetSessionNameRequest) error {
	return nil
}

func (c *reconnectRetryRuntimeControlClient) AppendCommittedEntry(_ context.Context, req *transcriptpb.AppendCommittedEntryRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.localEntries = append(c.localEntries, req)
	return c.appendErr
}

func (c *reconnectRetryRuntimeControlClient) ShouldCompactBeforeUserMessage(context.Context, *runtimepb.ShouldCompactRequest) (*runtimepb.ShouldCompactSuccess, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.compactCalls++
	if c.compactCalls == 1 && c.compactErr != nil {
		return &runtimepb.ShouldCompactSuccess{}, c.compactErr
	}
	return &runtimepb.ShouldCompactSuccess{}, nil
}

func (c *reconnectRetryRuntimeControlClient) SubmitUserTurn(_ context.Context, req *runtimepb.SubmitUserTurnRequest) (*runtimepb.SubmitUserTurnSuccess, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.submitCalls++
	if c.submitCalls == 1 && c.firstSubmitErr != nil {
		return &runtimepb.SubmitUserTurnSuccess{}, c.firstSubmitErr
	}
	return &runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_AssistantFinal{AssistantFinal: &runtimepb.SubmitUserTurnAssistantFinal{Message: "recovered"}}}, nil
}

func (c *reconnectRetryRuntimeControlClient) SubmitUserShellCommand(context.Context, *runtimepb.ShellCommandRequest) error {
	return nil
}

func (c *reconnectRetryRuntimeControlClient) CompactContext(context.Context, *runtimepb.CompactContextRequest) error {
	return nil
}

func (c *reconnectRetryRuntimeControlClient) Interrupt(_ context.Context, req *runtimepb.InterruptRequest) (*runtimepb.ReadModelUpdate, error) {
	c.mu.Lock()
	c.interruptReq = req
	c.mu.Unlock()
	return c.interruptResp, nil
}

func (c *reconnectRetryRuntimeControlClient) RecordPromptHistory(_ context.Context, req *promptpb.RecordHistoryRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recordCalls++
	if c.recordCalls == 1 && c.firstRecordErr != nil {
		return c.firstRecordErr
	}
	return nil
}

func (c *reconnectRetryRuntimeControlClient) ShowGoal(context.Context, *runtimepb.GoalShowRequest) (*runtimepb.GoalShowSuccess, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.showGoalCalls++
	if c.showGoalCalls == 1 && c.showGoalErr != nil {
		return &runtimepb.GoalShowSuccess{}, c.showGoalErr
	}
	return c.showGoalResp, nil
}

func (c *reconnectRetryRuntimeControlClient) SetGoal(_ context.Context, request *runtimepb.GoalSetRequest) (*runtimepb.GoalSetSuccess, error) {
	return &runtimepb.GoalSetSuccess{
		Session: request.GetTarget().GetSession(),
		Outcome: &runtimepb.GoalSetSuccess_Mutation{Mutation: c.setGoalResp},
	}, nil
}

func (c *reconnectRetryRuntimeControlClient) PauseGoal(context.Context, *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error) {
	return c.pauseGoalResp, nil
}

func (c *reconnectRetryRuntimeControlClient) ResumeGoal(context.Context, *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error) {
	return c.resumeGoalResp, nil
}

func (c *reconnectRetryRuntimeControlClient) CompleteGoal(context.Context, *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error) {
	return c.completeGoalResp, nil
}

func (c *reconnectRetryRuntimeControlClient) ClearGoal(context.Context, *runtimepb.GoalClearRequest) (*runtimepb.GoalMutationSuccess, error) {
	return c.clearGoalResp, nil
}

func runtimeClientTestShowResponse(goal *runtimepb.Goal) *runtimepb.GoalShowSuccess {
	return &runtimepb.GoalShowSuccess{
		Goal:         goal,
		Availability: runtimepb.GoalAvailability_GOAL_AVAILABILITY_AVAILABLE,
	}
}

func TestRuntimeClientInterruptDoesNotCommitRuntimeTuple(t *testing.T) {
	current := runtimeTupleTestView(
		10,
		runtimeTupleTestIdleActivity(),
	)
	controls := &reconnectRetryRuntimeControlClient{interruptResp: &runtimepb.ReadModelUpdate{
		Version:  &runtimepb.ReadModelVersion{Epoch: current.Version.Epoch, Generation: current.Version.Generation, Sequence: 11},
		Activity: runtimeTupleTestRunningActivity(),
	}}
	runtimeClient := newTestSessionRuntimeClientWithControls(controls)
	runtimeClient.storeMainView(current)

	if err := runtimeClient.Interrupt(); err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	assertRuntimeTupleView(t, runtimeClient.MainView(), current)
}

func TestCloneRuntimeGoalReturnsIndependentCopy(t *testing.T) {
	original := runtimeClientTestRuntimeGoal(
		runtimeClientTestGoal("goal-1", "ship", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE),
		true,
	)
	availability := runtimepb.GoalAvailability_GOAL_AVAILABILITY_AGENT_CAPABILITY_MISSING
	original.Availability = &availability
	cloned := cloneRuntimeGoal(original)
	original.Goal.Id = "goal-2"
	original.Goal.Objective = "mutated"
	original.Goal.Status = runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_PAUSED
	*original.Availability = runtimepb.GoalAvailability_GOAL_AVAILABILITY_AVAILABLE
	original.Suspended = false

	want := runtimeClientTestRuntimeGoal(
		runtimeClientTestGoal("goal-1", "ship", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE),
		true,
	)
	wantAvailability := runtimepb.GoalAvailability_GOAL_AVAILABILITY_AGENT_CAPABILITY_MISSING
	want.Availability = &wantAvailability
	if !proto.Equal(cloned, want) {
		t.Fatalf("clone = %+v, want %+v", cloned, want)
	}
}

func TestRuntimeClientGoalStatusEventPatchesCachedMainView(t *testing.T) {
	runtimeClient := newTestSessionRuntimeClientWithControls(&reconnectRetryRuntimeControlClient{})
	runtimeClient.storeMainView(&runtimepb.MainView{Session: &runtimepb.SessionView{SessionId: "session-1"}, Status: &runtimepb.Status{}})

	if _, err := runtimeClient.admitTranscriptMessageState(runtimeClientTestGoalMessage(runtimeClientTestGoal("goal-1", "ship feature", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE))); err != nil {
		t.Fatalf("admit goal status: %v", err)
	}
	assertRuntimeClientGoalCached(
		t,
		runtimeClient,
		runtimeClientTestRuntimeGoal(runtimeClientTestGoal("goal-1", "ship feature", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE), false),
		runtimeClientTestRuntimeGoal(runtimeClientTestGoal("goal-1", "ship feature", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE), false),
	)

	if _, err := runtimeClient.admitTranscriptMessageState(runtimeClientTestGoalMessage(runtimeClientTestGoal("goal-1", "ship feature", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_PAUSED))); err != nil {
		t.Fatalf("admit paused goal status: %v", err)
	}
	assertRuntimeClientGoalCached(
		t,
		runtimeClient,
		runtimeClientTestRuntimeGoal(runtimeClientTestGoal("goal-1", "ship feature", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_PAUSED), false),
		runtimeClientTestRuntimeGoal(runtimeClientTestGoal("goal-1", "ship feature", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_PAUSED), false),
	)

	if _, err := runtimeClient.admitTranscriptMessageState(runtimeClientTestGoalMessage(nil)); err != nil {
		t.Fatalf("admit cleared goal status: %v", err)
	}
	assertRuntimeClientGoalCached(t, runtimeClient, &runtimepb.GoalView{}, &runtimepb.GoalView{})
}

func TestRuntimeClientCanonicalGoalStatusReplacesCachedGoal(t *testing.T) {
	runtimeClient := newTestSessionRuntimeClientWithControls(&reconnectRetryRuntimeControlClient{})
	runtimeClient.storeMainView(&runtimepb.MainView{
		Session: &runtimepb.SessionView{SessionId: "session-1"},
		Status: &runtimepb.Status{Goal: runtimeClientTestRuntimeGoal(
			runtimeClientTestGoal("goal-old", "old", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE),
			true,
		)},
	})

	if _, err := runtimeClient.admitTranscriptMessageState(runtimeClientTestGoalMessage(runtimeClientTestGoal("goal-new", "new", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE))); err != nil {
		t.Fatalf("admit replacement goal status: %v", err)
	}
	assertRuntimeClientGoalCached(
		t,
		runtimeClient,
		runtimeClientTestRuntimeGoal(runtimeClientTestGoal("goal-new", "new", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE), false),
		runtimeClientTestRuntimeGoal(runtimeClientTestGoal("goal-new", "new", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE), false),
	)
}

func runtimeClientTestGoal(id, objective string, status runtimepb.GoalStatus) *runtimepb.Goal {
	now := time.Unix(1, 0).UTC()
	return &runtimepb.Goal{Id: id, Objective: objective, Status: status, CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)}
}

func runtimeClientTestRuntimeGoal(goal *runtimepb.Goal, suspended bool) *runtimepb.GoalView {
	if goal == nil {
		return nil
	}
	return &runtimepb.GoalView{Goal: goal, Suspended: suspended}
}

func runtimeClientTestGoalMessage(goal *runtimepb.Goal) *transcriptpb.Message {
	return &transcriptpb.Message{Event: &transcriptpb.Event{Payload: &transcriptpb.Event_GoalStatus{GoalStatus: &runtimepb.GoalView{Goal: goal}}}}
}

func assertRuntimeClientGoalCached(t *testing.T, runtimeClient *sessionRuntimeClient, got *runtimepb.GoalView, want *runtimepb.GoalView) {
	t.Helper()
	if !proto.Equal(got, want) {
		t.Fatalf("goal = %+v, want %+v", got, want)
	}
	view, ok := runtimeClient.CachedMainView()
	if !ok {
		t.Fatal("expected cached main view")
	}
	if !proto.Equal(view.Status.Goal, want) {
		t.Fatalf("cached goal = %+v, want %+v", view.Status.Goal, want)
	}
}

func deletedTestRuntimeClientSubmitUserMessageRecoversRuntimeUnavailableAndReusesRequestID(t *testing.T) {
	controls := &reconnectRetryRuntimeControlClient{firstSubmitErr: serverapi.ErrRuntimeUnavailable}
	runtimeClient := newTestSessionRuntimeClientWithControls(controls)
	reactivator := newRuntimeReactivator()
	recoveryCalls := 0
	reactivator.SetReactivateFunc(func(context.Context) error {
		recoveryCalls++
		return nil
	})
	runtimeClient.SetRuntimeReactivator(reactivator)

	submission, err := runtimeClient.SubmitRuntimeInput(context.Background(), clientui.RuntimeSubmitRequest{
		Input: runtimeinput.Text("hello"),
	})
	message := ""
	if submission.Message != nil {
		message = *submission.Message
	}
	if err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}
	if message != "recovered" {
		t.Fatalf("SubmitUserMessage message = %q, want recovered", message)
	}
	if recoveryCalls != 1 {
		t.Fatalf("recovery call count = %d, want 1", recoveryCalls)
	}
	if got := controls.submitRequestIDs(); len(got) != 2 || got[0] == "" || got[0] != got[1] {
		t.Fatalf("submit request ids = %+v, want same non-empty id across retry", got)
	}
}

func deletedTestRuntimeClientRecordPromptHistoryReusesRequestIDAcrossReconnect(t *testing.T) {
	controls := &reconnectRetryRuntimeControlClient{firstRecordErr: serverapi.ErrRuntimeUnavailable}
	runtimeClient := newTestSessionRuntimeClientWithControls(controls)
	reactivator := newRuntimeReactivator()
	reactivator.SetReactivateFunc(func(context.Context) error { return nil })
	runtimeClient.SetRuntimeReactivator(reactivator)

	if err := runtimeClient.RecordPromptHistory("/status"); err != nil {
		t.Fatalf("RecordPromptHistory: %v", err)
	}
	if got := controls.recordRequestIDs(); len(got) != 2 || got[0] == "" || got[0] != got[1] {
		t.Fatalf("record request ids = %+v, want same non-empty id across retry", got)
	}
}

func deletedTestRuntimeClientSubmitUserMessageRecoversRuntimeUnavailable(t *testing.T) {
	controls := &reconnectRetryRuntimeControlClient{firstSubmitErr: serverapi.ErrRuntimeUnavailable}
	runtimeClient := newTestSessionRuntimeClientWithControls(controls)
	reactivator := newRuntimeReactivator()
	recoveryCalls := 0
	reactivator.SetReactivateFunc(func(context.Context) error {
		recoveryCalls++
		return nil
	})
	runtimeClient.SetRuntimeReactivator(reactivator)

	submission, err := runtimeClient.SubmitRuntimeInput(context.Background(), clientui.RuntimeSubmitRequest{
		Input: runtimeinput.Text("hello"),
	})
	message := ""
	if submission.Message != nil {
		message = *submission.Message
	}
	if err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}
	if message != "recovered" {
		t.Fatalf("SubmitUserMessage message = %q, want recovered", message)
	}
	if recoveryCalls != 1 {
		t.Fatalf("recovery call count = %d, want 1", recoveryCalls)
	}
	entries := controls.appendedLocalEntries()
	if len(entries) != 1 {
		t.Fatalf("warning entry count = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Role != "warning" || entry.GetVisibility() != transcriptpb.AppendVisibility_APPEND_VISIBILITY_ONGOING {
		t.Fatalf("warning entry = %+v, want recovery warning", entry)
	}
}

func deletedTestRuntimeClientSubmitTurnRecoveryContinuesFirstPrompt(t *testing.T) {
	controls := &reconnectRetryRuntimeControlClient{firstSubmitErr: serverapi.ErrRuntimeUnavailable}
	runtimeClient := newTestSessionRuntimeClientWithControls(controls)
	reactivator := newRuntimeReactivator()
	reactivator.SetReactivateFunc(func(context.Context) error { return nil })
	runtimeClient.SetRuntimeReactivator(reactivator)
	model := newProjectedClosedUIModel(runtimeClient)
	model.startupCmds = nil

	submitCmd := model.inputController().startSubmissionWithPromptHistoryAndQueuePositionAndID("hello after restart", preSubmitQueueBack, "")
	if submitCmd == nil {
		t.Fatal("expected submit command")
	}
	next := tea.Model(model)
	updated := next.(*uiModel)
	submitMsgs := collectCmdMessages(t, submitCmd)
	var done submitDoneMsg
	foundDone := false
	for _, msg := range submitMsgs {
		if typed, ok := msg.(submitDoneMsg); ok {
			done = typed
			foundDone = true
		}
	}
	if !foundDone {
		t.Fatalf("expected submit result, got %+v", submitMsgs)
	}
	if done.err != nil || done.message != "recovered" {
		t.Fatalf("submit result = %+v, want recovered first prompt", done)
	}
	next, _ = updated.Update(done)
	updated = next.(*uiModel)
	if updated.activity == uiActivityError {
		t.Fatal("did not expect pre-submit recovery to surface operator error")
	}
	plain := stripANSIAndTrimRight(updated.view.View())
	if strings.Contains(plain, serverapi.ErrRuntimeUnavailable.Error()) || strings.Contains(plain, "runtime for session") {
		t.Fatalf("did not expect recovery diagnostics in ongoing transcript, got %q", plain)
	}
}

func TestRuntimeClientMainViewRefreshRecoversRuntimeUnavailableSilently(t *testing.T) {
	controls := &reconnectRetryRuntimeControlClient{}
	authoritativeView := &runtimepb.MainView{
		Session: &runtimepb.SessionView{SessionId: "session-1", SessionName: proto.String("restored")},
		Status:  &runtimepb.Status{ThinkingLevel: "high"},
	}
	reads := &flakySessionViewClient{
		errs:      []error{serverapi.ErrRuntimeUnavailable, nil},
		responses: []*sessionpb.MainViewSuccess{{}, {MainView: authoritativeView}},
	}
	runtimeClient := newTestSessionRuntimeClient(reads, controls)
	reactivator := newRuntimeReactivator()
	recoveryCalls := 0
	reactivator.SetReactivateFunc(func(context.Context) error {
		recoveryCalls++
		return nil
	})
	runtimeClient.SetRuntimeReactivator(reactivator)

	view, err := runtimeClient.RefreshMainView()
	if err != nil {
		t.Fatalf("RefreshMainView: %v", err)
	}
	if recoveryCalls != 1 {
		t.Fatalf("recovery call count = %d, want 1", recoveryCalls)
	}
	if reads.count != 2 {
		t.Fatalf("main-view read count = %d, want 2", reads.count)
	}
	if view.Session.GetSessionName() != "restored" || view.Status.ThinkingLevel != "high" {
		t.Fatalf("main view = %+v, want %+v", view, authoritativeView)
	}
	if entries := controls.appendedLocalEntries(); len(entries) != 0 {
		t.Fatalf("did not expect visible recovery warning during main-view refresh, got %+v", entries)
	}
}

func TestRuntimeClientMainViewRecoveryPreservesReadDeadline(t *testing.T) {
	controls := &reconnectRetryRuntimeControlClient{}
	reads := &flakySessionViewClient{errs: []error{serverapi.ErrRuntimeUnavailable}}
	runtimeClient := newTestSessionRuntimeClient(reads, controls)
	reactivator := newRuntimeReactivator()
	reactivationStarted := make(chan struct{})
	reactivator.SetReactivateFunc(func(ctx context.Context) error {
		close(reactivationStarted)
		<-ctx.Done()
		return ctx.Err()
	})
	runtimeClient.SetRuntimeReactivator(reactivator)

	start := time.Now()
	if _, err := runtimeClient.refreshMainViewSync(uiRuntimeReadTimeout); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("refreshMainViewSync error = %v, want reactivation deadline error", err)
	}
	if elapsed := time.Since(start); elapsed > uiRuntimeReadTimeout+500*time.Millisecond {
		t.Fatalf("refreshMainViewSync elapsed = %s, want bounded by read timeout %s", elapsed, uiRuntimeReadTimeout)
	}
	select {
	case <-reactivationStarted:
	case <-time.After(time.Second):
		t.Fatal("reactivation did not start")
	}
}

func deletedTestRuntimeClientShowGoalRecoversRuntimeUnavailableSilently(t *testing.T) {
	goal := &runtimepb.Goal{Id: "goal-1", Objective: "ship", Status: runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE}
	controls := &reconnectRetryRuntimeControlClient{
		showGoalErr:  serverapi.ErrRuntimeUnavailable,
		showGoalResp: runtimeClientTestShowResponse(goal),
	}
	runtimeClient := newTestSessionRuntimeClientWithControls(controls)
	reactivator := newRuntimeReactivator()
	recoveryCalls := 0
	reactivator.SetReactivateFunc(func(context.Context) error {
		recoveryCalls++
		return nil
	})
	runtimeClient.SetRuntimeReactivator(reactivator)

	got, err := runtimeClient.ShowGoal()
	if err != nil {
		t.Fatalf("ShowGoal: %v", err)
	}
	if recoveryCalls != 1 {
		t.Fatalf("recovery call count = %d, want 1", recoveryCalls)
	}
	if controls.showGoalCalls != 2 {
		t.Fatalf("show goal call count = %d, want 2", controls.showGoalCalls)
	}
	if got == nil || got.Goal == nil ||
		got.Goal.Id != "goal-1" ||
		got.Goal.Objective != "ship" ||
		got.Goal.Status != runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE {
		t.Fatalf("goal = %+v, want recovered active goal", got)
	}
	if entries := controls.appendedLocalEntries(); len(entries) != 0 {
		t.Fatalf("did not expect visible recovery warning during goal read, got %+v", entries)
	}
}

func deletedTestRuntimeClientReconnectWarningFailureDoesNotBlockSubmit(t *testing.T) {
	controls := &reconnectRetryRuntimeControlClient{firstSubmitErr: serverapi.ErrRuntimeUnavailable, appendErr: serverapi.ErrRuntimeUnavailable}
	runtimeClient := newTestSessionRuntimeClientWithControls(controls)
	warnings := make(chan runtimeReconnectWarningMsg, 1)
	runtimeClient.SetRuntimeReconnectWarningObserver(func(text string, visibility clientui.EntryVisibility) {
		warnings <- runtimeReconnectWarningMsg{text: text, visibility: visibility}
	})
	reactivator := newRuntimeReactivator()
	reactivator.SetReactivateFunc(func(context.Context) error { return nil })
	runtimeClient.SetRuntimeReactivator(reactivator)

	submission, err := runtimeClient.SubmitRuntimeInput(context.Background(), clientui.RuntimeSubmitRequest{
		Input: runtimeinput.Text("hello"),
	})
	message := ""
	if submission.Message != nil {
		message = *submission.Message
	}
	if err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}
	if message != "recovered" {
		t.Fatalf("SubmitUserMessage message = %q, want recovered", message)
	}
	if entries := controls.appendedLocalEntries(); len(entries) != 1 {
		t.Fatalf("warning append attempts = %d, want 1", len(entries))
	}
	select {
	case warning := <-warnings:
		if warning.visibility != clientui.EntryVisibilityOngoing {
			t.Fatalf("warning = %+v, want lease recovery warning", warning)
		}
	default:
		t.Fatal("expected warning fallback notification")
	}
}
