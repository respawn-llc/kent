package clientui

import "core/shared/runtimeids"

type PromptAnswer struct {
	ToolCallID           runtimeids.ToolCallID
	ErrorMessage         string
	Answer               string
	SelectedOptionNumber *int
	FreeformAnswer       string
	Approval             *ApprovalPromptAnswer
}

type ApprovalPromptAnswer struct {
	Decision   ApprovalDecision
	Commentary string
}
