package serverapi_test

import (
	"testing"

	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRuntimeGoalShowResponseOmitsAbsentGoal(t *testing.T) {
	wire, err := protoapi.Encode(&runtimepb.GoalShowSuccess{
		Availability: runtimepb.GoalAvailability_GOAL_AVAILABILITY_AVAILABLE,
	})
	if err != nil {
		t.Fatal(err)
	}
	var response runtimepb.GoalShowSuccess
	if err := protoapi.Decode(wire, &response); err != nil {
		t.Fatal(err)
	}
	if response.Goal != nil {
		t.Fatalf("goal = %+v; want absent", response.Goal)
	}
}

func TestRuntimeGoalShowResponseRejectsUnknownGoalStatus(t *testing.T) {
	now := timestamppb.Now()
	err := protoapi.Validate(&runtimepb.GoalShowSuccess{
		Availability: runtimepb.GoalAvailability_GOAL_AVAILABILITY_AVAILABLE,
		Goal: &runtimepb.Goal{
			Id: "goal-1", Objective: "ship", Status: runtimepb.GoalStatus(999),
			CreatedAt: now, UpdatedAt: now,
		},
	})
	if err == nil {
		t.Fatal("Goal Show accepted an unknown Goal status")
	}
}

func TestRuntimeGoalMutationResponseCarriesClosedAuthoritativeClear(t *testing.T) {
	response := &runtimepb.GoalMutationSuccess{
		Kind: runtimepb.GoalMutationResultKind_GOAL_MUTATION_RESULT_KIND_AUTHORITATIVE_CLEAR,
	}
	wire, err := protoapi.Encode(response)
	if err != nil {
		t.Fatal(err)
	}
	var decoded runtimepb.GoalMutationSuccess
	if err := protoapi.Decode(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != runtimepb.GoalMutationResultKind_GOAL_MUTATION_RESULT_KIND_AUTHORITATIVE_CLEAR || decoded.Goal != nil {
		t.Fatalf("decoded result = %+v, want authoritative clear", &decoded)
	}
}

func TestRuntimeSubmitUserShellCommandRejectsBlankCommand(t *testing.T) {
	err := protoapi.Validate(&runtimepb.ShellCommandRequest{
		SessionId: "session-1", Command: " \t\n",
	})
	if err == nil {
		t.Fatal("Shell command accepted a blank command")
	}
}

func TestRuntimeSubmitUserTurnRequestUsesInputAndRejectsMissingInput(t *testing.T) {
	req := &runtimepb.SubmitUserTurnRequest{
		SessionId: "session-1",
		Input:     &runtimepb.UserTurnInput{Input: &runtimepb.UserTurnInput_Text{Text: "hello"}},
	}
	if err := protoapi.Validate(req); err != nil {
		t.Fatalf("valid request: %v", err)
	}
	req.Input = nil
	if err := protoapi.Validate(req); err == nil {
		t.Fatal("missing input succeeded")
	}
}
