package runtimecontrol

import (
	"context"
	"strings"

	"core/server/metadata"
	"core/shared/serverapi"
)

func (s *Service) SubmitUserTurn(ctx context.Context, req serverapi.RuntimeSubmitUserTurnRequest) (serverapi.RuntimeSubmitUserTurnResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.RuntimeSubmitUserTurnResponse{}, err
	}
	memoReq := turnSubmitMemoRequest{SessionID: strings.TrimSpace(req.SessionID), Text: req.Text, PromptHistoryRecorded: req.PromptHistoryRecorded}
	return s.turnSubmits.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameTurnSubmitMemoRequest, func(ctx context.Context) (serverapi.RuntimeSubmitUserTurnResponse, error) {
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
		runCtx := context.Background()
		if ctx != nil {
			runCtx = context.WithoutCancel(ctx)
		}
		shouldCompact, err := engine.ShouldCompactBeforeUserMessage(runCtx, memoReq.Text)
		if err != nil {
			return serverapi.RuntimeSubmitUserTurnResponse{}, err
		}
		if shouldCompact {
			if err := engine.CompactContextForPreSubmit(runCtx); err != nil {
				return serverapi.RuntimeSubmitUserTurnResponse{}, err
			}
		}
		if !req.PromptHistoryRecorded {
			if _, _, err := s.recordPromptHistory(ctx, metadata.PromptHistorySourceSubmitUserTurn, memoReq.SessionID, strings.TrimSpace(req.ClientRequestID), strings.TrimSpace(req.ClientRequestID), memoReq.Text); err != nil {
				return serverapi.RuntimeSubmitUserTurnResponse{}, err
			}
		}
		msg, err := engine.SubmitUserMessage(runCtx, memoReq.Text)
		if err != nil {
			return serverapi.RuntimeSubmitUserTurnResponse{}, err
		}
		return serverapi.RuntimeSubmitUserTurnResponse{Message: msg.Content, Compacted: shouldCompact}, nil
	})
}
