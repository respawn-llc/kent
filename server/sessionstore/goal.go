package session

import (
	"errors"
	"strings"
)

func normalizeGoalActor(actor GoalActor) GoalActor {
	return GoalActor(strings.TrimSpace(string(actor)))
}

func normalizeGoalStatus(status GoalStatus) (GoalStatus, error) {
	switch GoalStatus(strings.TrimSpace(string(status))) {
	case GoalStatusActive:
		return GoalStatusActive, nil
	case GoalStatusPaused:
		return GoalStatusPaused, nil
	case GoalStatusComplete:
		return GoalStatusComplete, nil
	default:
		return "", errors.New("goal status must be active, paused, or complete")
	}
}

func cloneGoalState(goal *GoalState) *GoalState {
	if goal == nil {
		return nil
	}
	copyGoal := *goal
	copyGoal.ID = strings.TrimSpace(copyGoal.ID)
	copyGoal.Objective = strings.TrimSpace(copyGoal.Objective)
	copyGoal.Status = GoalStatus(strings.TrimSpace(string(copyGoal.Status)))
	return &copyGoal
}
