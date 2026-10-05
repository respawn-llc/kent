package protoapi

import (
	"testing"

	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"google.golang.org/protobuf/proto"
)

func validPromptAnswerBatchRequest() *promptpb.AnswerBatchRequest {
	return &promptpb.AnswerBatchRequest{
		SessionId: "11111111-1111-4111-8111-111111111111",
		StepId:    "22222222-2222-4222-8222-222222222222",
		Entries: []*promptpb.AnswerBatchEntry{
			{ToolCallId: "question-1", Answer: &promptpb.AnswerBatchEntry_QuestionAnswer{
				QuestionAnswer: &promptpb.QuestionAnswer{SelectedOptionNumber: proto.Int32(1)},
			}},
			{ToolCallId: "approval-1", Answer: &promptpb.AnswerBatchEntry_Declined{Declined: &promptpb.Declined{}}},
		},
	}
}

func TestPromptAnswerBatchRequestRejectsMalformedEntriesBeforeDelegation(t *testing.T) {
	for name, mutate := range map[string]func(*promptpb.AnswerBatchRequest){
		"missing session":         func(r *promptpb.AnswerBatchRequest) { r.SessionId = "" },
		"missing step":            func(r *promptpb.AnswerBatchRequest) { r.StepId = "" },
		"empty entries":           func(r *promptpb.AnswerBatchRequest) { r.Entries = nil },
		"duplicate prompt":        func(r *promptpb.AnswerBatchRequest) { r.Entries[1].ToolCallId = r.Entries[0].ToolCallId },
		"blank prompt":            func(r *promptpb.AnswerBatchRequest) { r.Entries[0].ToolCallId = " " },
		"padded prompt":           func(r *promptpb.AnswerBatchRequest) { r.Entries[0].ToolCallId = " question-1" },
		"missing union member":    func(r *promptpb.AnswerBatchRequest) { r.Entries[0].Answer = nil },
		"question missing answer": func(r *promptpb.AnswerBatchRequest) { r.Entries[0].GetQuestionAnswer().SelectedOptionNumber = nil },
		"question non-positive option": func(r *promptpb.AnswerBatchRequest) {
			r.Entries[0].GetQuestionAnswer().SelectedOptionNumber = proto.Int32(0)
		},
		"question blank freeform": func(r *promptpb.AnswerBatchRequest) { r.Entries[0].GetQuestionAnswer().Freeform = proto.String(" \t") },
		"approval invalid decision": func(r *promptpb.AnswerBatchRequest) {
			r.Entries[0].Answer = &promptpb.AnswerBatchEntry_ApprovalAnswer{ApprovalAnswer: &promptpb.ApprovalAnswer{Decision: promptpb.ApprovalDecision(99)}}
		},
		"approval blank commentary": func(r *promptpb.AnswerBatchRequest) {
			r.Entries[0].Answer = &promptpb.AnswerBatchEntry_ApprovalAnswer{ApprovalAnswer: &promptpb.ApprovalAnswer{Decision: promptpb.ApprovalDecision_APPROVAL_DECISION_DENY, Commentary: proto.String(" \t")}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			request := validPromptAnswerBatchRequest()
			mutate(request)
			if err := Validate(request); err == nil {
				t.Fatal("malformed batch unexpectedly validated")
			}
		})
	}
}

func TestPromptAnswerBatchResponseValidationAndCorrelationIgnoreResultOrder(t *testing.T) {
	request := validPromptAnswerBatchRequest()
	response := &promptpb.AnswerBatchSuccess{Results: []*promptpb.AnswerBatchEntryResult{
		{ToolCallId: "approval-1", Outcome: promptpb.AnswerBatchOutcome_ANSWER_BATCH_OUTCOME_SKIPPED},
		{ToolCallId: "question-1", Outcome: promptpb.AnswerBatchOutcome_ANSWER_BATCH_OUTCOME_RESOLVED},
	}}
	if err := ValidatePromptAnswerBatchResponse(request, response); err != nil {
		t.Fatalf("reordered exact response set rejected: %v", err)
	}
	for name, mutate := range map[string]func(*promptpb.AnswerBatchSuccess){
		"missing identity":   func(r *promptpb.AnswerBatchSuccess) { r.Results = r.Results[:1] },
		"foreign identity":   func(r *promptpb.AnswerBatchSuccess) { r.Results[0].ToolCallId = "foreign" },
		"duplicate identity": func(r *promptpb.AnswerBatchSuccess) { r.Results[0].ToolCallId = r.Results[1].ToolCallId },
		"blank identity":     func(r *promptpb.AnswerBatchSuccess) { r.Results[0].ToolCallId = "" },
		"invalid outcome":    func(r *promptpb.AnswerBatchSuccess) { r.Results[0].Outcome = promptpb.AnswerBatchOutcome(99) },
	} {
		t.Run(name, func(t *testing.T) {
			value := proto.Clone(response).(*promptpb.AnswerBatchSuccess)
			mutate(value)
			if err := ValidatePromptAnswerBatchResponse(request, value); err == nil {
				t.Fatal("malformed response unexpectedly correlated")
			}
		})
	}
}
