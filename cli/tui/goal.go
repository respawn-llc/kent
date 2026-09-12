package tui

import (
	"fmt"

	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
)

func GoalStatusLabel(status runtimepb.GoalStatus) (string, error) {
	switch status {
	case runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE:
		return "active", nil
	case runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_PAUSED:
		return "paused", nil
	case runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_COMPLETE:
		return "complete", nil
	default:
		return "", fmt.Errorf("invalid Goal status %v", status)
	}
}
