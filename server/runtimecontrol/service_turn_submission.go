package runtimecontrol

import (
	"context"
	"errors"
	"strings"

	"core/server/agentruntime"
	"core/server/promptcommands"
	"core/server/sessionstore"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/serverapi"
)

type PreparedUserTurn struct {
	QueueItemID   string
	Placement     promptcommands.Placement
	ExecutionText string
	HistoryText   string
}

func (p PreparedUserTurn) queuedInput() runtime.QueuedUserInput {
	return runtime.QueuedUserInput{
		ID:                    p.QueueItemID,
		ExecutionText:         p.ExecutionText,
		CanonicalPresentation: p.HistoryText,
	}
}

func queuedUserTurnResponse(queueItemID string) *runtimepb.SubmitUserTurnSuccess {
	return &runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_Queued{
		Queued: &runtimepb.SubmitUserTurnQueued{Steered: true, QueueItemId: queueItemID},
	}}
}

func (s *Service) PrepareUserTurn(ctx context.Context, sessionID string, input runtimeinput.Input) (PreparedUserTurn, error) {
	if input.Kind == runtimeinput.KindPromptCommand && (s == nil || s.promptCommands == nil) {
		return PreparedUserTurn{}, errors.New("prompt command resolver is required")
	}
	placement := promptcommands.PlacementCurrent
	execution, err := input.ExecutionText(func(command runtimeinput.PromptCommand) (string, error) {
		resolved, err := s.promptCommands.ResolvePromptCommand(ctx, sessionID, command.Name, command.Arguments)
		placement = resolved.Placement
		return resolved.Text, err
	})
	if err != nil {
		return PreparedUserTurn{}, err
	}
	history := input.CanonicalHistoryText()
	return PreparedUserTurn{Placement: placement, ExecutionText: execution, HistoryText: history}, nil
}

func (s *Service) SubmitUserTurn(ctx context.Context, req *runtimepb.SubmitUserTurnRequest) (*runtimepb.SubmitUserTurnSuccess, error) {
	return runRuntimeCommand(ctx, func(ctx context.Context) (*runtimepb.SubmitUserTurnSuccess, bool, error) {
		input := protoapi.UserTurnInputFromProto(req.Input)
		prepared, err := s.PrepareUserTurn(ctx, req.SessionId, input)
		if err != nil {
			return nil, false, err
		}
		prepared.QueueItemID = req.QueueItemId
		response, admission, err := s.admitUserTurn(ctx, req.SessionId, prepared)
		return response, admission.Accepted, err
	})
}

func (s *Service) AdmitChatUserTurn(
	ctx context.Context,
	sessionID string,
	prepared PreparedUserTurn,
) (serverapi.ChatInputAdmissionResult, error) {
	_, admission, err := s.admitUserTurn(ctx, sessionID, prepared)
	return admission, err
}

func (s *Service) admitUserTurn(
	ctx context.Context,
	sessionID string,
	projection PreparedUserTurn,
) (*runtimepb.SubmitUserTurnSuccess, serverapi.ChatInputAdmissionResult, error) {
	sessionID = strings.TrimSpace(sessionID)
	attempt := newRuntimeCommandAttempt(ctx)
	defer attempt.Finish()
	response, queued, commandErr := s.submitUserTurn(attempt, sessionID, projection)
	accepted := attempt.Accepted()
	var admission serverapi.ChatInputAdmissionResult
	if accepted {
		queueItemID, parseErr := runtimeids.ParseQueueItemID(queued.ID)
		admission = serverapi.ChatInputAdmissionResult{
			QueueItemID:          queueItemID,
			Disposition:          queued.Disposition,
			Accepted:             true,
			PromptHistoryFailure: s.recordAcceptedUserTurnHistory(sessionID, projection),
		}
		commandErr = errors.Join(commandErr, parseErr)
	}
	return response, admission, commandErr
}

func (s *Service) submitUserTurn(
	attempt *runtimeCommandAttempt,
	sessionIDText string,
	projection PreparedUserTurn,
) (*runtimepb.SubmitUserTurnSuccess, runtime.QueuedUserMessage, error) {
	var response *runtimepb.SubmitUserTurnSuccess
	var admitted runtime.QueuedUserMessage
	sessionID := runtimeids.SessionIDFromProto(sessionIDText)
	if s == nil || s.authority == nil {
		return nil, admitted, errors.New("session runtime authority is required")
	}
	descriptor, err := session.NewOpenSessionDescriptor(sessionID)
	if err != nil {
		return nil, admitted, err
	}
	admitted, err = s.authority.SubmitAgentInput(attempt.Context(), descriptor, projection.queuedInput(), attempt.Accept)
	if err == nil {
		response = queuedUserTurnResponse(admitted.ID)
	}
	return response, admitted, err
}

func (s *Service) recordAcceptedUserTurnHistory(
	sessionID string,
	projection PreparedUserTurn,
) error {
	if _, err := s.recordPromptHistory(context.Background(), sessionID, projection.HistoryText); err != nil {
		reportErr := s.withRuntime(context.Background(), sessionID, func(_ context.Context, engine *runtime.Engine) error {
			engine.ReportPromptHistoryPersistError(err.Error())
			return nil
		})
		return errors.Join(err, reportErr)
	}
	return nil
}
