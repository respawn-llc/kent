package promptcontrol

import (
	"context"
	"fmt"

	"core/server/metadata"
	"core/server/registry"
	servicecontract "core/shared/apicontract"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
)

type AskViewService struct {
	prompts  PendingPromptSource
	metadata *metadata.Store
}

func NewAskViewService(prompts PendingPromptSource, store *metadata.Store) *AskViewService {
	return &AskViewService{prompts: prompts, metadata: store}
}

func (s *AskViewService) ListPendingAsksBySession(ctx context.Context, req *promptpb.ListPendingRequest) (*promptpb.ListQuestionsSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if s == nil || s.prompts == nil {
		return nil, fmt.Errorf("pending prompt source is required")
	}
	sessionID, err := promptTarget(ctx, s.metadata, req.SessionId, req.CallerSessionId)
	if err != nil {
		return nil, fmt.Errorf("pending ask session identity: %w", err)
	}
	items := s.prompts.ListPendingPrompts(sessionID.String())
	identity, err := s.metadata.SessionIdentity(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	asks := make([]*promptpb.Question, 0, len(items))
	for _, item := range items {
		if item.Request.Approval {
			continue
		}
		ask, err := registry.QuestionFromSnapshot(sessionID.String(), item)
		if err != nil {
			return nil, err
		}
		ask.ShortId = protoapi.SessionShortIDToProto(identity.ShortID)
		asks = append(asks, ask)
	}
	return &promptpb.ListQuestionsSuccess{Questions: asks}, nil
}

var _ servicecontract.AskViewService = (*AskViewService)(nil)
