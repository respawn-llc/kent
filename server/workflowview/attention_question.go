package workflowview

import (
	"context"
	"strings"
	"time"

	"core/shared/clientui"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"
)

type SessionActiveTranscriptProvider interface {
	SessionNewestActiveSegmentQuestions(ctx context.Context, sessionID string) ([]PendingQuestionTranscriptEntry, error)
}

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

type PendingQuestionTranscriptEntry struct {
	AskID                  string
	Question               string
	Suggestions            []string
	RecommendedOptionIndex *int
}

type PendingPromptSource interface {
	ListPendingPrompts(sessionID string) ([]PendingPromptSnapshot, error)
}

type pendingQuestion struct {
	message                string
	suggestions            []string
	recommendedOptionIndex *int
	prompt                 *taskpb.AttentionQuestionPrompt
}

func pendingQuestionFromPrompt(snapshot PendingPromptSnapshot) (pendingQuestion, bool, error) {
	if snapshot.Approval {
		decisions := make([]promptpb.ApprovalDecision, 0, len(snapshot.ApprovalDecisions))
		for _, decision := range snapshot.ApprovalDecisions {
			value, err := protoapi.ApprovalDecisionToProto(decision)
			if err != nil {
				return pendingQuestion{}, true, err
			}
			decisions = append(decisions, value)
		}
		return pendingQuestion{
			message: strings.TrimSpace(snapshot.Question),
			prompt: &taskpb.AttentionQuestionPrompt{
				SessionId:  snapshot.SessionID.String(),
				StepId:     snapshot.StepID.String(),
				ToolCallId: string(snapshot.ToolCallID),
				Kind:       taskpb.AttentionQuestionKind_ATTENTION_QUESTION_KIND_APPROVAL,
				Prompt: &taskpb.AttentionQuestionPrompt_Approval{Approval: &taskpb.ApprovalQuestion{
					ApprovalDecisions: decisions,
					AccessTargets:     protoapi.FileAccessTargetsToProto(snapshot.AccessTargets),
				}},
			},
		}, true, nil
	}
	suggestions := normalizedPendingQuestionSuggestions(snapshot.Suggestions)
	recommended := snapshot.RecommendedOptionIndex
	var index *int32
	if recommended != nil {
		value, err := protoapi.Int32(*recommended, "recommended option index")
		if err != nil {
			return pendingQuestion{}, true, err
		}
		index = &value
	}
	return pendingQuestion{
		message:                strings.TrimSpace(snapshot.Question),
		suggestions:            suggestions,
		recommendedOptionIndex: recommended,
		prompt: &taskpb.AttentionQuestionPrompt{
			SessionId:  snapshot.SessionID.String(),
			StepId:     snapshot.StepID.String(),
			ToolCallId: string(snapshot.ToolCallID),
			Kind:       taskpb.AttentionQuestionKind_ATTENTION_QUESTION_KIND_ORDINARY,
			Prompt: &taskpb.AttentionQuestionPrompt_Ordinary{Ordinary: &taskpb.OrdinaryQuestion{
				Suggestions: suggestions, RecommendedOptionIndex: index,
			}},
		},
	}, true, nil
}

func normalizedPendingQuestionSuggestions(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	return append([]string(nil), in...)
}
