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

type PendingPromptSource interface {
	ListPendingPrompts(sessionID string) []registry.PendingPromptSnapshot
}

type ApprovalViewService struct {
	prompts  PendingPromptSource
	metadata *metadata.Store
}

func NewApprovalViewService(prompts PendingPromptSource, store *metadata.Store) *ApprovalViewService {
	return &ApprovalViewService{prompts: prompts, metadata: store}
}

func (s *ApprovalViewService) ListPendingApprovalsBySession(ctx context.Context, req *promptpb.ListPendingRequest) (*promptpb.ListApprovalsSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if s == nil || s.prompts == nil {
		return nil, fmt.Errorf("pending prompt source is required")
	}
	sessionID, err := promptTarget(ctx, s.metadata, req.SessionId, req.CallerSessionId)
	if err != nil {
		return nil, fmt.Errorf("pending approval session identity: %w", err)
	}
	items := s.prompts.ListPendingPrompts(sessionID.String())
	identity, err := s.metadata.SessionIdentity(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	approvals := make([]*promptpb.Approval, 0, len(items))
	for _, item := range items {
		if !item.Request.Approval {
			continue
		}
		approval, err := registry.ApprovalFromSnapshot(sessionID.String(), item)
		if err != nil {
			return nil, err
		}
		approval.ShortId = protoapi.SessionShortIDToProto(identity.ShortID)
		approvals = append(approvals, approval)
	}
	return &promptpb.ListApprovalsSuccess{Approvals: approvals}, nil
}

var _ servicecontract.ApprovalViewService = (*ApprovalViewService)(nil)
