package protoapi

import (
	"errors"
	"strings"

	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/serverapi"
)

func GoalSetErrorFromError(err error) *runtimepb.GoalSetError {
	if err == nil {
		err = errors.New("Goal Set failed")
	}
	if errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		return &runtimepb.GoalSetError{
			Code:   "runtime_unavailable",
			Detail: &runtimepb.GoalSetError_RuntimeUnavailable{RuntimeUnavailable: &runtimepb.RuntimeUnavailableDetails{}},
		}
	}
	cause := strings.TrimSpace(err.Error())
	return &runtimepb.GoalSetError{
		Code: "internal_failure",
		Detail: &runtimepb.GoalSetError_InternalFailure{
			InternalFailure: &sharedpb.InternalFailureDetails{Cause: &cause},
		},
	}
}
