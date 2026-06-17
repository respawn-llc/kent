package runtimecontrol

import (
	"context"
	"strings"

	"core/server/runtime"
	"core/shared/serverapi"
)

func (s *Service) SubmitUserTurn(ctx context.Context, req serverapi.RuntimeSubmitUserTurnRequest) (serverapi.RuntimeSubmitUserTurnResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.RuntimeSubmitUserTurnResponse{}, err
	}
	memoReq := turnSubmitMemoRequest{SessionID: strings.TrimSpace(req.SessionID), Text: req.Text, PromptHistoryRecorded: req.PromptHistoryRecorded}
	return s.turnSubmits.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameTurnSubmitMemoRequest, func(ctx context.Context) (serverapi.RuntimeSubmitUserTurnResponse, error) {
		lease, err := s.acquirePrimaryRun(memoReq.SessionID)
		if err != nil {
			return serverapi.RuntimeSubmitUserTurnResponse{}, err
		}
		defer lease.Release()
		runCtx := context.Background()
		if ctx != nil {
			runCtx = context.WithoutCancel(ctx)
		}
		var resp serverapi.RuntimeSubmitUserTurnResponse
		err = s.withRuntimeAccess(ctx, req.SessionID, req.ControllerLeaseID, serverapi.SessionRuntimeOperationSubmitUserTurn, func(engine *runtime.Engine) error {
			shouldCompact, err := engine.ShouldCompactBeforeUserMessage(runCtx, memoReq.Text)
			if err != nil {
				return err
			}
			if shouldCompact {
				if err := engine.CompactContextForPreSubmit(runCtx); err != nil {
					return err
				}
			}
			if !req.PromptHistoryRecorded {
				if _, _, err := s.recordPromptHistory(runCtx, memoReq.SessionID, strings.TrimSpace(req.ClientRequestID), memoReq.Text); err != nil {
					return err
				}
			}
			msg, err := engine.SubmitUserMessage(runCtx, memoReq.Text)
			resp = serverapi.RuntimeSubmitUserTurnResponse{Message: msg.Content, Compacted: shouldCompact}
			return err
		})
		return resp, err
	})
}
