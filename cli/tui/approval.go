package tui

import "core/shared/clientui"

func ApprovalDecisionLabel(decision clientui.ApprovalDecision) string {
	switch decision {
	case clientui.ApprovalDecisionAllowOnce:
		return "Allow once"
	case clientui.ApprovalDecisionAllowSession:
		return "Allow for this session"
	case clientui.ApprovalDecisionDeny:
		return "Deny"
	default:
		panic("invalid approval decision")
	}
}
