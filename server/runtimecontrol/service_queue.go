package runtimecontrol

import (
	"context"
	"errors"

	"core/server/agentruntime"
	"core/server/sessionstore"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/serverapi"
)

func (s *Service) AdmitChatQueuedUserInput(
	ctx context.Context,
	sessionIDText string,
	projection PreparedUserTurn,
) (serverapi.ChatInputAdmissionResult, error) {
	result, err := s.admitPendingInput(ctx, sessionIDText, func(runCtx context.Context, descriptor session.SessionDescriptor, accept runtime.CommandAcceptance) (runtime.QueuedUserMessage, error) {
		return s.authority.QueueAgentInput(runCtx, descriptor, projection.queuedInput(), accept)
	})
	if result.Accepted {
		result.PromptHistoryFailure = s.recordAcceptedUserTurnHistory(canonicalUserTurnRequest(sessionIDText, projection.Input), projection)
	}
	return result, err
}

// AdmitChatShellInput uses ordinary Chat acceptance. It never waits for process
// completion or records prompt history; accepted work belongs to the Runtime.
func (s *Service) AdmitChatShellInput(ctx context.Context, sessionID runtimeids.SessionID, id runtimeids.QueueItemID, command runtimeinput.ShellCommand, lane runtimeinput.PendingWorkLane) (serverapi.ChatInputAdmissionResult, error) {
	return s.admitPendingInput(ctx, sessionID.String(), func(runCtx context.Context, descriptor session.SessionDescriptor, accept runtime.CommandAcceptance) (runtime.QueuedUserMessage, error) {
		return s.authority.AdmitShellInput(runCtx, descriptor, id, command, lane, accept)
	})
}

func (s *Service) admitPendingInput(
	ctx context.Context,
	sessionIDText string,
	admit func(context.Context, session.SessionDescriptor, runtime.CommandAcceptance) (runtime.QueuedUserMessage, error),
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
	queued, err := admit(attempt.Context(), descriptor, attempt.Accept)
	if !attempt.Accepted() {
		return serverapi.ChatInputAdmissionResult{}, err
	}
	queueItemID, parseErr := runtimeids.QueueItemIDFromProto(queued.ID)
	if parseErr != nil {
		return serverapi.ChatInputAdmissionResult{Accepted: true}, errors.Join(err, parseErr)
	}
	return serverapi.ChatInputAdmissionResult{
		QueueItemID: queueItemID,
		Disposition: queued.Disposition,
		Accepted:    true,
	}, err
}
