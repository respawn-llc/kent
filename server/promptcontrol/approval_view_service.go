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

type PendingPromptSource interface {
	ListPendingPrompts(sessionID string) []registry.PendingPromptSnapshot
}

type ApprovalViewService struct {
	prompts PendingPromptSource
}

func NewApprovalViewService(prompts PendingPromptSource) *ApprovalViewService {
	return &ApprovalViewService{prompts: prompts}
}

func (s *ApprovalViewService) ListPendingApprovalsBySession(_ context.Context, req *promptpb.ListPendingRequest) (*promptpb.ListApprovalsSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if s == nil || s.prompts == nil {
		return nil, fmt.Errorf("pending prompt source is required")
	}
	sessionID, err := runtimeids.ParseSessionID(req.SessionId)
	if err != nil {
		return nil, fmt.Errorf("pending approval session identity: %w", err)
	}
	items := s.prompts.ListPendingPrompts(sessionID.String())
	approvals := make([]*promptpb.Approval, 0, len(items))
	for _, item := range items {
		if !item.Request.Approval {
			continue
		}
		approval, err := registry.ApprovalFromSnapshot(sessionID.String(), item)
		if err != nil {
			return nil, err
		}
		approvals = append(approvals, approval)
	}
	return &promptpb.ListApprovalsSuccess{Approvals: approvals}, nil
}

var _ servicecontract.ApprovalViewService = (*ApprovalViewService)(nil)
