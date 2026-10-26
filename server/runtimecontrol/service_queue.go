package runtimecontrol

import (
	"context"
	"errors"

	"core/server/session"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func (s *Service) AdmitChatQueuedUserInput(
	ctx context.Context,
	sessionIDText string,
	projection PreparedUserTurn,
) (serverapi.ChatInputAdmissionResult, error) {
	sessionID := runtimeids.SessionIDFromProto(sessionIDText)
	if s == nil || s.authority == nil {
		return serverapi.ChatInputAdmissionResult{}, errors.New("session runtime authority is required")
	}
	attempt := newRuntimeCommandAttempt(ctx)
	defer attempt.Finish()
	descriptor, err := session.NewOpenSessionDescriptor(sessionID)
	if err != nil {
		return serverapi.ChatInputAdmissionResult{}, err
	}
	queued, err := s.authority.QueueAgentInput(attempt.Context(), descriptor, projection.queuedInput(), attempt.Accept)
	if !attempt.Accepted() {
		return serverapi.ChatInputAdmissionResult{}, err
	}
	queueItemID, parseErr := runtimeids.QueueItemIDFromProto(queued.ID)
	if parseErr != nil {
		return serverapi.ChatInputAdmissionResult{Accepted: true}, errors.Join(err, parseErr)
	}
	historyErr := s.recordAcceptedUserTurnHistory(canonicalUserTurnRequest(sessionIDText, projection.Input), projection)
	return serverapi.ChatInputAdmissionResult{
		QueueItemID:          queueItemID,
		Disposition:          queued.Disposition,
		Accepted:             true,
		PromptHistoryFailure: historyErr,
	}, err
}
