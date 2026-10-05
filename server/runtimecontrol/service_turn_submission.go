package runtimecontrol

import (
	"context"
	"errors"
	"strings"

	"core/server/promptcommands"
	"core/server/runtime"
	"core/server/session"
	"core/server/sessionruntime"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/serverapi"
)

type PreparedUserTurn struct {
	Input         runtimeinput.Input
	Placement     promptcommands.Placement
	ExecutionText string
	HistoryText   string
}

func (p PreparedUserTurn) queuedInput() runtime.QueuedUserInput {
	return runtime.QueuedUserInput{
		ExecutionText:         p.ExecutionText,
		CanonicalPresentation: p.HistoryText,
	}
}

func queuedUserTurnResponse(compacted bool, queueItemID string) *runtimepb.SubmitUserTurnSuccess {
	return &runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_Queued{
		Queued: &runtimepb.SubmitUserTurnQueued{Compacted: compacted, Steered: true, QueueItemId: queueItemID},
	}}
}

func canonicalUserTurnRequest(sessionID string, input runtimeinput.Input) sessionUserTurnRequest {
	request := sessionUserTurnRequest{
		SessionID: strings.TrimSpace(sessionID),
		Kind:      input.Kind,
	}
	if input.Text != nil {
		request.Text = *input.Text
	}
	if input.PromptCommand != nil {
		request.Name = strings.TrimSpace(input.PromptCommand.Name)
		request.Arguments = input.PromptCommand.Arguments
	}
	return request
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
	history, err := input.CanonicalHistoryText()
	if err != nil {
		return PreparedUserTurn{}, err
	}
	return PreparedUserTurn{Input: input, Placement: placement, ExecutionText: execution, HistoryText: history}, nil
}

func (s *Service) SubmitUserTurn(ctx context.Context, req *runtimepb.SubmitUserTurnRequest) (*runtimepb.SubmitUserTurnSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	return runRuntimeCommand(ctx, func(ctx context.Context) (*runtimepb.SubmitUserTurnSuccess, bool, error) {
		input, err := protoapi.UserTurnInputFromProto(req.Input)
		if err != nil {
			return nil, false, err
		}
		prepared, err := s.PrepareUserTurn(ctx, req.SessionId, input)
		if err != nil {
			return nil, false, err
		}
		response, accepted, _, err := s.admitUserTurn(ctx, req.SessionId, prepared)
		return response, accepted, err
	})
}

func (s *Service) AdmitChatUserTurn(
	ctx context.Context,
	sessionID string,
	prepared PreparedUserTurn,
) (serverapi.ChatInputAdmissionResult, error) {
	response, accepted, historyErr, commandErr := s.admitUserTurn(ctx, sessionID, prepared)
	if !accepted {
		return serverapi.ChatInputAdmissionResult{}, commandErr
	}
	queueItemID, parseErr := runtimeids.ParseQueueItemID(response.GetQueued().GetQueueItemId())
	return serverapi.ChatInputAdmissionResult{
		QueueItemID:          queueItemID,
		Accepted:             true,
		PromptHistoryFailure: historyErr,
	}, errors.Join(commandErr, parseErr)
}

func (s *Service) admitUserTurn(
	ctx context.Context,
	sessionID string,
	projection PreparedUserTurn,
) (*runtimepb.SubmitUserTurnSuccess, bool, error, error) {
	request := canonicalUserTurnRequest(sessionID, projection.Input)
	attempt := newRuntimeCommandAttempt(ctx)
	defer attempt.Finish()
	response, commandErr := s.submitUserTurn(attempt, request, projection)
	if commandErr == nil {
		commandErr = protoapi.Validate(response)
	}
	accepted := attempt.Accepted()
	var historyErr error
	if accepted {
		historyErr = s.recordAcceptedUserTurnHistory(request, projection)
	}
	return response, accepted, historyErr, commandErr
}

