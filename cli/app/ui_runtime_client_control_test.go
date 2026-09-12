package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"core/shared/apicontract"
	"core/shared/client"
	"core/shared/clientui"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeinput"
	"core/shared/serverapi"

	"google.golang.org/protobuf/proto"
)

type goalRuntimeControlClient struct {
	apicontract.RuntimeControlService
	t     *testing.T
	cause error
}

func (c *goalRuntimeControlClient) respond(ctx context.Context) error {
	c.t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok {
		c.t.Fatal("goal request has no deadline")
	}
	if remaining := time.Until(deadline); remaining < 14*time.Second || remaining > 15*time.Second {
		c.t.Fatalf("goal request budget = %s, want fifteen seconds", remaining)
	}
	return c.cause
}

func (c *goalRuntimeControlClient) ShowGoal(ctx context.Context, _ *runtimepb.GoalShowRequest) (*runtimepb.GoalShowSuccess, error) {
	return &runtimepb.GoalShowSuccess{}, c.respond(ctx)
}

func (c *goalRuntimeControlClient) SetGoal(ctx context.Context, _ *runtimepb.GoalSetRequest) (*runtimepb.GoalMutationSuccess, error) {
	return &runtimepb.GoalMutationSuccess{}, c.respond(ctx)
}

func (c *goalRuntimeControlClient) PauseGoal(ctx context.Context, _ *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error) {
	return &runtimepb.GoalMutationSuccess{}, c.respond(ctx)
}

func (c *goalRuntimeControlClient) ResumeGoal(ctx context.Context, _ *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error) {
	return &runtimepb.GoalMutationSuccess{}, c.respond(ctx)
}

func (c *goalRuntimeControlClient) CompleteGoal(ctx context.Context, _ *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error) {
	return &runtimepb.GoalMutationSuccess{}, c.respond(ctx)
}

func (c *goalRuntimeControlClient) ClearGoal(ctx context.Context, _ *runtimepb.GoalClearRequest) (*runtimepb.GoalMutationSuccess, error) {
	return &runtimepb.GoalMutationSuccess{}, c.respond(ctx)
}

func TestRuntimeGoalCallsHaveGoalBudgetAndPresentTimeout(t *testing.T) {
	for _, action := range []string{"show", "set", "pause", "resume", "complete", "clear"} {
		t.Run(action, func(t *testing.T) {
			controls := &goalRuntimeControlClient{t: t, cause: context.DeadlineExceeded}
			runtimeClient := newUIRuntimeClientWithReads("session-1", &countingSessionViewClient{}, controls, nil)
			var err error
			switch action {
			case "show":
				_, err = runtimeClient.ShowGoal()
			case "set":
				_, err = runtimeClient.SetGoal("finish the task")
			case "pause":
				_, err = runtimeClient.PauseGoal()
			case "resume":
				_, err = runtimeClient.ResumeGoal()
			case "complete":
				_, err = runtimeClient.CompleteGoal()
			case "clear":
				_, err = runtimeClient.ClearGoal()
			}
			var presented client.GoalRequestTimeoutError
			if !errors.Is(err, controls.cause) || !errors.As(err, &presented) {
				t.Fatalf("goal timeout needs presentation with its diagnostic cause preserved: %T", err)
			}
		})
	}
}

func TestRuntimeGoalReadRecoversUnavailableConnection(t *testing.T) {
	goal := &runtimepb.Goal{Id: "goal-1", Objective: "finish the task", Status: runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE}
	controls := &reconnectRetryRuntimeControlClient{
		showGoalErr:  serverapi.ErrRuntimeUnavailable,
		showGoalResp: runtimeClientTestShowResponse(goal),
	}
	runtimeClient := newTestSessionRuntimeClientWithControls(controls)
	reactivator := newRuntimeReactivator()
	recovered := false
	reactivator.SetReactivateFunc(func(context.Context) error {
		recovered = true
		return nil
	})
	runtimeClient.SetRuntimeReactivator(reactivator)
	result, err := runtimeClient.ShowGoal()
	if err != nil {
		t.Fatalf("show goal after connection recovery: %v", err)
	}
	if !recovered || result == nil || result.Goal == nil || !proto.Equal(result.Goal, goal) {
		t.Fatalf("goal was not restored after connection recovery: %+v", result)
	}
}

func TestRuntimeClientInputMakesOneExplicitCall(t *testing.T) {
	controls := &reconnectRetryRuntimeControlClient{}
	runtimeClient := newUIRuntimeClientWithReads("session-1", &countingSessionViewClient{}, controls, nil).(*sessionRuntimeClient)

	if _, err := runtimeClient.SubmitRuntimeInput(context.Background(), clientui.RuntimeSubmitRequest{
		Input: runtimeinput.Text("hello"),
	}); err != nil {
		t.Fatalf("SubmitRuntimeInput: %v", err)
	}
	if controls.submitCalls != 1 {
		t.Fatalf("submit calls = %d, want 1", controls.submitCalls)
	}
}

func TestRuntimeClientProjectsUserTurnOutcomes(t *testing.T) {
	tests := []struct {
		name     string
		response *runtimepb.SubmitUserTurnSuccess
		kind     clientui.UserTurnResultKind
		message  *string
		queued   clientui.QueuedUserMessage
	}{
		{
			name:     "steered queue",
			response: &runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_Queued{Queued: &runtimepb.SubmitUserTurnQueued{QueueItemId: "queue-1", Steered: true}}},
			kind:     clientui.UserTurnResultKindQueued,
			queued:   clientui.QueuedUserMessage{ID: "queue-1", Text: "input"},
		},
		{
			name:     "admitted queue",
			response: &runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_Queued{Queued: &runtimepb.SubmitUserTurnQueued{QueueItemId: "queue-1"}}},
			kind:     clientui.UserTurnResultKindQueued,
		},
		{
			name:     "no final",
			response: &runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_NoFinal{NoFinal: &runtimepb.SubmitUserTurnNoFinal{}}},
			kind:     clientui.UserTurnResultKindNoFinal,
		},
		{
			name:     "assistant final",
			response: &runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_AssistantFinal{AssistantFinal: &runtimepb.SubmitUserTurnAssistantFinal{Message: "answer"}}},
			kind:     clientui.UserTurnResultKindAssistantFinal,
			message:  proto.String("answer"),
		},
		{
			name:     "silent final",
			response: &runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_SilentFinal{SilentFinal: &runtimepb.SubmitUserTurnSilentFinal{Message: "answer"}}},
			kind:     clientui.UserTurnResultKindSilentFinal,
			message:  proto.String("answer"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := userTurnSubmissionFromResponse(test.response, "input")
			if got.ResultKind != test.kind || got.Queued != test.queued {
				t.Fatalf("submission = %+v", got)
			}
			if test.message == nil {
				if got.Message != nil {
					t.Fatalf("unexpected final message: %q", *got.Message)
				}
			} else if got.Message == nil || *got.Message != *test.message {
				t.Fatalf("final message = %v, want %q", got.Message, *test.message)
			}
		})
	}
}
