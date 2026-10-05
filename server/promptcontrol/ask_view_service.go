package promptcontrol

import (
	"context"
	"fmt"

	"core/server/registry"
	servicecontract "core/shared/apicontract"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/runtimeids"
)

type AskViewService struct {
	prompts PendingPromptSource
}

func NewAskViewService(prompts PendingPromptSource) *AskViewService {
	return &AskViewService{prompts: prompts}
}

func (s *AskViewService) ListPendingAsksBySession(_ context.Context, req *promptpb.ListPendingRequest) (*promptpb.ListQuestionsSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if s == nil || s.prompts == nil {
		return nil, fmt.Errorf("pending prompt source is required")
	}
	sessionID, err := runtimeids.ParseSessionID(req.SessionId)
	if err != nil {
		return nil, fmt.Errorf("pending ask session identity: %w", err)
	}
	items := s.prompts.ListPendingPrompts(sessionID.String())
	asks := make([]*promptpb.Question, 0, len(items))
	for _, item := range items {
		if item.Request.Approval {
			continue
		}
		ask, err := registry.QuestionFromSnapshot(sessionID.String(), item)
		if err != nil {
			return nil, err
		}
		asks = append(asks, ask)
	}
	return &promptpb.ListQuestionsSuccess{Questions: asks}, nil
}

var _ servicecontract.AskViewService = (*AskViewService)(nil)
