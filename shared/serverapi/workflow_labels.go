package serverapi

type WorkflowLabelErrorReason string

const (
	WorkflowLabelErrorReasonProjectNotFound WorkflowLabelErrorReason = "project_not_found"
	WorkflowLabelErrorReasonLabelNotFound   WorkflowLabelErrorReason = "label_not_found"
	WorkflowLabelErrorReasonWrongProject    WorkflowLabelErrorReason = "wrong_project"
	WorkflowLabelErrorReasonInvalidFilter   WorkflowLabelErrorReason = "invalid_filter"
	WorkflowLabelErrorReasonInvalidMutation WorkflowLabelErrorReason = "invalid_mutation"
)

type WorkflowLabelError struct {
	Reason    WorkflowLabelErrorReason
	ProjectID *string
	TaskID    *string
	LabelID   *string
	Field     *string
	Limit     *int
}

func (e *WorkflowLabelError) Error() string {
	if e == nil {
		return "workflow label error"
	}
	return "workflow label error: " + string(e.Reason)
}
