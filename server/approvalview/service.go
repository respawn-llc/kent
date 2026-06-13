package approvalview

import (
	"context"
	"fmt"
	"strings"

	"core/server/registry"
	askquestion "core/server/tools/askquestion"
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

func (s *Service) ListPendingApprovalsBySession(_ context.Context, req serverapi.ApprovalListPendingBySessionRequest) (serverapi.ApprovalListPendingBySessionResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.ApprovalListPendingBySessionResponse{}, err
	}
	if s == nil || s.prompts == nil {
		return serverapi.ApprovalListPendingBySessionResponse{}, fmt.Errorf("pending prompt source is required")
	}
	items := s.prompts.ListPendingPrompts(strings.TrimSpace(req.SessionID))
	approvals := make([]clientui.PendingApproval, 0, len(items))
	for _, item := range items {
		if !item.Request.Approval {
			continue
		}
		approvals = append(approvals, clientui.PendingApproval{
			ApprovalID: item.Request.ID,
			SessionID:  strings.TrimSpace(req.SessionID),
			Question:   item.Request.Question,
			Options:    approvalOptionsFromRequest(item.Request.ApprovalOptions),
			CreatedAt:  item.CreatedAt,
		})
	}
	return serverapi.ApprovalListPendingBySessionResponse{Approvals: approvals}, nil
}

func approvalOptionsFromRequest(options []askquestion.ApprovalOption) []clientui.ApprovalOption {
	if len(options) == 0 {
		return nil
	}
	out := make([]clientui.ApprovalOption, 0, len(options))
	for _, option := range options {
		out = append(out, clientui.ApprovalOption{
			Decision: clientui.ApprovalDecision(option.Decision),
			Label:    option.Label,
		})
	}
	return out
}

var _ servicecontract.ApprovalViewService = (*Service)(nil)
