package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"core/shared/apicontract"
	"core/shared/client"
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
	rejection         *chatpb.InputNotAccepted
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
	return c.inputResult(request.Target, request.QueueItemId, chatpb.InputAdmissionDisposition_INPUT_ADMISSION_DISPOSITION_PENDING_STEER)
}

func (c *shellPreparationClient) inputResult(target *chatpb.ChatTarget, queueItemID string, disposition chatpb.InputAdmissionDisposition) (*chatpb.InputMutationSuccess, error) {
	if c.inputError != nil {
		return nil, c.inputError
	}
	var id string
	if c.selectedSessionID != nil {
		id = *c.selectedSessionID
	} else {
		id = target.GetSession().SessionId.GetSessionId()
	}
	if c.rejection != nil {
		return &chatpb.InputMutationSuccess{
			Session: &chatpb.ExistingSessionTarget{SessionId: id},
			Outcome: &chatpb.InputMutationSuccess_NotAccepted{NotAccepted: c.rejection},
		}, nil
	}
	return &chatpb.InputMutationSuccess{
		Session: &chatpb.ExistingSessionTarget{SessionId: id},
		Outcome: &chatpb.InputMutationSuccess_Accepted{Accepted: &chatpb.InputAccepted{
			QueueItem:   &chatpb.QueueItemIdentity{Id: queueItemID},
			Disposition: disposition,
			Diagnostic:  c.diagnostic,
		}},
	}, nil
}

func (c *shellPreparationClient) Queue(_ context.Context, request *chatpb.QueueRequest) (*chatpb.InputMutationSuccess, error) {
	c.queues = append(c.queues, request)
	return c.inputResult(request.Target, request.QueueItemId, chatpb.InputAdmissionDisposition_INPUT_ADMISSION_DISPOSITION_PENDING_QUEUE)
}

func TestNewSessionShellAdmissionPreservesDraftAndAdoptsCreatedSession(t *testing.T) {
	for _, intent := range []newSessionInputIntent{newSessionInputEnter, newSessionInputQueue} {
		for _, rejected := range []bool{false, true} {
			model := newProjectedStaticUIModel()
			model.presentationTarget = &uiNewSessionTarget{
				creation: &chatpb.NewChatTarget{}, settingsLoad: uiNewSettingsReady{},
			}
			id := runtimeids.NewSessionID().String()
			service := &shellPreparationClient{selectedSessionID: &id}
			if rejected {
				service.rejection = &chatpb.InputNotAccepted{Reason: &chatpb.InputNotAccepted_RuntimeUnavailable{
					RuntimeUnavailable: &chatpb.RuntimeUnavailableDetails{},
				}}
			}
			model.chatMutations = service
			submitted := "  $echo ready  "
			testSetMainInput(model, "newer draft")
			done := model.newSessionInputCmd(submitted, runtimeinput.Text(submitted), intent)().(newSessionActionDoneMsg)
			var activation *chatpb.Activation
			var draft *string
			if intent == newSessionInputQueue {
				if len(service.queues) != 1 || len(service.inputs) != 0 {
					t.Fatal("Shell Queue used the wrong admission operation")
				}
				activation, draft = service.queues[0].Activation, service.queues[0].InitialInputDraft
			} else {
				if len(service.inputs) != 1 || len(service.queues) != 0 {
					t.Fatal("Shell Enter used the wrong admission operation")
				}
				activation, draft = service.inputs[0].Activation, service.inputs[0].InitialInputDraft
			}
			if activation.GetShell().GetCommand() != "echo ready" || draft == nil || *draft != submitted {
				t.Fatalf("Shell activation or captured draft changed: %v, %v", activation, draft)
			}
			model.Update(done)
			if model.currentRuntimeSessionID() != id || model.newSessionTarget() != nil {
				t.Fatal("Shell admission lost the created Session")
			}
			wantDraft := "newer draft"
			if rejected {
				wantDraft += "\n\n" + submitted
			}
			if (done.err != nil) != rejected || model.mainEditor.Text() != wantDraft {
				t.Fatalf("Shell result lost rejection or draft: err=%v draft=%q", done.err, model.mainEditor.Text())
			}
		}
	}
}

