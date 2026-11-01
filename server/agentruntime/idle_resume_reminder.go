package runtime

import (
	"fmt"
	"strings"
	"time"
)

func idleResumeEligible(thresholdSeconds *int, lastActivity *time.Time, now time.Time) bool {
	return thresholdSeconds != nil && lastActivity != nil &&
		now.Sub(*lastActivity).Seconds() > float64(*thresholdSeconds)
}

func formatIdleResumeDuration(elapsed time.Duration) string {
	minutes := int64(elapsed / time.Minute)
	var output strings.Builder
	if days := minutes / (24 * 60); days > 0 {
		fmt.Fprintf(&output, "%dd", days)
	}
	if hours := minutes / 60 % 24; hours > 0 {
		fmt.Fprintf(&output, "%dh", hours)
	}
	if remaining := minutes % 60; remaining > 0 {
		fmt.Fprintf(&output, "%dm", remaining)
	}
	if output.Len() == 0 {
		return "0m"
	}
	return output.String()
}
