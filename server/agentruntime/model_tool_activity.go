package runtime

import (
	"fmt"
	"time"
)

func (e *Engine) recordModelToolActivity(stepID string, completedAt time.Time) {
	if err := e.steer(stepID, steeringIntent{
		priority: steeringPriorityRuntimeEvent,
		items:    []steeringItem{{modelToolActivity: &completedAt}},
	}); err != nil {
		e.surfaceRunError(fmt.Errorf("record model/tool activity: %w", err))
	}
}
