package clientui

import (
	"time"

	"core/shared/runtimeids"
)

type ProjectAvailability string

const (
	ProjectAvailabilityAvailable    ProjectAvailability = "available"
	ProjectAvailabilityMissing      ProjectAvailability = "missing"
	ProjectAvailabilityInaccessible ProjectAvailability = "inaccessible"
	ProjectAvailabilityUnlinked     ProjectAvailability = "unlinked"
)

type ProjectSummary struct {
	ProjectID   string
	DisplayName string
	RootPath    string
	UpdatedAt   time.Time
}

type SessionSummary struct {
	SessionID          runtimeids.SessionID       `json:"session_id"`
	ShortID            *runtimeids.SessionShortID `json:"short_id,omitempty"`
	Name               *string                    `json:"name,omitempty"`
	FirstPromptPreview *string                    `json:"first_prompt_preview,omitempty"`
	UpdatedAt          time.Time                  `json:"updated_at"`
}
