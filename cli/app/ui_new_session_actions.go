package app

import (
	"context"
	"errors"

	"core/shared/apicontract"
	"core/shared/client"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	chatshellpb "core/shared/protoapi/gen/kent/api/chat_shell"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeinput"
	"core/shared/textutil"

	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/proto"
)

type newSessionAction interface{ isNewSessionAction() }
type newSessionInputAction struct {
	input  *chatpb.Activation
	intent newSessionInputIntent
}
type newSessionShellAction struct{ command string }
type newSessionGoalAction struct{ objective string }
type newSessionCompactionAction struct {
	invocation *chatpb.CompactionInvocation
}

func (newSessionInputAction) isNewSessionAction()      {}
func (newSessionShellAction) isNewSessionAction()      {}
func (newSessionGoalAction) isNewSessionAction()       {}
func (newSessionCompactionAction) isNewSessionAction() {}

type newSessionActionDoneMsg struct {
	origin        *uiModel
	token         uint64
	action        newSessionAction
	submittedText *string
	session       *chatpb.ExistingSessionTarget
	err           error
	diagnostic    error
}

func (m *uiModel) newSessionInputCmd(text string, input runtimeinput.Input, intent newSessionInputIntent) tea.Cmd {
	if command, shell := parseUserShellCommand(text); shell {
		return m.newSessionActionCmd(newSessionShellAction{command: command}, &text)
	}
	activation, err := chatActivationFromInput(input)
	if err != nil {
		return m.sendTransientStatusWithNoticeID(err.Error(), uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
	}
	return m.newSessionActionCmd(newSessionInputAction{input: activation, intent: intent}, &text)
}

func (m *uiModel) newSessionActionCmd(action newSessionAction, submittedText *string) tea.Cmd {
	if m.newSessionTarget() != nil && !m.newSessionConnectionReady() {
		if submittedText != nil {
			m.inputController().restoreSubmittedTextIntoInput(*submittedText)
		}
		return nil
	}
	var target *chatpb.ChatTarget
	switch presentation := m.presentationTarget.(type) {
	case *uiNewSessionTarget:
		target = &chatpb.ChatTarget{Target: &chatpb.ChatTarget_NewChat{
			NewChat: proto.Clone(presentation.creation).(*chatpb.NewChatTarget),
		}}
	case uiKnownSessionTarget:
		target = &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{Session: &chatpb.ExistingSessionRequestTarget{
			SessionId: protoapi.SessionSelectorToProto(presentation.id),
		}}}
	default:
		panic("Chat action requires a presentation target")
	}
	service := m.chatMutations
	draft := m.mainEditor.Text()
	m.submitToken++
	m.pendingNewChatActions++
	result := newSessionActionDoneMsg{
		origin: m, token: m.submitToken, action: action, submittedText: textutil.Pointer(submittedText),
	}
	return func() tea.Msg {
		return executeNewSessionAction(context.Background(), service, target, draft, result)
	}
}

func (m *uiModel) usesDirectChatActions() bool {
	return m.newSessionTarget() != nil || (!m.hasRuntimeClient() && m.chatMutations != nil)
}

func executeNewSessionAction(ctx context.Context, service apicontract.ChatMutationService, target *chatpb.ChatTarget, draft string, result newSessionActionDoneMsg) newSessionActionDoneMsg {
	switch action := result.action.(type) {
	case newSessionInputAction:
		var response *chatpb.InputMutationSuccess
		if action.intent == newSessionInputQueue {
			response, result.err = service.Queue(ctx, &chatpb.QueueRequest{Target: target, Activation: action.input})
		} else {
			response, result.err = service.Steer(ctx, &chatpb.SteerRequest{Target: target, Activation: action.input})
		}
		if result.err == nil {
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
	case newSessionShellAction:
		response, err := service.SubmitUserShellCommand(ctx, &chatshellpb.ShellCommandRequest{
			Target: target, Command: action.command, InitialInputDraft: result.submittedText,
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
	case newSessionGoalAction:
		response, err := service.SetGoal(ctx, &runtimepb.GoalSetRequest{
			Target: target, Objective: action.objective, Actor: "user",
			ExecutionPolicy:   runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_PRESERVE_RUNTIME_STATE,
			InitialInputDraft: &draft,
		})
		result.err = err
		if err == nil {
			result.err = protoapi.Validate(response)
		}
		if result.err == nil {
			result.session = response.Session
			if rejected := response.GetRejected(); rejected != nil {
				result.err = client.GoalSetErrorAsError(rejected)
			}
			if response.Diagnostic != nil {
				result.diagnostic = client.GoalSetErrorAsError(response.Diagnostic)
			}
		}
	case newSessionCompactionAction:
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
