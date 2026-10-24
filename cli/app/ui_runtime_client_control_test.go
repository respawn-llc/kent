package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"core/shared/apicontract"
	"core/shared/client"
	"core/shared/clientui"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/serverapi"
)

type shellPreparationClient struct {
	apicontract.ChatMutationService
	inputs            []*chatpb.SteerRequest
	queues            []*chatpb.QueueRequest
	inputError        error
	compactions       []*chatpb.CompactRequest
	selectedSessionID *string
	diagnostic        *chatpb.AcceptedDiagnostic
}

func (c *shellPreparationClient) Compact(_ context.Context, request *chatpb.CompactRequest) (*chatpb.CompactionMutationSuccess, error) {
	c.compactions = append(c.compactions, request)
	return &chatpb.CompactionMutationSuccess{
		Session: &chatpb.ExistingSessionTarget{SessionId: request.Target.GetSession().SessionId.GetSessionId()},
		Outcome: &chatpb.CompactionMutationSuccess_Accepted{Accepted: &chatpb.CompactionAccepted{Diagnostic: c.diagnostic}},
	}, nil
}

func (c *shellPreparationClient) Steer(_ context.Context, request *chatpb.SteerRequest) (*chatpb.InputMutationSuccess, error) {
	c.inputs = append(c.inputs, request)
	if c.inputError != nil {
		return nil, c.inputError
	}
	id := request.Target.GetSession().SessionId.GetSessionId()
	if c.selectedSessionID != nil {
		id = *c.selectedSessionID
	}
	return &chatpb.InputMutationSuccess{
		Session: &chatpb.ExistingSessionTarget{SessionId: id},
		Outcome: &chatpb.InputMutationSuccess_Accepted{Accepted: &chatpb.InputAccepted{
			QueueItem:   &chatpb.QueueItemIdentity{Id: request.QueueItemId},
			Disposition: chatpb.InputAdmissionDisposition_INPUT_ADMISSION_DISPOSITION_PENDING_STEER,
			Diagnostic:  c.diagnostic,
		}},
	}, nil
}

func (c *shellPreparationClient) Queue(ctx context.Context, request *chatpb.QueueRequest) (*chatpb.InputMutationSuccess, error) {
	c.queues = append(c.queues, request)
	return &chatpb.InputMutationSuccess{
		Session: &chatpb.ExistingSessionTarget{SessionId: request.Target.GetSession().SessionId.GetSessionId()},
		Outcome: &chatpb.InputMutationSuccess_Accepted{Accepted: &chatpb.InputAccepted{
			QueueItem:   &chatpb.QueueItemIdentity{Id: request.QueueItemId},
			Disposition: chatpb.InputAdmissionDisposition_INPUT_ADMISSION_DISPOSITION_PENDING_QUEUE,
		}},
	}, c.inputError
}

func TestRuntimeClientConsumesLaunchIntentOnFailedFirstDispatch(t *testing.T) {
	client := newTestSessionRuntimeClientWithControls(&reconnectRetryRuntimeControlClient{})
	client.sessionID = ongoingTestSessionID().String()
	chat := &shellPreparationClient{inputError: serverapi.ErrRuntimeUnavailable}
	client.chatMutations = chat
	client.preparation = &sessionlaunchpb.SessionRuntimePreparationOptions{ThinkingOverrideExplicit: true}
	request := clientui.RuntimeSubmitRequest{Input: runtimeinput.Shell("pwd"), Intent: runtimeinput.PendingWorkLaneSteer, QueueItemID: runtimeids.NewQueueItemID()}
	if _, err := client.SubmitRuntimeInput(t.Context(), request); !errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		t.Fatalf("first shell error = %v", err)
	}
	if _, err := client.CompactRuntime(t.Context(), clientui.RuntimeCompactRequest{}); err != nil {
		t.Fatal(err)
	}
	if len(chat.inputs) != 1 || len(chat.compactions) != 1 ||
		chat.inputs[0].Target.GetSession().Preparation == nil ||
		chat.compactions[0].Target.GetSession().Preparation != nil {
		t.Fatalf("launch choices must be consumed even when first dispatch fails: %+v", chat.inputs)
	}
}

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

