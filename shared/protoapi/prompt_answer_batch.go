package protoapi

import (
	"fmt"

	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func ParentQuestionAnswerRejectedToProto(
	rejected *serverapi.ParentQuestionAnswerRejectedError,
) *promptpb.ParentQuestionAnswerRejectedDetails {
	return &promptpb.ParentQuestionAnswerRejectedDetails{
		AnsweringSessionId: rejected.AnsweringSessionID.String(),
		QuestionSessionId:  rejected.QuestionSessionID.String(),
	}
}

func ParentQuestionAnswerRejectedFromProto(
	details *promptpb.ParentQuestionAnswerRejectedDetails,
) *serverapi.ParentQuestionAnswerRejectedError {
	return &serverapi.ParentQuestionAnswerRejectedError{
		AnsweringSessionID: runtimeids.SessionIDFromProto(details.AnsweringSessionId),
		QuestionSessionID:  runtimeids.SessionIDFromProto(details.QuestionSessionId),
	}
}

// ValidatePromptAnswerBatchResponse checks correlation beyond each message's schema.
func ValidatePromptAnswerBatchResponse(request *promptpb.AnswerBatchRequest, response *promptpb.AnswerBatchSuccess) error {
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
