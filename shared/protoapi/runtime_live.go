package protoapi

import (
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/serverapi"
	"fmt"
)

func ObservationQuestionFromProto(question *promptpb.ObservationQuestion) (serverapi.ObservationQuestion, error) {
	switch selected := question.GetQuestion().(type) {
	case *promptpb.ObservationQuestion_Ask:
		ask, err := PendingAskFromQuestion(selected.Ask)
		if err != nil {
			return serverapi.ObservationQuestion{}, err
		}
		return serverapi.ObservationQuestion{Ask: &ask}, nil
	case *promptpb.ObservationQuestion_Approval:
		approval, err := PendingApprovalFromApproval(selected.Approval)
		if err != nil {
			return serverapi.ObservationQuestion{}, err
		}
		return serverapi.ObservationQuestion{Approval: &approval}, nil
	default:
		return serverapi.ObservationQuestion{}, fmt.Errorf("observation Question is required")
	}
}

func ObservationQuestionToProto(question serverapi.ObservationQuestion) (*promptpb.ObservationQuestion, error) {
	if err := question.Validate(); err != nil {
		return nil, err
	}
	if question.Ask != nil {
		ask, err := QuestionFromPendingAsk(*question.Ask)
		if err != nil {
			return nil, err
		}
		return &promptpb.ObservationQuestion{Question: &promptpb.ObservationQuestion_Ask{Ask: ask}}, nil
	}
	approval, err := ApprovalFromPendingApproval(*question.Approval)
	if err != nil {
		return nil, err
	}
	return &promptpb.ObservationQuestion{Question: &promptpb.ObservationQuestion_Approval{Approval: approval}}, nil
}