func (c *goalRuntimeControlClient) SetGoal(ctx context.Context, _ *runtimepb.GoalSetRequest) (*runtimepb.GoalSetSuccess, error) {
	return &runtimepb.GoalSetSuccess{}, c.respond(ctx)
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
			runtimeClient := newUIRuntimeClientWithReads("11111111-1111-4111-8111-111111111111", &countingSessionViewClient{}, controls, controls, nil)
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

func TestRuntimeGoalReadDoesNotActivateUnavailableRuntime(t *testing.T) {
	goal := &runtimepb.Goal{Id: "goal-1", Objective: "finish the task", Status: runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE}
	controls := &reconnectRetryRuntimeControlClient{
		showGoalErr:  serverapi.ErrRuntimeUnavailable,
		showGoalResp: runtimeClientTestShowResponse(goal),
	}
	runtimeClient := newTestSessionRuntimeClientWithControls(controls)
	_, err := runtimeClient.ShowGoal()
	if !errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		t.Fatalf("ShowGoal error = %v, want unavailable", err)
	}
	if controls.showGoalCalls != 1 {
		t.Fatalf("ShowGoal calls = %d, want one read without replay", controls.showGoalCalls)
	}
}

func TestRuntimeClientInputMakesOneExplicitCall(t *testing.T) {
	controls := &reconnectRetryRuntimeControlClient{}
	runtimeClient := newUIRuntimeClientWithReads(ongoingTestSessionID().String(), &countingSessionViewClient{}, controls, controls, nil).(*sessionRuntimeClient)
	selected := runtimeids.NewSessionID().String()
	chat := &shellPreparationClient{selectedSessionID: &selected, diagnostic: &chatpb.AcceptedDiagnostic{
		Detail: &chatpb.AcceptedDiagnostic_PromptHistoryFailure{PromptHistoryFailure: &sharedpb.InternalFailureDetails{}},
	}}
	runtimeClient.chatMutations = chat

	submission, err := runtimeClient.SubmitRuntimeInput(context.Background(), clientui.RuntimeSubmitRequest{
		Input:  runtimeinput.Text("hello"),
		Intent: runtimeinput.PendingWorkLaneSteer, QueueItemID: runtimeids.NewQueueItemID(),
	})
	if err != nil {
		t.Fatalf("SubmitRuntimeInput: %v", err)
	}
	if submission.SessionID == nil || *submission.SessionID != selected {
		t.Fatalf("selected Session = %v", submission.SessionID)
	}
	if submission.Diagnostic == nil || submission.Queued.ID == "" {
		t.Fatalf("accepted identity or diagnostic lost: %+v", submission)
	}
	if len(chat.inputs) != 1 {
		t.Fatalf("submit calls = %d, want 1", len(chat.inputs))
	}
	compaction, err := runtimeClient.CompactRuntime(t.Context(), clientui.RuntimeCompactRequest{})
	if err != nil || compaction.Diagnostic == nil {
		t.Fatalf("accepted compaction diagnostic = %+v, error = %v", compaction, err)
	}
}

func TestRuntimeClientQueuesTypedInputWithCallerIdentity(t *testing.T) {
	for _, input := range []runtimeinput.Input{
		runtimeinput.Text("text"), runtimeinput.Command("prompt:check", "arguments"), runtimeinput.Shell("echo ready"),
	} {
		t.Run(string(input.Kind), func(t *testing.T) {
			runtimeClient := newTestSessionRuntimeClientWithControls(&reconnectRetryRuntimeControlClient{})
			runtimeClient.sessionID = ongoingTestSessionID().String()
			chat := &shellPreparationClient{}
			runtimeClient.chatMutations = chat
			id := runtimeids.NewQueueItemID()
			result, err := runtimeClient.SubmitRuntimeInput(t.Context(), clientui.RuntimeSubmitRequest{
				Input: input, Intent: runtimeinput.PendingWorkLaneQueue, QueueItemID: id,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(chat.inputs) != 0 || len(chat.queues) != 1 || chat.queues[0].QueueItemId != id.String() || result.Queued.ID != id.String() {
				t.Fatalf("Queue admission = %+v", result)
			}
			activation := chat.queues[0].Activation
			switch input.Kind {
			case runtimeinput.KindText:
				if activation.GetText() != *input.Text {
					t.Fatal("ordinary input changed")
				}
			case runtimeinput.KindPromptCommand:
				if activation.GetCommand().CatalogIdentity != input.PromptCommand.Name {
					t.Fatal("prompt command lost its typed identity")
				}
			case runtimeinput.KindShell:
				if activation.GetShell().Command != input.Shell.Command {
					t.Fatal("Shell command became model input")
				}
			}
		})
	}
}