func TestRuntimeClientConsumesLaunchIntentOnFailedFirstDispatch(t *testing.T) {
	model := newProjectedStaticUIModel()
	model.presentationTarget = uiKnownSessionTarget{id: ongoingTestSessionID(),
		preparation: &sessionlaunchpb.SessionRuntimePreparationOptions{ThinkingOverrideExplicit: true}}
	chat := &shellPreparationClient{inputError: serverapi.ErrRuntimeUnavailable}
	model.chatMutations = chat
	activation, err := chatActivationFromInput(runtimeinput.Shell("pwd"))
	if err != nil {
		t.Fatal(err)
	}
	if result := model.captureChatAction(chatInputAction{input: activation, queueItemID: runtimeids.NewQueueItemID()}, nil)(); !errors.Is(result.err, serverapi.ErrRuntimeUnavailable) {
		t.Fatalf("first shell error = %v", result.err)
	}
	if result := model.captureChatAction(chatCompactionAction{invocation: chatCompactionInvocation(nil)}, nil)(); result.err != nil {
		t.Fatal(result.err)
	}
	if len(chat.inputs) != 1 || len(chat.compactions) != 1 ||
		chat.inputs[0].Target.GetSession().Preparation == nil ||
		chat.compactions[0].Target.GetSession().Preparation != nil {
		t.Fatalf("launch choices must be consumed even when first dispatch fails: %+v", chat.inputs)
	}
}

type goalRuntimeControlClient struct {
	apicontract.RuntimeControlService
	apicontract.ChatMutationService
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
	for _, action := range []goalRuntimeOperation{goalRuntimeShow, goalRuntimeSet, goalRuntimePause, goalRuntimeResume, goalRuntimeComplete, goalRuntimeClear} {
		t.Run(string(action), func(t *testing.T) {
			controls := &goalRuntimeControlClient{t: t, cause: context.DeadlineExceeded}
			model := newProjectedStaticUIModel()
			model.setKnownSessionTarget(ongoingTestSessionID().String())
			model.sessionControls = controls
			model.chatMutations = controls
			result := model.goalRuntimeCommand(action, "finish the task")().(goalRuntimeDoneMsg)
			err := result.err
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
	model := newProjectedStaticUIModel()
	model.setKnownSessionTarget(ongoingTestSessionID().String())
	model.sessionControls = controls
	err := model.goalRuntimeCommand(goalRuntimeShow, "")().(goalRuntimeDoneMsg).err
	if !errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		t.Fatalf("ShowGoal error = %v, want unavailable", err)
	}
	if controls.showGoalCalls != 1 {
		t.Fatalf("ShowGoal calls = %d, want one read without replay", controls.showGoalCalls)
	}
}

func TestRuntimeClientInputMakesOneExplicitCall(t *testing.T) {
	model := newProjectedStaticUIModel()
	model.setKnownSessionTarget(ongoingTestSessionID().String())
	selected := runtimeids.NewSessionID().String()
	chat := &shellPreparationClient{selectedSessionID: &selected, diagnostic: &chatpb.AcceptedDiagnostic{
		Detail: &chatpb.AcceptedDiagnostic_PromptHistoryFailure{PromptHistoryFailure: &sharedpb.InternalFailureDetails{}},
	}}
	model.chatMutations = chat
	activation, err := chatActivationFromInput(runtimeinput.Text("hello"))
	if err != nil {
		t.Fatal(err)
	}
	submission, err := model.captureChatAction(chatInputAction{input: activation, queueItemID: runtimeids.NewQueueItemID()}, nil)().submission("hello")
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
	compaction := model.captureChatAction(chatCompactionAction{invocation: chatCompactionInvocation(nil)}, nil)()
	if compaction.err != nil || compaction.diagnostic == nil {
		t.Fatalf("accepted compaction diagnostic = %+v, error = %v", compaction, compaction.err)
	}
}

func TestRuntimeClientQueuesTypedInputWithCallerIdentity(t *testing.T) {
	for _, input := range []runtimeinput.Input{
		runtimeinput.Text("text"), runtimeinput.Command("prompt:check", "arguments"), runtimeinput.Shell("echo ready"),
	} {
		t.Run(string(input.Kind), func(t *testing.T) {
			model := newProjectedStaticUIModel()
			model.setKnownSessionTarget(ongoingTestSessionID().String())
			chat := &shellPreparationClient{}
			model.chatMutations = chat
			id := runtimeids.NewQueueItemID()
			inputActivation, err := chatActivationFromInput(input)
			if err != nil {
				t.Fatal(err)
			}
			result, err := model.captureChatAction(chatInputAction{
				input: inputActivation, intent: newSessionInputQueue, queueItemID: id,
			}, nil)().submission(input.CanonicalHistoryText())
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
