package clientui

import "time"

type PendingPromptEventType string

const (
	PendingPromptEventPending  PendingPromptEventType = "pending"
	PendingPromptEventResolved PendingPromptEventType = "resolved"
	PendingPromptEventSnapshot PendingPromptEventType = "snapshot_complete"
)

type PendingPromptEvent struct {
	Sequence               uint64
	Type                   PendingPromptEventType
	PromptID               string
	SessionID              string
	Question               string
	Suggestions            []string
	RecommendedOptionIndex int
	Approval               bool
	ApprovalOptions        []ApprovalOption
	CreatedAt              time.Time
}

func (e PendingPromptEvent) IsZero() bool {
	return e.Type == "" && e.PromptID == ""
}
