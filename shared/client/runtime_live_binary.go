package client

import (
	"context"
	"errors"

	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/serverapi"
)

func (c *Remote) LiveSteer(ctx context.Context, request *runtimepb.LiveSteerRequest) (*runtimepb.LiveSteerSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(runtimepb.File_kent_api_runtime_runtime_proto, "LiveService", "Steer"),
		request, &runtimepb.LiveSteerResult{}, runtimeLiveGeneratedError[*runtimepb.LiveSteerError])
}

func (c *Remote) LiveStop(ctx context.Context, request *runtimepb.LiveStopRequest) (*runtimepb.LiveStopSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(runtimepb.File_kent_api_runtime_runtime_proto, "LiveService", "Stop"),
		request, &runtimepb.LiveStopResult{}, runtimeLiveGeneratedError[*runtimepb.LiveStopError])
}

func (c *Remote) LiveWait(ctx context.Context, request *runtimepb.LiveWaitRequest) (*runtimepb.LiveWaitSuccess, error) {
	response, err := callGeneratedBinary(c, ctx,
		bootstrapMethod(runtimepb.File_kent_api_runtime_runtime_proto, "LiveService", "Wait"),
		request, &runtimepb.LiveWaitResult{}, runtimeLiveGeneratedError[*runtimepb.LiveWaitError])
	if err != nil {
		return nil, err
	}
	if err := validateRuntimeLiveResponseSession("runtime live wait", request.SessionId, response.SessionId); err != nil {
		return nil, err
	}
	return response, nil
}

func (c *Remote) LiveWatch(ctx context.Context, request *promptpb.LiveWatchRequest) (*promptpb.LiveWatchSuccess, error) {
	response, err := callGeneratedBinary(c, ctx,
		bootstrapMethod(promptpb.File_kent_api_prompt_prompt_proto, "LiveWatchService", "Watch"),
		request, &promptpb.LiveWatchResult{}, runtimeLiveGeneratedError[*promptpb.LiveWatchError])
	if err != nil {
		return nil, err
	}
	if err := validateRuntimeLiveResponseSession("runtime live watch", request.SessionId, response.SessionId); err != nil {
		return nil, err
	}
	return response, nil
}

func runtimeLiveGeneratedError[Failure runtimeControlFailure](failure Failure) error {
	switch failure.GetCode() {
	case "no_active_run":
		return serverapi.ErrRuntimeNoActiveRun
	case "no_final_answer":
		return serverapi.ErrRuntimeNoFinalAnswer
	case "stream_failed":
		if stream, ok := any(failure).(interface {
			GetStreamFailed() *sharedpb.InternalFailureDetails
		}); ok {
			return errors.Join(serverapi.ErrStreamFailed, protoapi.InternalFailureFromProto(stream.GetStreamFailed()))
		}
	}
	return runtimeControlGeneratedError(failure)
}