func (s *Service) submitUserTurn(
	attempt *runtimeCommandAttempt,
	request sessionUserTurnRequest,
	projection PreparedUserTurn,
) (*runtimepb.SubmitUserTurnSuccess, error) {
	var response *runtimepb.SubmitUserTurnSuccess
	sessionID, err := runtimeids.ParseSessionID(request.SessionID)
	if err != nil {
		return nil, err
	}
	if s == nil || s.authority == nil {
		return nil, errors.New("session runtime authority is required")
	}
	descriptor, err := session.NewOpenSessionDescriptor(sessionID)
	if err != nil {
		return nil, err
	}
	runTurn := func(runCtx context.Context, engine *runtime.Engine, accept runtime.CommandAcceptance) error {
		shouldCompact, err := engine.ShouldCompactBeforeUserMessage(runCtx, projection.ExecutionText)
		if err != nil {
			return err
		}
		compacted := false
		var acceptedCompactionErr error
		if shouldCompact {
			compactionAccepted, compactErr := s.runPreSubmitCompaction(
				attempt.Context(),
				engine,
				projection.ExecutionText,
			)
			if compactionAccepted {
				compacted = true
				acceptedCompactionErr = compactErr
			} else if compactErr != nil {
				if !errors.Is(compactErr, runtime.ErrAgentBusy) {
					return compactErr
				}
			}
		}
		queued, err := engine.SteerInput(
			runCtx,
			projection.queuedInput(),
			accept,
		)
		if err != nil {
			return errors.Join(acceptedCompactionErr, err)
		}
		response = queuedUserTurnResponse(compacted, queued.ID)
		return acceptedCompactionErr
	}
	executeTurn := func() error {
		return s.authority.RunCurrentTurn(
			attempt.Context(),
			descriptor,
			attempt.Accept,
			runTurn,
		)
	}
	err = executeTurn()
	if errors.Is(err, sessionruntime.ErrSessionWorkflowActivationActive) {
		preparing := false
		var preparingErr error
		if s.preparations != nil {
			preparing, preparingErr = s.preparations.WorkflowSessionPreparing(attempt.Context(), sessionID)
		}
		switch {
		case preparingErr != nil:
			err = preparingErr
		case preparing:
			err = s.withRuntime(attempt.Context(), request.SessionID, func(runCtx context.Context, engine *runtime.Engine) error {
				return runTurn(runCtx, engine, attempt.Accept)
			})
		case s.reactivator != nil:
			var workflowState *runtime.WorkflowSessionState
			stateErr := s.withRuntime(attempt.Context(), request.SessionID, func(_ context.Context, engine *runtime.Engine) error {
				var err error
				workflowState, err = engine.WorkflowSessionState()
				return err
			})
			if stateErr != nil {
				err = stateErr
				break
			}
			if workflowState == nil {
				err = errors.New("retained Workflow Session has no Current Node binding")
				break
			}
			handle, reactivateErr := s.reactivator.ReactivateWorkflowSession(attempt.Context(), sessionID)
			if reactivateErr != nil {
				err = reactivateErr
			} else {
				_, err = s.authority.ValidateLiveWorkflowAgentExecution(
					handle,
					sessionID,
					workflowState.CurrentNode,
				)
				if err == nil {
					err = s.withRuntime(attempt.Context(), request.SessionID, func(runCtx context.Context, engine *runtime.Engine) error {
						return runTurn(runCtx, engine, attempt.Accept)
					})
				}
			}
		}
	}
	return response, err
}

func (s *Service) recordAcceptedUserTurnHistory(
	request sessionUserTurnRequest,
	projection PreparedUserTurn,
) error {
	if _, err := s.recordPromptHistory(context.Background(), request.SessionID, projection.HistoryText); err != nil {
		reportErr := s.withRuntime(context.Background(), request.SessionID, func(_ context.Context, engine *runtime.Engine) error {
			engine.ReportPromptHistoryPersistError(err.Error())
			return nil
		})
		return errors.Join(err, reportErr)
	}
	return nil
}

func (s *Service) runPreSubmitCompaction(
	ctx context.Context,
	engine *runtime.Engine,
	text string,
) (bool, error) {
	attempt := newRuntimeCommandAttempt(ctx)
	defer attempt.Finish()
	receipt, err := engine.CompactContextForPreSubmitWithAcceptance(attempt.Context(), text, attempt.Accept)
	return receipt.Committed, err
}
