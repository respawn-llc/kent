package client

import (
	"context"

	sessionpb "core/shared/protoapi/gen/kent/api/session"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
)

func (c *Remote) GetViewedImageOriginal(ctx context.Context, request *sessionpb.ViewedImageOriginalRequest) (*sessionpb.ViewedImageOriginalSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(sessionpb.File_kent_api_session_session_proto, "ReadService", "GetViewedImageOriginal"),
		request, &sessionpb.ViewedImageOriginalResult{},
		func(failure *sessionpb.ViewedImageOriginalError) error {
			return generatedOperationFailure(failure.Code)
		})
}

func (c *Remote) GetPromptHistory(ctx context.Context, request *sessionpb.PromptHistoryRequest) (*sessionpb.PromptHistorySuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(sessionpb.File_kent_api_session_session_proto, "ReadService", "GetPromptHistory"),
		request, &sessionpb.PromptHistoryResult{},
		func(failure *sessionpb.PromptHistoryError) error {
			return generatedOperationFailure(failure.Code)
		})
}

func (c *Remote) GetSessionIdentityLocation(ctx context.Context, request *sessionpb.IdentityLocationRequest) (*sessionpb.IdentityLocationSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(sessionpb.File_kent_api_session_session_proto, "ReadService", "GetIdentityLocation"),
		request, &sessionpb.IdentityLocationResult{},
		func(failure *sessionpb.IdentityLocationError) error {
			return generatedOperationFailure(failure.Code)
		})
}

func (c *Remote) GetSessionMainView(ctx context.Context, request *sessionpb.MainViewRequest) (*sessionpb.MainViewSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(sessionpb.File_kent_api_session_session_proto, "ReadService", "GetMainView"),
		request, &sessionpb.MainViewResult{},
		func(failure *sessionpb.MainViewError) error {
			return generatedOperationFailure(failure.Code)
		})
}

func (c *Remote) GetSessionTranscriptPage(ctx context.Context, request *transcriptpb.PageRequest) (*transcriptpb.PageSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(transcriptpb.File_kent_api_transcript_transcript_proto, "ReadService", "GetPage"),
		request, &transcriptpb.PageResult{},
		func(failure *transcriptpb.PageError) error {
			return generatedOperationFailure(failure.Code)
		})
}

func (c *Remote) GetLatestCommittedAssistantFinalAnswer(ctx context.Context, request *transcriptpb.LatestFinalAnswerRequest) (*transcriptpb.LatestFinalAnswerSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(transcriptpb.File_kent_api_transcript_transcript_proto, "ReadService", "GetLatestFinalAnswer"),
		request, &transcriptpb.LatestFinalAnswerResult{},
		func(failure *transcriptpb.LatestFinalAnswerError) error {
			return generatedOperationFailure(failure.Code)
		})
}
