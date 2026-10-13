package protoapi

import (
	"errors"

	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/serverapi"
)

func GoalSetErrorFromError(err error) *runtimepb.GoalSetError {
	if err == nil {
		err = errors.New("Goal Set failed")
	}
	if errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		details := &runtimepb.RuntimeUnavailableDetails{SessionTarget: SessionTargetErrorContext(err)}
		return &runtimepb.GoalSetError{
			Code: "runtime_unavailable",
			Detail: &runtimepb.GoalSetError_RuntimeUnavailable{
				RuntimeUnavailable: details,
			},
		}
	}
	return &runtimepb.GoalSetError{
		Code: "internal_failure",
		Detail: &runtimepb.GoalSetError_InternalFailure{
			InternalFailure: InternalFailureToProto(err),
		},
	}
}
