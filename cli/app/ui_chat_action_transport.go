package app

import (
	"context"
	"errors"

	"core/shared/apicontract"
	"core/shared/client"
	"core/shared/clientui"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	chatshellpb "core/shared/protoapi/gen/kent/api/chat_shell"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"google.golang.org/protobuf/proto"
)

type chatAction interface{ isChatAction() }
type chatInputAction struct {
	input       *chatpb.Activation
	intent      newSessionInputIntent
	queueItemID runtimeids.QueueItemID
}
type chatShellAction struct{ command string }
type chatGoalAction struct{ objective string }
type chatCompactionAction struct {
	invocation *chatpb.CompactionInvocation
}

func (chatInputAction) isChatAction()      {}
func (chatShellAction) isChatAction()      {}
func (chatGoalAction) isChatAction()       {}
func (chatCompactionAction) isChatAction() {}

type chatActionTransport struct {
	actions apicontract.ChatMutationService
}

type chatActionResult struct {
	session    *chatpb.ExistingSessionTarget
	input      *chatpb.InputAccepted
	goal       *runtimepb.GoalSetSuccess
	err        error
	diagnostic error
}

func chatCompactionInvocation(guidance *string) *chatpb.CompactionInvocation {
	invocation := &chatpb.CompactionInvocation{Token: runtimeinput.ManualCompactionToken}
	if guidance != nil {
		invocation.SeparatorWhitespace = " "
		invocation.RawGuidance = *guidance
	}
	return invocation
}

func (r chatActionResult) submission(text string) (clientui.UserTurnSubmission, error) {
	if r.err != nil {
		return clientui.UserTurnSubmission{}, r.err
	}
	return clientui.UserTurnSubmission{
		Diagnostic: r.diagnostic, SessionID: &r.session.SessionId,
		ResultKind: clientui.UserTurnResultKindQueued,
		Queued:     clientui.QueuedUserMessage{ID: r.input.QueueItem.Id, Text: text},
	}, nil
}

func (m *uiModel) captureChatAction(action chatAction, submittedText *string) func() chatActionResult {
	var target *chatpb.ChatTarget
	var draft *string
	switch selected := m.presentationTarget.(type) {
	case *uiNewSessionTarget:
		target = &chatpb.ChatTarget{Target: &chatpb.ChatTarget_NewChat{NewChat: proto.Clone(selected.creation).(*chatpb.NewChatTarget)}}
		text := m.mainEditor.Text()
		if _, shell := action.(chatShellAction); shell && submittedText != nil {
			text = *submittedText
		}
		draft = &text
	case uiKnownSessionTarget:
		target = &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{Session: &chatpb.ExistingSessionRequestTarget{
			SessionId: protoapi.SessionSelectorToProto(selected.id), Preparation: selected.preparation,
		}}}
		selected.preparation = nil
		m.presentationTarget = selected
	default:
		panic("Chat action requires a selected target")
	}
	transport := chatActionTransport{actions: m.chatMutations}
	return func() chatActionResult { return transport.execute(context.Background(), target, draft, action) }
}

func (t chatActionTransport) execute(ctx context.Context, target *chatpb.ChatTarget, draft *string, action chatAction) chatActionResult {
	result := chatActionResult{}
	service := t.actions
	if target.GetNewChat() == nil {
		draft = nil
	}
	switch action := action.(type) {
	case chatInputAction:
		var response *chatpb.InputMutationSuccess
		if action.intent == newSessionInputQueue {
			response, result.err = service.Queue(ctx, &chatpb.QueueRequest{Target: target, Activation: action.input, QueueItemId: action.queueItemID.String()})
		} else {
			response, result.err = service.Steer(ctx, &chatpb.SteerRequest{Target: target, Activation: action.input, QueueItemId: action.queueItemID.String()})
		}
		if result.err == nil {
			result.err = protoapi.Validate(response)
		}
		if result.err == nil {
			result.session = response.Session
			if rejected := response.GetNotAccepted(); rejected != nil {
				result.err = protoapi.ChatNotAcceptedFromProto(rejected)
			} else {
				result.input = response.GetAccepted()
				result.diagnostic = protoapi.ChatAcceptedDiagnosticFromProto(response.GetAccepted().Diagnostic)
			}
		}
	case chatShellAction:
		response, err := service.SubmitUserShellCommand(ctx, &chatshellpb.ShellCommandRequest{
			Target: target, Command: action.command, InitialInputDraft: draft,
		})
		result.err = err
		if err == nil {
			result.err = protoapi.Validate(response)
		}
		if result.err == nil {
			result.session = response.Session
			if failure := response.GetFailed(); failure != nil {
				result.err = protoapi.ChatNotAcceptedFromProto(failure)
			}
		}
	case chatGoalAction:
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, client.GoalRequestTimeout)
		defer cancel()
		policy := runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_PRESERVE_RUNTIME_STATE
		if target.GetNewChat() != nil {
			policy = runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE
		} else {
			draft = nil
		}
		response, err := service.SetGoal(ctx, &runtimepb.GoalSetRequest{
			Target: target, Objective: action.objective, Actor: "user",
			ExecutionPolicy:   policy,
			InitialInputDraft: draft,
		})
		result.err = client.PresentGoalRequestError(err)
		if err == nil {
			result.err = protoapi.Validate(response)
		}
		if result.err == nil {
			result.session = response.Session
			if rejected := response.GetRejected(); rejected != nil {
				result.err = client.GoalSetErrorAsError(rejected)
			}
			result.goal = response
			if response.Diagnostic != nil {
				result.diagnostic = client.GoalSetErrorAsError(response.Diagnostic)
			}
		}
	case chatCompactionAction:
		response, err := service.Compact(ctx, &chatpb.CompactRequest{
			Target: target, Invocation: action.invocation,
		})
		result.err = err
		if err == nil {
			result.err = protoapi.Validate(response)
		}
		if result.err == nil {
			result.session = response.Session
			if rejected := response.GetNotAccepted(); rejected != nil {
				result.err = protoapi.ChatNotAcceptedFromProto(rejected)
			} else {
				result.diagnostic = protoapi.ChatAcceptedDiagnosticFromProto(response.GetAccepted().Diagnostic)
			}
		}
	default:
		result.err = errors.New("New Session action kind is required")
	}
	return result
}
