package sessioncontract

import (
	"errors"
	"fmt"
)

type PromptApprovalDecision string

const (
	PromptApprovalDecisionAllowOnce    PromptApprovalDecision = "allow_once"
	PromptApprovalDecisionAllowSession PromptApprovalDecision = "allow_session"
	PromptApprovalDecisionDeny         PromptApprovalDecision = "deny"
)

var ErrPromptQuestionAnswerRequired = errors.New("question answer requires a selected option or freeform text")

func ValidatePromptApprovalDecision(decision PromptApprovalDecision) error {
	switch decision {
	case PromptApprovalDecisionAllowOnce,
		PromptApprovalDecisionAllowSession,
		PromptApprovalDecisionDeny:
		return nil
	default:
		return fmt.Errorf("approval decision %q is invalid", decision)
	}
}
