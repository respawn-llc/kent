package registry

import (
	"fmt"

	"core/shared/clientui"
	"core/shared/runtimeids"
)

func PendingAskFromSnapshot(sessionID runtimeids.SessionID, item PendingPromptSnapshot) (clientui.PendingAsk, error) {
	toolCallID, stepID, err := pendingToolCallIdentity(item.Request.ToolCallID, item.Request.StepID)
	if err != nil {
		return clientui.PendingAsk{}, fmt.Errorf("pending ask identity: %w", err)
	}
	recommendedOptionIndex, err := DecodeLegacyRecommendedOptionIndex(item.Request.RecommendedOptionIndex, len(item.Request.Suggestions))
	if err != nil {
		return clientui.PendingAsk{}, fmt.Errorf("pending ask %q: %w", item.Request.ToolCallID, err)
	}
	return clientui.PendingAsk{
		ToolCallID: toolCallID, SessionID: sessionID, StepID: stepID,
		Question: item.Request.Question, Suggestions: append([]string(nil), item.Request.Suggestions...),
		RecommendedOptionIndex: recommendedOptionIndex, CreatedAt: item.CreatedAt,
	}, nil
}

func PendingApprovalFromSnapshot(sessionID runtimeids.SessionID, item PendingPromptSnapshot) (clientui.PendingApproval, error) {
	toolCallID, stepID, err := pendingToolCallIdentity(item.Request.ToolCallID, item.Request.StepID)
	if err != nil {
		return clientui.PendingApproval{}, fmt.Errorf("pending approval identity: %w", err)
	}
	var options []clientui.ApprovalOption
	for _, option := range item.Request.ApprovalOptions {
		options = append(options, clientui.ApprovalOption{Decision: clientui.ApprovalDecision(option.Decision)})
	}
	return clientui.PendingApproval{
		ToolCallID: toolCallID, SessionID: sessionID, StepID: stepID,
		Question: item.Request.Question, Options: options,
		AccessTargets: append([]clientui.FileAccessTarget(nil), item.Request.AccessTargets...), CreatedAt: item.CreatedAt,
	}, nil
}

func pendingToolCallIdentity(rawToolCallID, rawStepID string) (clientui.ToolCallID, runtimeids.StepID, error) {
	toolCallID := clientui.ToolCallID(rawToolCallID)
	if err := toolCallID.Validate(); err != nil {
		return "", runtimeids.StepID{}, err
	}
	stepID, err := runtimeids.ParseStepID(rawStepID)
	if err != nil {
		return "", runtimeids.StepID{}, err
	}
	return toolCallID, stepID, nil
}
