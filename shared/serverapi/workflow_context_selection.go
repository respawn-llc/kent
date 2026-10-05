package serverapi

type WorkflowTaskContextSelectionRequiredError struct {
	TaskID string
}

func (e *WorkflowTaskContextSelectionRequiredError) Error() string {
	return "workflow task context selection required"
}
