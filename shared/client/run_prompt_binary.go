package client

import (
	"context"
	"errors"

	"core/shared/protoapi"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"google.golang.org/protobuf/proto"
)

func (c *Remote) RunPrompt(ctx context.Context, request *runpromptpb.Request, progress serverapi.RunPromptProgressSink) (*runpromptpb.Success, error) {
	method := bootstrapMethod(runpromptpb.File_kent_api_run_prompt_run_prompt_proto, "RunService", "Prompt")
	result := &runpromptpb.Result{}
	if err := c.callProgress(ctx, "run-prompt", method, request, result, func() proto.Message { return &runpromptpb.ProgressEvent{} }, func(event proto.Message) {
		if progress != nil {
			progress.PublishRunPromptProgress(event.(*runpromptpb.ProgressEvent))
		}
	}); err != nil {
		return nil, err
	}
	return decodeGeneratedResult(method, result, func(failure *runpromptpb.Error) error {
		if rejected := failure.GetSelectionRejected(); rejected != nil {
			return &serverapi.RunSelectionRejectedError{Reason: rejected.Reason}
		}
		if details := failure.GetSubagentLaunchDenied(); details != nil {
			return protoapi.SubagentLaunchDeniedFromProto(details)
		}
		if details := failure.GetCallerSessionNotFound(); details != nil {
			return errors.Join(
				&serverapi.SubagentLaunchDeniedError{Kind: serverapi.SubagentLaunchDenialCallerMissing},
				&sessioncontract.SessionNotFoundError{SessionID: details.SessionId},
			)
		}
		return generatedOperationFailure(failure.Code)
	})
}
