package goalview

import (
	"strings"

	"core/server/session"
	"core/shared/clientui"
)

func FromSessionState(
	goal *session.GoalState,
	availability session.GoalAvailability,
	suspended bool,
) *clientui.RuntimeGoal {
	projected := AvailabilityFromSession(availability)
	return &clientui.RuntimeGoal{
		Goal:         CoreFromSessionState(goal),
		Availability: &projected,
		Suspended:    suspended,
	}
}

func CoreFromSessionState(goal *session.GoalState) *clientui.Goal {
	if goal == nil {
		return nil
	}
	return &clientui.Goal{
		ID:        strings.TrimSpace(goal.ID),
		Objective: goal.Objective,
		Status:    clientui.RuntimeGoalStatus(goal.Status),
		CreatedAt: goal.CreatedAt,
		UpdatedAt: goal.UpdatedAt,
	}
}

func AvailabilityFromSession(availability session.GoalAvailability) clientui.GoalAvailability {
	if availability == session.GoalAvailable {
		return clientui.GoalAvailabilityAvailable
	}
	return clientui.GoalAvailabilityAgentCapabilityMissing
}
