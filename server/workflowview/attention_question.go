package workflowview

import (
	"fmt"
	"strings"
	"time"

	"core/shared/clientui"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
)

type PendingPromptSnapshot struct {
	ToolCallID             clientui.ToolCallID
	SessionID              runtimeids.SessionID
	StepID                 runtimeids.StepID
	CreatedAt              time.Time
	Question               string
	Suggestions            []string
	RecommendedOptionIndex *int
	Approval               bool
	ApprovalDecisions      []clientui.ApprovalDecision
	AccessTargets          []clientui.FileAccessTarget
}

type PendingPromptSource interface {
	ListPendingPrompts(sessionID string) ([]PendingPromptSnapshot, error)
}

type pendingQuestion struct {
	message                string
	suggestions            []string
	recommendedOptionIndex *int
	prompt                 *serverapi.WorkflowAttentionQuestionPrompt
}

func pendingQuestionFromPrompt(snapshot PendingPromptSnapshot) (pendingQuestion, bool, error) {
	if err := snapshot.ToolCallID.Validate(); err != nil {
		return pendingQuestion{}, true, fmt.Errorf("pending prompt identity: %w", err)
	}
	if snapshot.SessionID.IsZero() {
		return pendingQuestion{}, true, fmt.Errorf("pending prompt %q has no session identity", snapshot.ToolCallID)
	}
	if snapshot.StepID.IsZero() {
		return pendingQuestion{}, true, fmt.Errorf("pending prompt %q has no step identity", snapshot.ToolCallID)
	}
	if snapshot.Approval {
		decisions := append([]clientui.ApprovalDecision(nil), snapshot.ApprovalDecisions...)
		for _, decision := range decisions {
			switch decision {
			case clientui.ApprovalDecisionAllowOnce, clientui.ApprovalDecisionAllowSession, clientui.ApprovalDecisionDeny:
			default:
				return pendingQuestion{}, true, fmt.Errorf("pending approval question %q has invalid decision %q", snapshot.ToolCallID, decision)
			}
		}
		if len(decisions) == 0 {
			return pendingQuestion{}, true, fmt.Errorf("pending approval question %q has no approval decisions", snapshot.ToolCallID)
		}
		return pendingQuestion{
			message: strings.TrimSpace(snapshot.Question),
			prompt: &serverapi.WorkflowAttentionQuestionPrompt{
				SessionID:         snapshot.SessionID,
				StepID:            snapshot.StepID,
				ToolCallID:        snapshot.ToolCallID,
				Kind:              serverapi.WorkflowAttentionQuestionKindApproval,
				ApprovalDecisions: decisions,
				AccessTargets:     append([]clientui.FileAccessTarget(nil), snapshot.AccessTargets...),
			},
		}, true, nil
	}
	suggestions := normalizedPendingQuestionSuggestions(snapshot.Suggestions)
	recommended, err := validatePendingQuestionRecommendation(snapshot.RecommendedOptionIndex, len(suggestions))
	if err != nil {
		return pendingQuestion{}, true, fmt.Errorf("pending question %q: %w", snapshot.ToolCallID, err)
	}
	return pendingQuestion{
		message:                strings.TrimSpace(snapshot.Question),
		suggestions:            suggestions,
		recommendedOptionIndex: recommended,
		prompt: &serverapi.WorkflowAttentionQuestionPrompt{
			SessionID:              snapshot.SessionID,
			StepID:                 snapshot.StepID,
			ToolCallID:             snapshot.ToolCallID,
			Kind:                   serverapi.WorkflowAttentionQuestionKindOrdinary,
			Suggestions:            suggestions,
			RecommendedOptionIndex: textutil.Pointer(recommended),
		},
	}, true, nil
}

func normalizedPendingQuestionSuggestions(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	return append([]string(nil), in...)
}

func validatePendingQuestionRecommendation(index *int, suggestionCount int) (*int, error) {
	if index == nil {
		return nil, nil
	}
	if *index < 1 || *index > suggestionCount {
		return nil, serverapi.WorkflowRequestValidationError{
			Code:    serverapi.WorkflowRequestErrorInvalidValue,
			Field:   "recommended_option_index",
			Message: fmt.Sprintf("recommended option %d is invalid for %d suggestions", *index, suggestionCount),
		}
	}
	return textutil.Pointer(index), nil
}
