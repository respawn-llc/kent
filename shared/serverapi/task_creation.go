package serverapi

import "fmt"

// CommittedTaskCreationError identifies a persisted Task when response projection fails.
type CommittedTaskCreationError struct {
	TaskID    string
	ShortID   string
	ProjectID string
	Cause     error
}

func (e *CommittedTaskCreationError) Error() string {
	return fmt.Sprintf("Task %s (%s) was created, but loading its response failed: %v", e.ShortID, e.TaskID, e.Cause)
}
func (e *CommittedTaskCreationError) Unwrap() error { return e.Cause }
