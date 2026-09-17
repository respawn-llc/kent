package protoapi

import (
	"testing"

	chatpb "core/shared/protoapi/gen/kent/api/chat"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
	"core/shared/serverapi"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestGoalSetSuccessRejectsDiagnosticWithoutCommittedMutation(t *testing.T) {
	sessionID := uuid.NewString()
	success := &runtimepb.GoalSetSuccess{
		Session: &chatpb.ExistingSessionTarget{SessionId: sessionID},
		Outcome: &runtimepb.GoalSetSuccess_Rejected{
			Rejected: &runtimepb.GoalSetError{
				Code: "internal_failure",
				Detail: &runtimepb.GoalSetError_InternalFailure{
					InternalFailure: &sharedpb.InternalFailureDetails{},
				},
			},
		},
		Diagnostic: &runtimepb.GoalSetError{
			Code: "internal_failure",
			Detail: &runtimepb.GoalSetError_InternalFailure{
				InternalFailure: &sharedpb.InternalFailureDetails{},
			},
		},
	}

	if err := Validate(success); err == nil {
		t.Fatal("Goal Set rejection with diagnostic unexpectedly validated")
	}
}

func TestGoalSetSuccessRejectsRuntimeUnavailableDiagnostic(t *testing.T) {
	sessionID := uuid.NewString()
	now := timestamppb.Now()
	success := &runtimepb.GoalSetSuccess{
		Session: &chatpb.ExistingSessionTarget{SessionId: sessionID},
		Outcome: &runtimepb.GoalSetSuccess_Mutation{
			Mutation: &runtimepb.GoalMutationSuccess{
				Kind: runtimepb.GoalMutationResultKind_GOAL_MUTATION_RESULT_KIND_AUTHORITATIVE_GOAL,
				Goal: &runtimepb.Goal{
					Id:        "goal-1",
					Objective: "ship the feature",
					Status:    runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE,
					CreatedAt: now,
					UpdatedAt: now,
				},
			},
		},
		Diagnostic: &runtimepb.GoalSetError{
			Code: "runtime_unavailable",
			Detail: &runtimepb.GoalSetError_RuntimeUnavailable{
				RuntimeUnavailable: &runtimepb.RuntimeUnavailableDetails{SessionId: sessionID},
			},
		},
	}

	if err := Validate(success); err == nil {
		t.Fatal("Goal Set runtime-unavailable diagnostic unexpectedly validated")
	}
}

func TestGoalSetErrorFromErrorIncludesRuntimeUnavailableSession(t *testing.T) {
	sessionID := runtimeids.NewSessionID()

	got := GoalSetErrorFromError(serverapi.ErrRuntimeUnavailable, sessionID)

	if got.Code != "runtime_unavailable" {
		t.Fatalf("Goal Set error code = %q, want runtime_unavailable", got.Code)
	}
	detail := got.GetRuntimeUnavailable()
	if detail == nil || detail.SessionId != sessionID.String() {
		t.Fatalf("runtime-unavailable details = %+v, want Session %q", detail, sessionID)
	}
}
