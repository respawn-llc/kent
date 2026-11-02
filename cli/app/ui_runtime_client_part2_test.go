package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"core/shared/apicontract"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/serverapi"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRuntimeClientMainViewDoesNotRefreshCachedSnapshotBehindUIBack(t *testing.T) {
	reads := &countingSessionViewClient{view: &runtimepb.MainView{Session: &runtimepb.SessionView{SessionId: detailTestSessionID}, Status: &runtimepb.Status{}}}
	runtimeClient := newTestSessionRuntimeClient(reads)
	runtimeClient.storeMainView(&runtimepb.MainView{Session: &runtimepb.SessionView{SessionId: detailTestSessionID}, Status: &runtimepb.Status{}})
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
	apicontract.RuntimeLiveControlService
	stopStatus runtimepb.LiveStopStatus
	stopReq    *runtimepb.LiveStopRequest
	stopCalls  int
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

func (c *reconnectRetryRuntimeControlClient) SubmitUserTurn(_ context.Context, req *runtimepb.SubmitUserTurnRequest) (*runtimepb.SubmitUserTurnSuccess, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.submitCalls++
	if c.submitCalls == 1 && c.firstSubmitErr != nil {
		return &runtimepb.SubmitUserTurnSuccess{}, c.firstSubmitErr
	}
	return &runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_AssistantFinal{AssistantFinal: &runtimepb.SubmitUserTurnAssistantFinal{Message: "recovered"}}}, nil
}

func (c *reconnectRetryRuntimeControlClient) CompactContext(context.Context, *runtimepb.CompactContextRequest) error {
	return nil
}

func (c *reconnectRetryRuntimeControlClient) LiveStop(_ context.Context, req *runtimepb.LiveStopRequest) (*runtimepb.LiveStopSuccess, error) {
	c.mu.Lock()
	c.stopReq = req
	c.stopCalls++
	c.mu.Unlock()
	return &runtimepb.LiveStopSuccess{Status: c.stopStatus}, nil
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
		Session: &chatpb.ExistingSessionTarget{SessionId: request.GetTarget().GetSession().GetSessionId().GetSessionId()},
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

func TestRuntimeClientStopTargetsSessionWithoutCommittingRuntimeTuple(t *testing.T) {
	current := runtimeTupleTestView(
		10,
		runtimeTupleTestIdleActivity(),
	)
	controls := &reconnectRetryRuntimeControlClient{stopStatus: runtimepb.LiveStopStatus_RUNTIME_LIVE_STOP_STATUS_STOPPED}
	runtimeClient := newTestSessionRuntimeClient(&countingSessionViewClient{})
	runtimeClient.storeMainView(current)

	model := newProjectedTestUIModel(runtimeClient, WithUISessionID(ongoingTestSessionID().String()))
	model.sessionLiveControls = controls
	if result := model.runtimeControlCommand(runtimeControlInterrupt, nil)().(runtimeControlDoneMsg); result.err != nil {
		t.Fatalf("interrupt: %v", result.err)
	}
	if controls.stopReq.GetSessionId().GetSessionId() != ongoingTestSessionID().String() {
		t.Fatalf("Stop targeted %v, want current Session", controls.stopReq)
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
	runtimeClient := newTestSessionRuntimeClient(&countingSessionViewClient{})
	runtimeClient.storeMainView(&runtimepb.MainView{Session: &runtimepb.SessionView{SessionId: detailTestSessionID}, Status: &runtimepb.Status{}})

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
	runtimeClient := newTestSessionRuntimeClient(&countingSessionViewClient{})
	runtimeClient.storeMainView(&runtimepb.MainView{
		Session: &runtimepb.SessionView{SessionId: detailTestSessionID},
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

func TestRuntimeClientMainViewRefreshDoesNotActivateUnavailableRuntime(t *testing.T) {
	controls := &reconnectRetryRuntimeControlClient{}
	authoritativeView := &runtimepb.MainView{
		Session: &runtimepb.SessionView{SessionId: detailTestSessionID, SessionName: proto.String("restored")},
		Status:  &runtimepb.Status{ThinkingLevel: "high"},
	}
	reads := &flakySessionViewClient{
		errs:      []error{serverapi.ErrRuntimeUnavailable, nil},
		responses: []*sessionpb.MainViewSuccess{{}, {MainView: authoritativeView}},
	}
	runtimeClient := newTestSessionRuntimeClient(reads)
	if _, err := runtimeClient.RefreshMainView(); !errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		t.Fatalf("RefreshMainView error = %v, want unavailable", err)
	}
	if reads.count != 1 {
		t.Fatalf("main-view read count = %d, want one read without replay", reads.count)
	}
	view, err := runtimeClient.RefreshMainView()
	if err != nil {
		t.Fatal(err)
	}
	if view.Session.GetSessionName() != "restored" || view.Status.ThinkingLevel != "high" {
		t.Fatalf("main view = %+v, want %+v", view, authoritativeView)
	}
	if entries := controls.appendedLocalEntries(); len(entries) != 0 {
		t.Fatalf("did not expect visible recovery warning during main-view refresh, got %+v", entries)
	}
}
