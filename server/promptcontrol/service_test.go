package promptcontrol

import (
	"context"
	"errors"
	"testing"

	"core/server/sessionruntime"
	askquestion "core/server/tools"
	"core/shared/clientui"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

type stubPromptResponder struct {
	batchCalls    int
	batchSession  runtimeids.SessionID
	batchStep     runtimeids.StepID
	batchCommands []sessionruntime.PromptAnswerCommand
	batchResults  []sessionruntime.PromptAnswerResult
	batchErr      error

	followUpCalls    int
	followUpSession  runtimeids.SessionID
	followUpStep     runtimeids.StepID
	followUpToolCall clientui.ToolCallID
	followUp         serverapi.PromptFollowUpSubscription
	followUpErr      error
}

func (s *stubPromptResponder) ResolvePromptBatch(
	_ context.Context,
	sessionID runtimeids.SessionID,
	stepID runtimeids.StepID,
	commands []sessionruntime.PromptAnswerCommand,
) ([]sessionruntime.PromptAnswerResult, error) {
	s.batchCalls++
	s.batchSession = sessionID
	s.batchStep = stepID
	s.batchCommands = append([]sessionruntime.PromptAnswerCommand(nil), commands...)
	return append([]sessionruntime.PromptAnswerResult(nil), s.batchResults...), s.batchErr
}

func (s *stubPromptResponder) SubscribePromptFollowUp(
	_ context.Context,
	sessionID runtimeids.SessionID,
	stepID runtimeids.StepID,
	toolCallID clientui.ToolCallID,
) (serverapi.PromptFollowUpSubscription, error) {
	s.followUpCalls++
	s.followUpSession = sessionID
	s.followUpStep = stepID
	s.followUpToolCall = toolCallID
	return s.followUp, s.followUpErr
}

type stubPromptFollowUpSubscription struct{}

func (*stubPromptFollowUpSubscription) Next(context.Context) (*promptpb.FollowUpEvent, error) {
	return nil, errors.New("unexpected Next")
}

func (*stubPromptFollowUpSubscription) Close() error { return nil }

func newPromptControlTestService() (*PromptControlService, *stubPromptResponder) {
	responder := &stubPromptResponder{}
	return NewPromptControlService(responder), responder
}

func TestServiceSubscribeFollowUpInstallsWatcherBeforeReturning(t *testing.T) {
	service, responder := newPromptControlTestService()
	request := &promptpb.FollowUpWatchRequest{
		SessionId:  runtimeids.NewSessionID().String(),
		StepId:     promptControlStepID(t).String(),
		ToolCallId: "prompt-1",
	}
	subscription := &stubPromptFollowUpSubscription{}
	responder.followUp = subscription

	got, err := service.SubscribeFollowUp(context.Background(), request)
	if err != nil {
		t.Fatalf("SubscribeFollowUp: %v", err)
	}
	if got != subscription || responder.followUpCalls != 1 ||
		responder.followUpSession.String() != request.SessionId ||
		responder.followUpStep.String() != request.StepId ||
		string(responder.followUpToolCall) != request.ToolCallId {
		t.Fatalf("follow-up installation = subscription %p responder %+v", got, responder)
	}
}

func TestServiceAnswerPromptBatchTranslatesMixedEntries(t *testing.T) {
	service, responder := newPromptControlTestService()
	request := promptAnswerBatchRequest(t)
	responder.batchResults = []sessionruntime.PromptAnswerResult{
		{ToolCallID: "declined-1", Outcome: sessionruntime.PromptAnswerOutcomeSkipped},
		{ToolCallID: "question-1", Outcome: sessionruntime.PromptAnswerOutcomeResolved},
		{ToolCallID: "approval-1", Outcome: sessionruntime.PromptAnswerOutcomeResolved},
	}

	response, err := service.AnswerPromptBatch(context.Background(), request)
	if err != nil {
		t.Fatalf("AnswerPromptBatch: %v", err)
	}
	if responder.batchCalls != 1 || responder.batchSession.String() != request.SessionId || responder.batchStep.String() != request.StepId {
		t.Fatalf("batch delegation = calls %d session %s step %s", responder.batchCalls, responder.batchSession, responder.batchStep)
	}
	if len(responder.batchCommands) != 3 {
		t.Fatalf("batch commands = %+v", responder.batchCommands)
	}
	question, ok := responder.batchCommands[0].Payload.(sessionruntime.PromptQuestionAnswerCommand)
	if !ok ||
		question.Answer.SelectedOptionNumber == nil ||
		*question.Answer.SelectedOptionNumber != 2 ||
		question.Answer.Freeform == nil ||
		*question.Answer.Freeform != "question commentary" {
		t.Fatalf("question command = %+v", responder.batchCommands[0])
	}
	approval, ok := responder.batchCommands[1].Payload.(sessionruntime.PromptApprovalAnswerCommand)
	if !ok ||
		approval.Answer.Decision != askquestion.AskQuestionApprovalDecisionDeny ||
		approval.Answer.Commentary == nil ||
		*approval.Answer.Commentary != "approval commentary" {
		t.Fatalf("approval command = %+v", responder.batchCommands[1])
	}
	if _, ok := responder.batchCommands[2].Payload.(sessionruntime.PromptDeclinedCommand); !ok {
		t.Fatalf("declined command = %+v", responder.batchCommands[2])
	}
	if err := protoapi.ValidatePromptAnswerBatchResponse(request, response); err != nil {
		t.Fatalf("response correlation: %v", err)
	}
}

func promptAnswerBatchRequest(t *testing.T) *promptpb.AnswerBatchRequest {
	t.Helper()
	sessionID, err := runtimeids.ParseSessionID("11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	stepID, err := runtimeids.ParseStepID("22222222-2222-4222-8222-222222222222")
	if err != nil {
		t.Fatalf("ParseStepID: %v", err)
	}
	selected := int32(2)
	questionCommentary := "question commentary"
	approvalCommentary := "approval commentary"
	return &promptpb.AnswerBatchRequest{
		SessionId: sessionID.String(),
		StepId:    stepID.String(),
		Entries: []*promptpb.AnswerBatchEntry{
			{
				ToolCallId: "question-1",
				Answer: &promptpb.AnswerBatchEntry_QuestionAnswer{QuestionAnswer: &promptpb.QuestionAnswer{
					SelectedOptionNumber: &selected,
					Freeform:             &questionCommentary,
				}},
			},
			{
				ToolCallId: "approval-1",
				Answer: &promptpb.AnswerBatchEntry_ApprovalAnswer{ApprovalAnswer: &promptpb.ApprovalAnswer{
					Decision:   promptpb.ApprovalDecision_APPROVAL_DECISION_DENY,
					Commentary: &approvalCommentary,
				}},
			},
			{ToolCallId: "declined-1", Answer: &promptpb.AnswerBatchEntry_Declined{Declined: &promptpb.Declined{}}},
		},
	}
}

func promptControlStepID(t *testing.T) runtimeids.StepID {
	t.Helper()
	stepID, err := runtimeids.ParseStepID("22222222-2222-4222-8222-222222222222")
	if err != nil {
		t.Fatalf("ParseStepID: %v", err)
	}
	return stepID
}
