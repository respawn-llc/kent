package workflowview

import (
	"fmt"

	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type PendingPromptSource interface {
	ListPendingPrompts(sessionID string) ([]*promptpb.ObservationQuestion, error)
}

type pendingPromptIdentity interface {
	GetSessionId() string
	GetStepId() string
	GetToolCallId() string
	GetCreatedAt() *timestamppb.Timestamp
}

func pendingPromptDetails(question *promptpb.ObservationQuestion) (pendingPromptIdentity, error) {
	switch selected := question.GetQuestion().(type) {
	case *promptpb.ObservationQuestion_Ask:
		return selected.Ask, nil
	case *promptpb.ObservationQuestion_Approval:
		return selected.Approval, nil
	default:
		return nil, fmt.Errorf("pending prompt has no Question or Approval")
	}
}
