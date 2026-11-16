package runtime

import (
	"fmt"
	"time"
)

func (e *Engine) recordAgentActivity(stepID string, completedAt time.Time) {
	if err := e.steer(stepID, steeringIntent{
		priority: steeringPriorityRuntimeEvent,
		items:    []steeringItem{{agentActivity: &completedAt}},
	}); err != nil {
		e.surfaceRunError(fmt.Errorf("record model/tool activity: %w", err))
	}
}
