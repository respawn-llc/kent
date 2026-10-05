package client

import (
	"context"

	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
)

func (c *Remote) ListPendingAsksBySession(ctx context.Context, request *promptpb.ListPendingRequest) (*promptpb.ListQuestionsSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(promptpb.File_kent_api_prompt_prompt_proto, "QuestionService", "ListPending"),
		request, &promptpb.ListQuestionsResult{}, func(failure *promptpb.ListQuestionsError) error {
			return runtimeControlGeneratedError(failure)
		})
}

func (c *Remote) ListPendingApprovalsBySession(ctx context.Context, request *promptpb.ListPendingRequest) (*promptpb.ListApprovalsSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(promptpb.File_kent_api_prompt_prompt_proto, "ApprovalService", "ListPending"),
		request, &promptpb.ListApprovalsResult{}, func(failure *promptpb.ListApprovalsError) error {
			return runtimeControlGeneratedError(failure)
		})
}

func (c *Remote) AnswerPromptBatch(ctx context.Context, request *promptpb.AnswerBatchRequest) (*promptpb.AnswerBatchSuccess, error) {
	response, err := callGeneratedBinary(c, ctx,
		bootstrapMethod(promptpb.File_kent_api_prompt_prompt_proto, "AnswerService", "AnswerBatch"),
		request, &promptpb.AnswerBatchResult{}, func(failure *promptpb.AnswerBatchError) error {
			return runtimeControlGeneratedError(failure)
		})
	if err != nil {
		return nil, err
	}
	if err := protoapi.ValidatePromptAnswerBatchResponse(request, response); err != nil {
		return nil, err
	}
	return response, nil
}
