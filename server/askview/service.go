package askview

import (
	"context"
	"fmt"
	"strings"

	"core/server/registry"
	"core/shared/clientui"
	"core/shared/serverapi"
	"core/shared/servicecontract"
)

type PendingPromptSource interface {
	ListPendingPrompts(sessionID string) []registry.PendingPromptSnapshot
}

type Service struct {
	prompts PendingPromptSource
}

func NewService(prompts PendingPromptSource) *Service {
	return &Service{prompts: prompts}
}

func (s *Service) ListPendingAsksBySession(_ context.Context, req serverapi.AskListPendingBySessionRequest) (serverapi.AskListPendingBySessionResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.AskListPendingBySessionResponse{}, err
	}
	if s == nil || s.prompts == nil {
		return serverapi.AskListPendingBySessionResponse{}, fmt.Errorf("pending prompt source is required")
	}
	items := s.prompts.ListPendingPrompts(strings.TrimSpace(req.SessionID))
	asks := make([]clientui.PendingAsk, 0, len(items))
	for _, item := range items {
		if item.Request.Approval {
			continue
		}
		asks = append(asks, clientui.PendingAsk{
			AskID:                  item.Request.ID,
			SessionID:              strings.TrimSpace(req.SessionID),
			Question:               item.Request.Question,
			Suggestions:            append([]string(nil), item.Request.Suggestions...),
			RecommendedOptionIndex: item.Request.RecommendedOptionIndex,
			CreatedAt:              item.CreatedAt,
		})
	}
	return serverapi.AskListPendingBySessionResponse{Asks: asks}, nil
}

var _ servicecontract.AskViewService = (*Service)(nil)
