package serverapi

import (
	"encoding/json"
	"testing"
	"time"

	"core/shared/clientui"
)

func TestRuntimeGoalShowResponseOmitsAbsentGoal(t *testing.T) {
	wire, err := json.Marshal(RuntimeGoalShowResponse{
		GoalEnvelope: clientui.GoalEnvelope{
			Availability: clientui.GoalAvailabilityAvailable,
		},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(wire, &fields); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, exists := fields["goal"]; exists {
		t.Fatalf("goal field = %s; want absent", fields["goal"])
	}
}

func TestRuntimeGoalShowResponseRejectsUnknownGoalStatus(t *testing.T) {
	now := time.Now().UTC()
	err := (RuntimeGoalShowResponse{
		GoalEnvelope: clientui.GoalEnvelope{
			Availability: clientui.GoalAvailabilityAvailable,
			Goal: &clientui.Goal{
				ID:        "goal-1",
				Objective: "ship",
				Status:    clientui.RuntimeGoalStatus("unknown"),
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}).Validate()
	if err == nil {
		t.Fatal("RuntimeGoalShowResponse accepted an unknown Goal status")
	}
}

func TestRuntimeGoalMutationResponseCarriesClosedAuthoritativeClear(t *testing.T) {
	response := RuntimeGoalMutationResponse{Result: clientui.GoalMutationResult{
		Kind: clientui.GoalMutationResultAuthoritativeClear,
	}}
	if err := response.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	wire, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded struct {
		Result clientui.GoalMutationResult `json:"result"`
	}
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Result.Kind != clientui.GoalMutationResultAuthoritativeClear {
		t.Fatalf("result kind = %q, want authoritative clear", decoded.Result.Kind)
	}
}

func TestRuntimeSubmitUserShellCommandRejectsBlankCommand(t *testing.T) {
	err := (RuntimeSubmitUserShellCommandRequest{
		SessionID: "session-1",
		Command:   " \t\n",
	}).Validate()
	if err == nil {
		t.Fatal("RuntimeSubmitUserShellCommandRequest accepted a blank command")
	}
}
