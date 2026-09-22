package serverapi

import (
	"core/shared/tasksearchtext"
)

const (
	TaskSearchDefaultContext = 20

	TaskSearchDefaultPageSize = 100
)

func ValidateTaskSearchLiteralQuery(query string) error {
	if tasksearchtext.NormalizedLiteralRuneCount(query) < 3 {
		return &TaskSearchError{Reason: TaskSearchErrorReasonNormalizedTooShort}
	}
	return nil
}

type TaskSearchErrorReason string

const (
	TaskSearchErrorReasonNormalizedTooShort TaskSearchErrorReason = "normalized_too_short"
)

type TaskSearchError struct {
	Reason TaskSearchErrorReason `json:"reason"`
}

func (e *TaskSearchError) Error() string {
	if e == nil {
		return "task search error"
	}
	return "task search error: " + string(e.Reason)
}
