package protoapi

import (
	"errors"
	"reflect"
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

func TestGoalMutationContractRequiresStepOnlyForAgentActor(t *testing.T) {
	sessionID := uuid.NewString()
	selector := &sharedpb.SessionSelector{
		Value: &sharedpb.SessionSelector_SessionId{SessionId: sessionID},
	}
	stepID := uuid.NewString()
	for _, tc := range []struct {
		name    string
		actor   string
		stepID  *string
		isValid bool
	}{
		{name: "agent without Step", actor: "agent"},
		{name: "agent with Step", actor: "agent", stepID: &stepID, isValid: true},
		{name: "user without Step", actor: "user", isValid: true},
		{name: "user with Step", actor: "user", stepID: &stepID},
		{name: "system without Step", actor: "system", isValid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := &runtimepb.GoalMutationRequest{
				SessionId: selector,
				Actor:     tc.actor,
				StepId:    tc.stepID,
			}
			if err := Validate(request); (err == nil) != tc.isValid {
				t.Fatalf("Goal Mutation validation = %v, want valid=%t", err, tc.isValid)
			}
		})
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
				RuntimeUnavailable: &runtimepb.RuntimeUnavailableDetails{},
			},
		},
	}

	if err := Validate(success); err == nil {
		t.Fatal("Goal Set runtime-unavailable diagnostic unexpectedly validated")
	}
}

func TestGoalSetErrorFromErrorIncludesRuntimeUnavailableSession(t *testing.T) {
	sessionID := runtimeids.NewSessionID()

	got := GoalSetErrorFromError(serverapi.WithSessionTarget(serverapi.CanonicalSessionTarget{ID: sessionID}, serverapi.ErrRuntimeUnavailable))

	if got.Code != "runtime_unavailable" {
		t.Fatalf("Goal Set error code = %q, want runtime_unavailable", got.Code)
	}
	detail := got.GetRuntimeUnavailable()
	var target *serverapi.SessionTargetError
	if detail == nil || !errors.As(RuntimeUnavailableFromProto(detail), &target) || target.SessionID() == nil || *target.SessionID() != sessionID {
		t.Fatalf("runtime-unavailable details = %+v, want Session %q", detail, sessionID)
	}
}

func TestGoalSetCapturedTargetPreservesFailureIdentity(t *testing.T) {
	id := runtimeids.NewSessionID()
	shortID, err := runtimeids.NewSessionShortID(73)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		target  serverapi.SessionTarget
		id      *runtimeids.SessionID
		shortID *runtimeids.SessionShortID
	}{
		{target: serverapi.UnresolvedSessionTarget{}},
		{target: serverapi.CanonicalSessionTarget{ID: id}, id: &id},
		{target: serverapi.ProjectedSessionTarget{Identity: runtimeids.SessionIdentity{ID: id}}, id: &id},
		{target: serverapi.ProjectedSessionTarget{Identity: runtimeids.SessionIdentity{ID: id, ShortID: &shortID}}, id: &id, shortID: &shortID},
	} {
		cause := serverapi.ErrRuntimeUnavailable
		captured := serverapi.WithSessionTarget(test.target, cause)
		if !errors.Is(captured, cause) {
			t.Fatal("capturing target replaced original cause")
		}
		encoded := GoalSetErrorFromError(captured)
		details := encoded.GetRuntimeUnavailable()
		if details == nil {
			t.Fatalf("original failure classification changed: %v", encoded)
		}
		if !errors.Is(RuntimeUnavailableFromProto(details), cause) {
			t.Fatal("conversion replaced original failure category")
		}
		var unavailable *serverapi.SessionTargetError
		if !errors.As(RuntimeUnavailableFromProto(details), &unavailable) || !reflect.DeepEqual(unavailable.Target, test.target) {
			t.Fatal("runtime unavailable conversion lost captured target stage")
		}
		var roundTrip *serverapi.SessionTargetError
		if !errors.As(InternalFailureFromProto(InternalFailureToProto(captured)), &roundTrip) {
			t.Fatal("internal failure conversion lost target context")
		}
		original := captured.(*serverapi.SessionTargetError)
		if !reflect.DeepEqual(roundTrip.Target, original.Target) {
			t.Fatalf("failure conversion changed captured facts: %v -> %v", original.Target, roundTrip.Target)
		}
	}
}
