package transport

import (
	"testing"

	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
)

func TestBinaryResultRejectsNestedErrorDetail(t *testing.T) {
	method := runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("GoalService").Methods().ByName("Set")
	operation, err := protoapi.OperationFromDescriptor(method)
	if err != nil {
		t.Fatal(err)
	}
	failure := &runtimepb.GoalSetError{Code: "auth_required",
		Detail: &runtimepb.GoalSetError_InternalFailure{InternalFailure: &sharedpb.InternalFailureDetails{}}}
	result := &runtimepb.GoalSetResult{Outcome: &runtimepb.GoalSetResult_Success{
		Success: &runtimepb.GoalSetSuccess{
			Session: &chatpb.ExistingSessionTarget{SessionId: runtimeids.NewSessionID().String()},
			Outcome: &runtimepb.GoalSetSuccess_Rejected{Rejected: failure},
		},
	}}
	correlation := "nested-error"
	if encoded, err := encodeBinaryResult(operation, &correlation, result); err == nil || encoded != nil {
		t.Fatalf("invalid nested error produced an outgoing frame: bytes=%d err=%v", len(encoded), err)
	}
	failure.Detail = &runtimepb.GoalSetError_AuthRequired{AuthRequired: &authpb.AuthRequiredDetails{}}
	if _, err := encodeBinaryResult(operation, &correlation, result); err != nil {
		t.Fatalf("valid nested error rejected: %v", err)
	}
}
