package runtimecontrol

import (
	"context"
	"errors"

	"core/server/agentruntime"
	"core/server/sessionstore"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func (s *Service) AdmitChatQueuedUserInput(
	ctx context.Context,
	sessionIDText string,
	projection PreparedUserTurn,
) (serverapi.ChatInputAdmissionResult, error) {
	sessionID, err := runtimeids.ParseSessionID(sessionIDText)
	if err != nil {
		return serverapi.ChatInputAdmissionResult{}, err
	}
	if s == nil || s.authority == nil {
		return serverapi.ChatInputAdmissionResult{}, errors.New("session runtime authority is required")
	}
	attempt := newRuntimeCommandAttempt(ctx)
	defer attempt.Finish()
	descriptor, err := session.NewOpenSessionDescriptor(sessionID)
	if err != nil {
		return serverapi.ChatInputAdmissionResult{}, err
	}
	var queued runtime.QueuedUserMessage
	err = s.authority.RunCurrentTurn(attempt.Context(), descriptor, attempt.Accept, func(runCtx context.Context, engine *runtime.Engine, accept runtime.CommandAcceptance) error {
		var queueErr error
		queued, queueErr = engine.QueueUserInputWithAcceptance(
			runCtx,
			projection.queuedInput(),
			accept,
		)
		return queueErr
	})
	if !attempt.Accepted() {
		return serverapi.ChatInputAdmissionResult{}, err
	}
	queueItemID, parseErr := runtimeids.ParseQueueItemID(queued.ID)
	if parseErr != nil {
		return serverapi.ChatInputAdmissionResult{Accepted: true}, errors.Join(err, parseErr)
	}
	historyErr := s.recordAcceptedUserTurnHistory(canonicalUserTurnRequest(sessionIDText, projection.Input), projection)
	return serverapi.ChatInputAdmissionResult{
		QueueItemID:          queueItemID,
		Accepted:             true,
		PromptHistoryFailure: historyErr,
	}, err
}
