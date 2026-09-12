package serverapi_test

import (
	"testing"

	"core/shared/protoapi"
	sessionpb "core/shared/protoapi/gen/kent/api/session"

	"google.golang.org/protobuf/proto"
)

func TestSessionExecutionEnvironmentFieldStatesRemainIndependent(t *testing.T) {
	want := &sessionpb.ExecutionEnvironmentSuccess{Environment: &sessionpb.ExecutionEnvironment{
		SessionId: "environment-session",
		Workspace: &sessionpb.ExecutionWorkspaceField{Result: &sessionpb.ExecutionWorkspaceField_Available{
			Available: &sessionpb.ExecutionWorkspace{Path: "/workspace/current"},
		}},
		Branch: &sessionpb.ExecutionBranchField{Result: &sessionpb.ExecutionBranchField_Unavailable{
			Unavailable: sessionpb.ExecutionBranchUnavailableReason_EXECUTION_BRANCH_UNAVAILABLE_REASON_DETACHED_HEAD,
		}},
		Auth: &sessionpb.ExecutionAuthField{Result: &sessionpb.ExecutionAuthField_Failed{
			Failed: &sessionpb.ExecutionFieldError{
				Code:    sessionpb.ExecutionFieldErrorCode_EXECUTION_FIELD_ERROR_CODE_SOURCE_FAILURE,
				Message: "auth source unavailable",
			},
		}},
		Model: &sessionpb.ExecutionModelField{Result: &sessionpb.ExecutionModelField_Unavailable{
			Unavailable: sessionpb.ExecutionModelUnavailableReason_EXECUTION_MODEL_UNAVAILABLE_REASON_NOT_CONFIGURED,
		}},
	}}
	encoded, err := protoapi.Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	got := &sessionpb.ExecutionEnvironmentSuccess{}
	if err := protoapi.Decode(encoded, got); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(got, want) {
		t.Fatalf("execution field outcomes changed: got %v, want %v", got, want)
	}
}

func TestSessionExecutionEnvironmentRejectsMissingFieldOutcomes(t *testing.T) {
	if err := protoapi.Validate(&sessionpb.ExecutionEnvironmentSuccess{}); err == nil {
		t.Fatal("accepted absent environment")
	}
	if err := protoapi.Validate(&sessionpb.ExecutionWorkspaceField{}); err == nil {
		t.Fatal("accepted workspace without an outcome")
	}
	if err := protoapi.Validate(&sessionpb.ExecutionBranchField{Result: &sessionpb.ExecutionBranchField_Unavailable{
		Unavailable: sessionpb.ExecutionBranchUnavailableReason_EXECUTION_BRANCH_UNAVAILABLE_REASON_UNSPECIFIED,
	}}); err == nil {
		t.Fatal("accepted unavailable branch without a reason")
	}
}
