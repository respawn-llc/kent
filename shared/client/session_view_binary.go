package client

import (
	"context"
	"fmt"

	sessionpb "core/shared/protoapi/gen/kent/api/session"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
)

func (c *Remote) GetPromptHistory(ctx context.Context, request *sessionpb.PromptHistoryRequest) (*sessionpb.PromptHistorySuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(sessionpb.File_kent_api_session_session_proto, "ReadService", "GetPromptHistory"),
		request, &sessionpb.PromptHistoryResult{},
		func(failure *sessionpb.PromptHistoryError) error {
			return projectInternalGeneratedError(failure.Code, failure.GetInternalFailure())
		})
}

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

func (c *Remote) GetSessionExecutionEnvironment(ctx context.Context, request *sessionpb.ExecutionEnvironmentRequest) (*sessionpb.ExecutionEnvironmentSuccess, error) {
	response, err := callGeneratedBinary(c, ctx,
		bootstrapMethod(sessionpb.File_kent_api_session_session_proto, "ReadService", "GetExecutionEnvironment"),
		request, &sessionpb.ExecutionEnvironmentResult{},
		func(failure *sessionpb.ExecutionEnvironmentError) error {
			return projectInternalGeneratedError(failure.Code, failure.GetInternalFailure())
		})
	if err != nil {
		return nil, err
	}
	if response.Environment.SessionId != request.SessionId {
		return nil, fmt.Errorf("session execution environment identity mismatch: requested %q, resolved %q", request.SessionId, response.Environment.SessionId)
	}
	return response, nil
}
