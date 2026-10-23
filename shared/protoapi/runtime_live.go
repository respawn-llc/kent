package protoapi

import (
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/serverapi"
)

func ObservationQuestionFromProto(question *promptpb.ObservationQuestion) (serverapi.ObservationQuestion, error) {
	switch selected := question.GetQuestion().(type) {
	case *promptpb.ObservationQuestion_Ask:
		ask, err := PendingAskFromQuestion(selected.Ask)
		if err != nil {
			return serverapi.ObservationQuestion{}, err
		}
		return serverapi.ObservationQuestion{Ask: &ask}, nil
	default:
		selectedApproval := question.Question.(*promptpb.ObservationQuestion_Approval)
		approval, err := PendingApprovalFromApproval(selectedApproval.Approval)
		if err != nil {
			return serverapi.ObservationQuestion{}, err
		}
		return serverapi.ObservationQuestion{Approval: &approval}, nil
	}
}

func ObservationQuestionToProto(question serverapi.ObservationQuestion) (*promptpb.ObservationQuestion, error) {
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
