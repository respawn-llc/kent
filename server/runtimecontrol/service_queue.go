package runtimecontrol

import (
	"context"
	"errors"

	"core/server/runtime"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func (s *Service) AdmitChatQueuedUserInput(
	ctx context.Context,
	req *runtimepb.SubmitUserTurnRequest,
) (serverapi.ChatInputAdmissionResult, error) {
	if err := protoapi.Validate(req); err != nil {
		return serverapi.ChatInputAdmissionResult{}, err
	}
	input, err := protoapi.UserTurnInputFromProto(req.Input)
	if err != nil {
		return serverapi.ChatInputAdmissionResult{}, err
	}
	projection, err := s.resolveUserTurnInput(ctx, req.SessionId, input)
	if err != nil {
		return serverapi.ChatInputAdmissionResult{}, err
	}
	sessionID, err := runtimeids.ParseSessionID(req.SessionId)
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
	historyErr := s.recordAcceptedUserTurnHistory(canonicalUserTurnRequest(req.SessionId, input), projection)
	return serverapi.ChatInputAdmissionResult{
		QueueItemID:          queueItemID,
		Accepted:             true,
		PromptHistoryFailure: historyErr,
	}, err
}
