package promptcontrol

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/metadata"
	"core/server/session"
	"core/server/sessionruntime"
	askquestion "core/server/tools"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
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
	return NewPromptControlService(responder, nil), responder
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
		*question.Answer.Freeform != "question commentary" ||
		question.Answer.AnsweredBySessionID != nil {
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

func TestServiceAnswerPromptBatchAttributesUnrelatedAgent(t *testing.T) {
	persistedSessions, target, caller := promptControlAgentSessions(t)
	_, responder := newPromptControlTestService()
	service := NewPromptControlService(responder, persistedSessions)
	responder.batchResults = []sessionruntime.PromptAnswerResult{
		{ToolCallID: "question-1", Outcome: sessionruntime.PromptAnswerOutcomeResolved},
		{ToolCallID: "approval-1", Outcome: sessionruntime.PromptAnswerOutcomeResolved},
		{ToolCallID: "declined-1", Outcome: sessionruntime.PromptAnswerOutcomeSkipped},
	}
	request := promptAnswerBatchRequest(t)
	request.SessionId = target.Meta().SessionID
	callerSessionID := caller.Meta().SessionID
	request.InvokingSessionId = &callerSessionID

	if _, err := service.AnswerPromptBatch(t.Context(), request); err != nil {
		t.Fatalf("AnswerPromptBatch: %v", err)
	}
	if len(responder.batchCommands) == 0 {
		t.Fatal("AnswerPromptBatch did not deliver the Question answer")
	}
	answer, ok := responder.batchCommands[0].Payload.(sessionruntime.PromptQuestionAnswerCommand)
	if !ok || answer.Answer.AnsweredBySessionID == nil ||
		answer.Answer.AnsweredBySessionID.String() != caller.Meta().SessionID {
		t.Fatalf("Question answer actor = %+v, want invoking Session %q", answer, caller.Meta().SessionID)
	}
}

func promptControlAgentSessions(t *testing.T) (*metadata.Store, *session.Store, *session.Store) {
	t.Helper()
	cfg := testsetup.ProgrammaticConfig(t, config.DefaultOnboardingSettings())
	persistedSessions := testsetup.OpenStore(t, cfg.PersistenceRoot)
	binding, err := persistedSessions.RegisterWorkspaceBinding(t.Context(), cfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterBinding: %v", err)
	}
	options := persistedSessions.AuthoritativeSessionStoreOptions()
	container := filepath.Join(cfg.PersistenceRoot, "projects", binding.ProjectID, "sessions")
	target, err := session.Create(
		container,
		binding.WorkspaceName,
		cfg.WorkspaceRoot,
		sessioncontract.SessionCategoryMain,
		options...,
	)
	if err != nil {
		t.Fatalf("create target Session: %v", err)
	}
	otherParent, err := session.Create(
		container,
		binding.WorkspaceName,
		cfg.WorkspaceRoot,
		sessioncontract.SessionCategoryMain,
		options...,
	)
	if err != nil {
		t.Fatalf("create unrelated parent Session: %v", err)
	}
	caller, err := session.NewLazy(
		container,
		binding.WorkspaceName,
		cfg.WorkspaceRoot,
		sessioncontract.SessionCategorySubagent,
		options...,
	)
	if err != nil {
		t.Fatalf("create unrelated agent Session: %v", err)
	}
	if err := session.InitializeCreationContext(
		caller,
		otherParent,
		session.SessionCreationSourceParentAgent,
		session.ChildContextOptions{},
	); err != nil {
		t.Fatalf("initialize unrelated agent Session: %v", err)
	}
	if err := caller.EnsureDurable(); err != nil {
		t.Fatalf("persist unrelated agent Session: %v", err)
	}
	return persistedSessions, target, caller
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
