package protoapi

import (
	"core/shared/clientui"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/runtimeids"
	"core/shared/textutil"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func PendingAskFromQuestion(value *promptpb.Question) (clientui.PendingAsk, error) {
	shortID := SessionShortIDFromProto(value.ShortId)
	sessionID := runtimeids.SessionIDFromProto(value.SessionId)
	stepID, err := runtimeids.StepIDFromProto(value.StepId)
	if err != nil {
		return clientui.PendingAsk{}, err
	}
	var recommended *int
	if value.RecommendedOptionIndex != nil {
		index := int(*value.RecommendedOptionIndex)
		recommended = &index
	}
	return clientui.PendingAsk{
		ShortID:    shortID,
		ToolCallID: clientui.ToolCallID(value.ToolCallId), SessionID: sessionID, StepID: stepID,
		Question: value.Question, Suggestions: append([]string(nil), value.Suggestions...),
		RecommendedOptionIndex: recommended, CreatedAt: value.CreatedAt.AsTime(),
		Batch: questionBatchFromProto(value.Batch),
	}, nil
}

func PendingApprovalFromApproval(value *promptpb.Approval) (clientui.PendingApproval, error) {
	shortID := SessionShortIDFromProto(value.ShortId)
	sessionID := runtimeids.SessionIDFromProto(value.SessionId)
	stepID, err := runtimeids.StepIDFromProto(value.StepId)
	if err != nil {
		return clientui.PendingApproval{}, err
	}
	result := clientui.PendingApproval{
		ShortID:    shortID,
		ToolCallID: clientui.ToolCallID(value.ToolCallId), SessionID: sessionID, StepID: stepID,
		Question: value.GetQuestion(), AccessTargets: FileAccessTargetsFromProto(value.AccessTargets),
		CreatedAt: value.CreatedAt.AsTime(),
		Batch:     questionBatchFromProto(value.Batch),
	}
	for _, option := range value.Options {
		decision := ApprovalDecisionFromProto(option.Decision)
		result.Options = append(result.Options, clientui.ApprovalOption{Decision: decision})
	}
	return result, nil
}

func QuestionFromPendingAsk(value clientui.PendingAsk) (*promptpb.Question, error) {
	result := &promptpb.Question{
		ShortId:    SessionShortIDToProto(value.ShortID),
		ToolCallId: string(value.ToolCallID), SessionId: value.SessionID.String(), StepId: value.StepID.String(),
		Question: value.Question, Suggestions: append([]string(nil), value.Suggestions...),
		CreatedAt: timestamppb.New(value.CreatedAt),
		Batch:     questionBatchToProto(value.Batch),
	}
	if value.RecommendedOptionIndex != nil {
		index, err := Int32(*value.RecommendedOptionIndex, "recommended option index")
		if err != nil {
			return nil, err
		}
		result.RecommendedOptionIndex = &index
	}
	return result, nil
}

func ApprovalFromPendingApproval(value clientui.PendingApproval) (*promptpb.Approval, error) {
	result := &promptpb.Approval{
		ShortId:    SessionShortIDToProto(value.ShortID),
		ToolCallId: string(value.ToolCallID), SessionId: value.SessionID.String(), StepId: value.StepID.String(),
		Question: textutil.OptionalExactString(value.Question), AccessTargets: FileAccessTargetsToProto(value.AccessTargets),
		CreatedAt: timestamppb.New(value.CreatedAt),
		Batch:     questionBatchToProto(value.Batch),
	}
	for _, option := range value.Options {
		decision, err := ApprovalDecisionToProto(option.Decision)
		if err != nil {
			return nil, err
		}
		result.Options = append(result.Options, &promptpb.ApprovalOption{Decision: decision})
	}
	return result, nil
}

func questionBatchFromProto(value *promptpb.QuestionBatch) *clientui.QuestionBatch {
	if value == nil {
		return nil
	}
	return &clientui.QuestionBatch{
		ToolCallIDs: append([]string(nil), value.ToolCallIds...), UnmaterializedCount: value.UnmaterializedCount,
	}
}

func questionBatchToProto(value *clientui.QuestionBatch) *promptpb.QuestionBatch {
	if value == nil {
		return nil
	}
	return &promptpb.QuestionBatch{
		ToolCallIds: append([]string(nil), value.ToolCallIDs...), UnmaterializedCount: value.UnmaterializedCount,
	}
}

var approvalDecisionValues = map[promptpb.ApprovalDecision]clientui.ApprovalDecision{
	promptpb.ApprovalDecision_APPROVAL_DECISION_ALLOW_ONCE:    clientui.ApprovalDecisionAllowOnce,
	promptpb.ApprovalDecision_APPROVAL_DECISION_ALLOW_SESSION: clientui.ApprovalDecisionAllowSession,
	promptpb.ApprovalDecision_APPROVAL_DECISION_DENY:          clientui.ApprovalDecisionDeny,
}

func ApprovalDecisionToProto(value clientui.ApprovalDecision) (promptpb.ApprovalDecision, error) {
	return enumToProto(approvalDecisionValues, value, "approval decision")
}

func ApprovalDecisionFromProto(value promptpb.ApprovalDecision) clientui.ApprovalDecision {
	return approvalDecisionValues[value]
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
