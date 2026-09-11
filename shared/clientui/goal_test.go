package clientui

import (
	"testing"
	"time"
)

func TestGoalMutationResultValidatesEachClosedVariant(t *testing.T) {
	availability := GoalAvailabilityAvailable
	goal := &Goal{
		ID:        "goal-1",
		Objective: "ship",
		Status:    RuntimeGoalStatusActive,
		CreatedAt: time.Unix(1, 0).UTC(),
		UpdatedAt: time.Unix(1, 0).UTC(),
	}
	tests := []GoalMutationResult{
		{Kind: GoalMutationResultAuthoritativeGoal, Goal: goal, Availability: &availability},
		{Kind: GoalMutationResultAuthoritativeClear},
	}
	for _, result := range tests {
		if err := result.Validate(); err != nil {
			t.Fatalf("Validate(%q): %v", result.Kind, err)
		}
	}
}
