package goalview

import (
	"fmt"
	"strings"

	"core/server/session"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
)

func FromSessionState(
	goal *session.GoalState,
	availability session.GoalAvailability,
	suspended bool,
) (*runtimepb.GoalView, error) {
	value, err := CoreFromSessionState(goal)
	if err != nil {
		return nil, err
	}
	projected := AvailabilityFromSession(availability)
	return &runtimepb.GoalView{
		Goal:         value,
		Availability: &projected,
		Suspended:    suspended,
	}, nil
}

func CoreFromSessionState(goal *session.GoalState) (*runtimepb.Goal, error) {
	if goal == nil {
		return nil, nil
	}
	status, err := statusFromSession(goal.Status)
	if err != nil {
		return nil, err
	}
	return &runtimepb.Goal{
		Id:        strings.TrimSpace(goal.ID),
		Objective: goal.Objective,
		Status:    status,
		CreatedAt: timestamppb.New(goal.CreatedAt),
		UpdatedAt: timestamppb.New(goal.UpdatedAt),
	}, nil
}

func statusFromSession(status session.GoalStatus) (runtimepb.GoalStatus, error) {
	switch status {
	case session.GoalStatusActive:
		return runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE, nil
	case session.GoalStatusPaused:
		return runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_PAUSED, nil
	case session.GoalStatusComplete:
		return runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_COMPLETE, nil
	default:
		return runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_UNSPECIFIED, fmt.Errorf("invalid persisted Goal status %q", status)
	}
}

func AvailabilityFromSession(availability session.GoalAvailability) runtimepb.GoalAvailability {
	if availability == session.GoalAvailable {
		return runtimepb.GoalAvailability_GOAL_AVAILABILITY_AVAILABLE
	}
	return runtimepb.GoalAvailability_GOAL_AVAILABILITY_AGENT_CAPABILITY_MISSING
}
