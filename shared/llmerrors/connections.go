package llmerrors

import (
	"fmt"
	"strings"
	"time"

	"core/shared/config"
)

type UnavailableConnection struct {
	ID          config.ConnectionID
	AvailableAt *time.Time
}

type ConnectionsUnavailableError struct {
	Members []UnavailableConnection
	Causes  []error
}

func (e *ConnectionsUnavailableError) Error() string {
	var message strings.Builder
	message.WriteString("Configured connections are unavailable:")
	for _, member := range e.Members {
		fmt.Fprintf(&message, " %s", member.ID)
		if member.AvailableAt != nil {
			fmt.Fprintf(&message, " (available after %s)", member.AvailableAt.UTC().Format(time.RFC3339))
		} else {
			message.WriteString(" (already attempted)")
		}
	}
	message.WriteString(". Retry after cooldown or update the configured connections.")
	for _, cause := range e.Causes {
		fmt.Fprintf(&message, "\n%v", cause)
	}
	return message.String()
}

func (e *ConnectionsUnavailableError) Unwrap() []error { return e.Causes }
