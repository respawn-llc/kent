package clientui

import (
	"time"

	"core/shared/runtimeids"
)

type PendingAsk struct {
	ToolCallID             ToolCallID
	SessionID              runtimeids.SessionID
	ShortID                *runtimeids.SessionShortID
	StepID                 runtimeids.StepID
	Question               string
	Suggestions            []string
	RecommendedOptionIndex *int
	CreatedAt              time.Time
	Batch                  *QuestionBatch
}

type QuestionBatch struct {
	ToolCallIDs         []string
	UnmaterializedCount uint32
}
