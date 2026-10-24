package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"google.golang.org/protobuf/proto"
)

func TestGoalRequestsRequireCanonicalStepID(t *testing.T) {
	const sessionID = "11111111-1111-4111-8111-111111111111"
	const stepID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	for _, value := range []string{
		stepID, " " + stepID + " ", "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA",
		"aaaaaaaaaaaa4aaa8aaaaaaaaaaaaaaa", "not-a-uuid", "aaaaaaaa-aaaa-1aaa-8aaa-aaaaaaaaaaaa",
	} {
		for _, request := range []proto.Message{
			&runtimepb.GoalMutationRequest{
				SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: sessionID}},
				Actor:     "agent", StepId: proto.String(value),
			},
			&runtimepb.GoalSetRequest{
				Target: &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{Session: &chatpb.ExistingSessionRequestTarget{
					SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: sessionID}},
				}}},
				Objective: "goal", Actor: "agent", StepId: proto.String(value),
				ExecutionPolicy: runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_PRESERVE_RUNTIME_STATE,
			},
		} {
			if err := protoapi.Validate(request); (err == nil) != (value == stepID) {
				t.Errorf("%T Step=%q acceptance=%v", request, value, err)
			}
		}
	}
}

func TestGoalMutationExecutionIdentityContract(t *testing.T) {
	sessionID, step, blank := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", " \t "
	for _, tc := range []struct {
		name  string
		actor string
		step  *string
		valid bool
	}{
		{"user without identity", "user", nil, true},
		{"agent without identity", "agent", nil, false},
		{"normalized blank identity", " agent ", &blank, false},
		{"agent exact identity", " agent ", &step, true},
		{"user identity", "user", &step, false},
		{"system identity", "system", &step, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := protoapi.Validate(&runtimepb.GoalMutationRequest{
				SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: sessionID}},
				Actor:     tc.actor, StepId: tc.step,
			})
			if (err == nil) != tc.valid {
				t.Fatalf("validation = %v, want valid=%t", err, tc.valid)
			}
		})
	}
}

func TestGoalObservationSequenceSupportsDesktopProjection(t *testing.T) {
	for _, tc := range []struct {
		sequence uint64
		valid    bool
	}{
		{9007199254740991, true},
		{9007199254740992, false},
	} {
		err := protoapi.Validate(&runtimepb.GoalObservation{
			Sequence: tc.sequence,
			Kind:     runtimepb.GoalObservationKind_GOAL_OBSERVATION_KIND_UPDATE,
			Status:   &runtimepb.GoalProjection{},
		})
		if (err == nil) != tc.valid {
			t.Fatalf("sequence %d validation = %v, want valid=%t", tc.sequence, err, tc.valid)
		}
	}
}
