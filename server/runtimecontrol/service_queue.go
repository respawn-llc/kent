package runtimecontrol

import (
	"context"
	"errors"

	"core/server/runtime"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func (s *Service) AdmitChatQueuedUserInput(
	ctx context.Context,
	session string,
	projection PreparedUserTurn,
) (serverapi.ChatInputAdmissionResult, error) {
	sessionID, err := runtimeids.ParseSessionID(session)
	if err != nil {
		return serverapi.ChatInputAdmissionResult{}, err
	}
	if s == nil || s.authority == nil {
		return serverapi.ChatInputAdmissionResult{}, errors.New("session runtime authority is required")
	}
	attempt := newRuntimeCommandAttempt(ctx)
	defer attempt.Finish()
	var queued runtime.QueuedUserMessage
	err = s.authority.WithCurrentRuntime(attempt.Context(), sessionID, func(runCtx context.Context, engine *runtime.Engine) error {
		var queueErr error
		queued, queueErr = engine.QueueUserInputWithAcceptance(
			runCtx,
			projection.queuedInput(),
			attempt.Accept,
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
	historyErr := s.recordAcceptedUserTurnHistory(canonicalUserTurnRequest(session, projection.Input), projection)
	return serverapi.ChatInputAdmissionResult{
		QueueItemID:          queueItemID,
		Accepted:             true,
		PromptHistoryFailure: historyErr,
	}, err
}
