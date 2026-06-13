package runtimewire

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"core/server/tools/askquestion"
	patchtool "core/server/tools/patch"
)

const (
	OutsideWorkspaceAllowOnceSuggestion    = "Allow once"
	OutsideWorkspaceAllowSessionSuggestion = "Allow for this session"
	OutsideWorkspaceDenySuggestion         = "Deny"
)

type OutsideWorkspaceApprover struct {
	broker         *askquestion.Broker
	actionVerb     string
	mu             sync.Mutex
	sessionAllowed bool
}

func NewOutsideWorkspaceApprover(broker *askquestion.Broker, actionVerb string) *OutsideWorkspaceApprover {
	verb := strings.TrimSpace(actionVerb)
	if verb == "" {
		verb = "accessing"
	}
	return &OutsideWorkspaceApprover{broker: broker, actionVerb: verb}
}

func (a *OutsideWorkspaceApprover) Approve(ctx context.Context, req patchtool.OutsideWorkspaceRequest) (patchtool.OutsideWorkspaceApproval, error) {
	a.mu.Lock()
	if a.sessionAllowed {
		a.mu.Unlock()
		return patchtool.OutsideWorkspaceApproval{Decision: patchtool.OutsideWorkspaceDecisionAllowSession}, nil
	}
	a.mu.Unlock()

	resp, err := a.broker.Ask(ctx, askquestion.Request{
		Question: fmt.Sprintf("Allow %s %s (outside workspace dir)?", a.actionVerb, req.ResolvedPath),
		Approval: true,
		ApprovalOptions: []askquestion.ApprovalOption{
			{Decision: askquestion.ApprovalDecisionAllowOnce, Label: OutsideWorkspaceAllowOnceSuggestion},
			{Decision: askquestion.ApprovalDecisionAllowSession, Label: OutsideWorkspaceAllowSessionSuggestion},
			{Decision: askquestion.ApprovalDecisionDeny, Label: OutsideWorkspaceDenySuggestion},
		},
	})
	if err != nil {
		return patchtool.OutsideWorkspaceApproval{Decision: patchtool.OutsideWorkspaceDecisionDeny}, err
	}

	approval, err := OutsideWorkspaceApprovalFromResponse(resp)
	if err != nil {
		return patchtool.OutsideWorkspaceApproval{Decision: patchtool.OutsideWorkspaceDecisionDeny}, err
	}
	if approval.Decision == patchtool.OutsideWorkspaceDecisionAllowSession {
		a.mu.Lock()
		a.sessionAllowed = true
		a.mu.Unlock()
	}
	return approval, nil
}

func OutsideWorkspaceApprovalFromResponse(resp askquestion.Response) (patchtool.OutsideWorkspaceApproval, error) {
	payload := resp.Approval
	if payload == nil {
		return patchtool.OutsideWorkspaceApproval{}, errors.New("missing approval payload")
	}
	approval := patchtool.OutsideWorkspaceApproval{Commentary: strings.TrimSpace(payload.Commentary)}
	switch payload.Decision {
	case askquestion.ApprovalDecisionAllowOnce:
		approval.Decision = patchtool.OutsideWorkspaceDecisionAllowOnce
	case askquestion.ApprovalDecisionAllowSession:
		approval.Decision = patchtool.OutsideWorkspaceDecisionAllowSession
	case askquestion.ApprovalDecisionDeny:
		approval.Decision = patchtool.OutsideWorkspaceDecisionDeny
	default:
		return patchtool.OutsideWorkspaceApproval{}, fmt.Errorf("unsupported approval decision %q", payload.Decision)
	}
	return approval, nil
}
