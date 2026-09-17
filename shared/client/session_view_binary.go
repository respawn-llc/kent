package client

import (
	"context"

	sessionpb "core/shared/protoapi/gen/kent/api/session"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
)

func (c *Remote) GetSessionMainView(ctx context.Context, request *sessionpb.MainViewRequest) (*sessionpb.MainViewSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(sessionpb.File_kent_api_session_session_proto, "ReadService", "GetMainView"),
		request, &sessionpb.MainViewResult{},
		func(failure *sessionpb.MainViewError) error {
			return projectInternalGeneratedError(failure.Code, failure.GetInternalFailure())
		})
}

func (c *Remote) GetSessionTranscriptPage(ctx context.Context, request *transcriptpb.PageRequest) (*transcriptpb.PageSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(transcriptpb.File_kent_api_transcript_transcript_proto, "ReadService", "GetPage"),
		request, &transcriptpb.PageResult{},
		func(failure *transcriptpb.PageError) error {
			return projectInternalGeneratedError(failure.Code, failure.GetInternalFailure())
		})
}

func (c *Remote) GetLatestCommittedAssistantFinalAnswer(ctx context.Context, request *transcriptpb.LatestFinalAnswerRequest) (*transcriptpb.LatestFinalAnswerSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(transcriptpb.File_kent_api_transcript_transcript_proto, "ReadService", "GetLatestFinalAnswer"),
		request, &transcriptpb.LatestFinalAnswerResult{},
		func(failure *transcriptpb.LatestFinalAnswerError) error {
			return projectInternalGeneratedError(failure.Code, failure.GetInternalFailure())
		})
}
