package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
)

func TestGoalMutationExecutionIdentityContract(t *testing.T) {
	run, step, blank := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", " \t "
	for _, tc := range []struct {
		name  string
		actor string
		run   *string
		step  *string
		valid bool
	}{
		{"user without identity", "user", nil, nil, true},
		{"agent without identity", "agent", nil, nil, true},
		{"normalized blank identity", " agent ", &blank, &blank, true},
		{"agent exact identity", " agent ", &run, &step, true},
		{"agent missing run", "agent", nil, &step, false},
		{"agent missing step", "agent", &run, nil, false},
		{"user identity", "user", &run, &step, false},
		{"system identity", "system", &run, &step, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := protoapi.Validate(&runtimepb.GoalMutationRequest{
				SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: run}},
				Actor:     tc.actor, RunId: tc.run, StepId: tc.step,
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
