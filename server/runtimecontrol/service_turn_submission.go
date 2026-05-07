package runtimecontrol

import (
	"context"
	"strings"

	"builder/shared/serverapi"
)

func (s *Service) SubmitUserTurn(ctx context.Context, req serverapi.RuntimeSubmitUserTurnRequest) (serverapi.RuntimeSubmitUserTurnResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.RuntimeSubmitUserTurnResponse{}, err
	}
	memoReq := sessionTextMemoRequest{SessionID: strings.TrimSpace(req.SessionID), Text: req.Text}
	return s.turnSubmits.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameSessionTextMemoRequest, func(ctx context.Context) (serverapi.RuntimeSubmitUserTurnResponse, error) {
		if err := s.requireControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return serverapi.RuntimeSubmitUserTurnResponse{}, err
		}
		lease, err := s.acquirePrimaryRun(memoReq.SessionID)
		if err != nil {
			return serverapi.RuntimeSubmitUserTurnResponse{}, err
		}
		defer lease.Release()
		engine, err := s.resolve(ctx, req.SessionID)
		if err != nil {
			return serverapi.RuntimeSubmitUserTurnResponse{}, err
		}
		shouldCompact, err := engine.ShouldCompactBeforeUserMessage(ctx, memoReq.Text)
		if err != nil {
			return serverapi.RuntimeSubmitUserTurnResponse{}, err
		}
		if shouldCompact {
			if err := engine.CompactContextForPreSubmit(ctx); err != nil {
				return serverapi.RuntimeSubmitUserTurnResponse{}, err
			}
		}
		if err := engine.RecordPromptHistory(memoReq.Text); err != nil {
			return serverapi.RuntimeSubmitUserTurnResponse{}, err
		}
		msg, err := engine.SubmitUserMessage(ctx, memoReq.Text)
		if err != nil {
			return serverapi.RuntimeSubmitUserTurnResponse{}, err
		}
		return serverapi.RuntimeSubmitUserTurnResponse{Message: msg.Content, Compacted: shouldCompact}, nil
	})
}
