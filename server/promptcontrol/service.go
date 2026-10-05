package promptcontrol

import (
	"context"
	"errors"
	"fmt"

	"core/server/sessionruntime"
	askquestion "core/server/tools"
	servicecontract "core/shared/apicontract"
	"core/shared/clientui"
	"core/shared/invariant"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

type PendingPromptResponder interface {
	ResolvePromptBatch(
		context.Context,
		runtimeids.SessionID,
		runtimeids.StepID,
		[]sessionruntime.PromptAnswerCommand,
	) ([]sessionruntime.PromptAnswerResult, error)
	SubscribePromptFollowUp(
		context.Context,
		runtimeids.SessionID,
		runtimeids.StepID,
		clientui.ToolCallID,
	) (serverapi.PromptFollowUpSubscription, error)
}

type PromptControlService struct {
	prompts PendingPromptResponder
}

func NewPromptControlService(prompts PendingPromptResponder) *PromptControlService {
	return &PromptControlService{prompts: prompts}
}

func (s *PromptControlService) AnswerPromptBatch(
	ctx context.Context,
	req *promptpb.AnswerBatchRequest,
) (*promptpb.AnswerBatchSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if s == nil || s.prompts == nil {
		return nil, errors.New("prompt responder is required")
	}
	sessionID, err := runtimeids.ParseSessionID(req.SessionId)
	if err != nil {
		return nil, err
	}
	stepID, err := runtimeids.ParseStepID(req.StepId)
	if err != nil {
		return nil, err
	}
	commands := make([]sessionruntime.PromptAnswerCommand, 0, len(req.Entries))
	for _, entry := range req.Entries {
		command := sessionruntime.PromptAnswerCommand{ToolCallID: clientui.ToolCallID(entry.ToolCallId)}
		switch answer := entry.Answer.(type) {
		case *promptpb.AnswerBatchEntry_QuestionAnswer:
			var selected *int
			if answer.QuestionAnswer.SelectedOptionNumber != nil {
				value := int(*answer.QuestionAnswer.SelectedOptionNumber)
				selected = &value
			}
			command.Payload = sessionruntime.PromptQuestionAnswerCommand{
				Answer: askquestion.AskQuestionAnswer{
					SelectedOptionNumber: selected,
					Freeform:             answer.QuestionAnswer.Freeform,
				},
			}
		case *promptpb.AnswerBatchEntry_ApprovalAnswer:
			decision, err := protoapi.ApprovalDecisionFromProto(answer.ApprovalAnswer.Decision)
			if err != nil {
				return nil, err
			}
			command.Payload = sessionruntime.PromptApprovalAnswerCommand{
				Answer: askquestion.AskQuestionApproval{
					Decision:   askquestion.AskQuestionApprovalDecision(decision),
					Commentary: answer.ApprovalAnswer.Commentary,
				},
			}
		case *promptpb.AnswerBatchEntry_Declined:
			command.Payload = sessionruntime.PromptDeclinedCommand{}
		default:
			return nil, reportPromptBatchTranslationInvariant(command.ToolCallID)
		}
		commands = append(commands, command)
	}
	results, err := s.prompts.ResolvePromptBatch(ctx, sessionID, stepID, commands)
	if err != nil {
		return nil, err
	}
	response := &promptpb.AnswerBatchSuccess{
		Results: make([]*promptpb.AnswerBatchEntryResult, 0, len(results)),
	}
	for _, result := range results {
		var outcome promptpb.AnswerBatchOutcome
		switch result.Outcome {
		case sessionruntime.PromptAnswerOutcomeResolved:
			outcome = promptpb.AnswerBatchOutcome_ANSWER_BATCH_OUTCOME_RESOLVED
		case sessionruntime.PromptAnswerOutcomeSkipped:
			outcome = promptpb.AnswerBatchOutcome_ANSWER_BATCH_OUTCOME_SKIPPED
		default:
			return nil, fmt.Errorf(
				"prompt batch responder returned invalid outcome %q",
				result.Outcome,
			)
		}
		response.Results = append(response.Results, &promptpb.AnswerBatchEntryResult{
			ToolCallId: string(result.ToolCallID), Outcome: outcome,
		})
	}
	if err := protoapi.ValidatePromptAnswerBatchResponse(req, response); err != nil {
		return nil, fmt.Errorf("validate prompt answer batch response: %w", err)
	}
	return response, nil
}

func (s *PromptControlService) SubscribeFollowUp(
	ctx context.Context,
	req *promptpb.FollowUpWatchRequest,
) (serverapi.PromptFollowUpSubscription, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if s == nil || s.prompts == nil {
		return nil, errors.New("prompt responder is required")
	}
	sessionID, err := runtimeids.ParseSessionID(req.SessionId)
	if err != nil {
		return nil, err
	}
	stepID, err := runtimeids.ParseStepID(req.StepId)
	if err != nil {
		return nil, err
	}
	return s.prompts.SubscribePromptFollowUp(ctx, sessionID, stepID, clientui.ToolCallID(req.ToolCallId))
}

func reportPromptBatchTranslationInvariant(toolCallID clientui.ToolCallID) error {
	err := fmt.Errorf("validated prompt answer batch entry %q has no disposition", toolCallID)
	invariant.NewPolicy().Check(false, invariant.WorkflowPromptDiagnostic(
		"translate_prompt_answer_batch_entry",
		string(toolCallID),
		err,
	))
	return err
}

var _ servicecontract.PromptControlService = (*PromptControlService)(nil)
