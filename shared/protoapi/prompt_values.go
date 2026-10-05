package protoapi

import (
	"fmt"

	"core/shared/clientui"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/runtimeids"
	"core/shared/textutil"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func PendingAskFromQuestion(value *promptpb.Question) (clientui.PendingAsk, error) {
	if err := Validate(value); err != nil {
		return clientui.PendingAsk{}, err
	}
	sessionID, err := runtimeids.ParseSessionID(value.SessionId)
	if err != nil {
		return clientui.PendingAsk{}, err
	}
	stepID, err := runtimeids.ParseStepID(value.StepId)
	if err != nil {
		return clientui.PendingAsk{}, err
	}
	var recommended *int
	if value.RecommendedOptionIndex != nil {
		index := int(*value.RecommendedOptionIndex)
		recommended = &index
	}
	return clientui.PendingAsk{
		ToolCallID: clientui.ToolCallID(value.ToolCallId), SessionID: sessionID, StepID: stepID,
		Question: value.Question, Suggestions: append([]string(nil), value.Suggestions...),
		RecommendedOptionIndex: recommended, CreatedAt: value.CreatedAt.AsTime(),
	}, nil
}

func PendingApprovalFromApproval(value *promptpb.Approval) (clientui.PendingApproval, error) {
	if err := Validate(value); err != nil {
		return clientui.PendingApproval{}, err
	}
	sessionID, err := runtimeids.ParseSessionID(value.SessionId)
	if err != nil {
		return clientui.PendingApproval{}, err
	}
	stepID, err := runtimeids.ParseStepID(value.StepId)
	if err != nil {
		return clientui.PendingApproval{}, err
	}
	result := clientui.PendingApproval{
		ToolCallID: clientui.ToolCallID(value.ToolCallId), SessionID: sessionID, StepID: stepID,
		Question: value.GetQuestion(), AccessTargets: FileAccessTargetsFromProto(value.AccessTargets),
		CreatedAt: value.CreatedAt.AsTime(),
	}
	for _, option := range value.Options {
		decision, err := ApprovalDecisionFromProto(option.Decision)
		if err != nil {
			return clientui.PendingApproval{}, err
		}
		result.Options = append(result.Options, clientui.ApprovalOption{Decision: decision})
	}
	return result, nil
}

func QuestionFromPendingAsk(value clientui.PendingAsk) (*promptpb.Question, error) {
	result := &promptpb.Question{
		ToolCallId: string(value.ToolCallID), SessionId: value.SessionID.String(), StepId: value.StepID.String(),
		Question: value.Question, Suggestions: append([]string(nil), value.Suggestions...),
		CreatedAt: timestamppb.New(value.CreatedAt),
	}
	if value.RecommendedOptionIndex != nil {
		index, err := Int32(*value.RecommendedOptionIndex, "recommended option index")
		if err != nil {
			return nil, err
		}
		result.RecommendedOptionIndex = &index
	}
	return result, Validate(result)
}

func ApprovalFromPendingApproval(value clientui.PendingApproval) (*promptpb.Approval, error) {
	result := &promptpb.Approval{
		ToolCallId: string(value.ToolCallID), SessionId: value.SessionID.String(), StepId: value.StepID.String(),
		Question: textutil.OptionalExactString(value.Question), AccessTargets: FileAccessTargetsToProto(value.AccessTargets),
		CreatedAt: timestamppb.New(value.CreatedAt),
	}
	for _, option := range value.Options {
		decision, err := ApprovalDecisionToProto(option.Decision)
		if err != nil {
			return nil, err
		}
		result.Options = append(result.Options, &promptpb.ApprovalOption{Decision: decision})
	}
	return result, Validate(result)
}

func ApprovalDecisionToProto(value clientui.ApprovalDecision) (promptpb.ApprovalDecision, error) {
	switch value {
	case clientui.ApprovalDecisionAllowOnce:
		return promptpb.ApprovalDecision_APPROVAL_DECISION_ALLOW_ONCE, nil
	case clientui.ApprovalDecisionAllowSession:
		return promptpb.ApprovalDecision_APPROVAL_DECISION_ALLOW_SESSION, nil
	case clientui.ApprovalDecisionDeny:
		return promptpb.ApprovalDecision_APPROVAL_DECISION_DENY, nil
	default:
		return promptpb.ApprovalDecision_APPROVAL_DECISION_UNSPECIFIED, fmt.Errorf("invalid approval decision %q", value)
	}
}

func ApprovalDecisionFromProto(value promptpb.ApprovalDecision) (clientui.ApprovalDecision, error) {
	switch value {
	case promptpb.ApprovalDecision_APPROVAL_DECISION_ALLOW_ONCE:
		return clientui.ApprovalDecisionAllowOnce, nil
	case promptpb.ApprovalDecision_APPROVAL_DECISION_ALLOW_SESSION:
		return clientui.ApprovalDecisionAllowSession, nil
	case promptpb.ApprovalDecision_APPROVAL_DECISION_DENY:
		return clientui.ApprovalDecisionDeny, nil
	default:
		return "", fmt.Errorf("invalid approval decision %v", value)
	}
}

func FileAccessTargetsToProto(values []clientui.FileAccessTarget) []*promptpb.FileAccessTarget {
	out := make([]*promptpb.FileAccessTarget, 0, len(values))
	for _, value := range values {
		out = append(out, &promptpb.FileAccessTarget{RequestedPath: value.RequestedPath, ResolvedPath: value.ResolvedPath})
	}
	return out
}

func FileAccessTargetsFromProto(values []*promptpb.FileAccessTarget) []clientui.FileAccessTarget {
	out := make([]clientui.FileAccessTarget, 0, len(values))
	for _, value := range values {
		out = append(out, clientui.FileAccessTarget{RequestedPath: value.RequestedPath, ResolvedPath: value.ResolvedPath})
	}
	return out
}
