package protoapi

import (
	"fmt"

	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func ParentQuestionAnswerRejectedToProto(
	rejected *serverapi.ParentQuestionAnswerRejectedError,
) (*promptpb.ParentQuestionAnswerRejectedDetails, error) {
	if rejected == nil {
		return nil, fmt.Errorf("parent Question-answer rejection is required")
	}
	details := &promptpb.ParentQuestionAnswerRejectedDetails{
		AnsweringSessionId: rejected.AnsweringSessionID.String(),
		QuestionSessionId:  rejected.QuestionSessionID.String(),
	}
	if err := Validate(details); err != nil {
		return nil, fmt.Errorf("validate parent Question-answer rejection: %w", err)
	}
	return details, nil
}

func ParentQuestionAnswerRejectedFromProto(
	details *promptpb.ParentQuestionAnswerRejectedDetails,
) (*serverapi.ParentQuestionAnswerRejectedError, error) {
	if err := Validate(details); err != nil {
		return nil, fmt.Errorf("validate parent Question-answer rejection: %w", err)
	}
	answeringSessionID, err := runtimeids.ParseSessionID(details.AnsweringSessionId)
	if err != nil {
		return nil, fmt.Errorf("parse answering Session ID: %w", err)
	}
	questionSessionID, err := runtimeids.ParseSessionID(details.QuestionSessionId)
	if err != nil {
		return nil, fmt.Errorf("parse Question Session ID: %w", err)
	}
	return &serverapi.ParentQuestionAnswerRejectedError{
		AnsweringSessionID: answeringSessionID,
		QuestionSessionID:  questionSessionID,
	}, nil
}

// ValidatePromptAnswerBatchResponse checks correlation beyond each message's schema.
func ValidatePromptAnswerBatchResponse(request *promptpb.AnswerBatchRequest, response *promptpb.AnswerBatchSuccess) error {
	if err := Validate(request); err != nil {
		return err
	}
	if err := Validate(response); err != nil {
		return err
	}
	if len(request.Entries) != len(response.Results) {
		return fmt.Errorf("prompt answer batch result count %d does not match request entry count %d", len(response.Results), len(request.Entries))
	}
	requestIDs := make(map[string]struct{}, len(request.Entries))
	for _, entry := range request.Entries {
		requestIDs[entry.ToolCallId] = struct{}{}
	}
	for _, result := range response.Results {
		if _, exists := requestIDs[result.ToolCallId]; !exists {
			return fmt.Errorf("prompt answer batch result contains foreign tool call id %q", result.ToolCallId)
		}
	}
	return nil
}
