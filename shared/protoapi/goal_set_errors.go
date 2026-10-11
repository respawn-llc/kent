package protoapi

import (
	"errors"
	"strings"

	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func GoalSetErrorFromError(err error, sessionID *runtimeids.SessionID) *runtimepb.GoalSetError {
	if err == nil {
		err = errors.New("Goal Set failed")
	}
	if errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		var identity *string
		if sessionID != nil {
			value := sessionID.String()
			identity = &value
		}
		return &runtimepb.GoalSetError{
			Code: "runtime_unavailable",
			Detail: &runtimepb.GoalSetError_RuntimeUnavailable{
				RuntimeUnavailable: &runtimepb.RuntimeUnavailableDetails{SessionId: identity},
			},
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
