package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
)

func TestNestedResultErrorDetail(t *testing.T) {
	for _, test := range []struct {
		name     string
		failure  *runtimepb.GoalSetError
		accepted bool
	}{
		{
			name: "known code with declared detail",
			failure: &runtimepb.GoalSetError{Code: "auth_required",
				Detail: &runtimepb.GoalSetError_AuthRequired{AuthRequired: &authpb.AuthRequiredDetails{}}},
			accepted: true,
		},
		{
			name: "known code with wrong detail",
			failure: &runtimepb.GoalSetError{Code: "auth_required",
				Detail: &runtimepb.GoalSetError_InternalFailure{InternalFailure: &sharedpb.InternalFailureDetails{}}},
		},
		{
			name:    "known code without detail",
			failure: &runtimepb.GoalSetError{Code: "auth_required"},
		},
		{
			name: "unknown code with known detail",
			failure: &runtimepb.GoalSetError{Code: "future_error",
				Detail: &runtimepb.GoalSetError_InternalFailure{InternalFailure: &sharedpb.InternalFailureDetails{}}},
			accepted: true,
		},
		{
			name:     "unknown code without detail",
			failure:  &runtimepb.GoalSetError{Code: "future_error"},
			accepted: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := &runtimepb.GoalSetResult{Outcome: &runtimepb.GoalSetResult_Success{
				Success: &runtimepb.GoalSetSuccess{
					Session: &chatpb.ExistingSessionTarget{SessionId: runtimeids.NewSessionID().String()},
					Outcome: &runtimepb.GoalSetSuccess_Rejected{Rejected: test.failure},
				},
			}}
			encoded, err := protoapi.Encode(result)
			if err != nil {
				t.Fatal(err)
			}
			received := &runtimepb.GoalSetResult{}
			if err := protoapi.Decode(encoded, received); err != nil {
				t.Fatal(err)
			}
			err = protoapi.ValidateResultErrorDetail(received)
			if (err == nil) != test.accepted {
				t.Errorf("nested error acceptance = %v, want %t", err, test.accepted)
			}
		})
	}
}
