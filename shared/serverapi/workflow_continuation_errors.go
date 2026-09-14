package serverapi

import "fmt"

type WorkflowContinuationRejectionReason string

const (
	WorkflowContinuationWaitingForApproval WorkflowContinuationRejectionReason = "waiting_for_transition_approval"
	WorkflowContinuationNotCurrentNode     WorkflowContinuationRejectionReason = "not_current_workflow_node"
)

type WorkflowContinuationRejectionError struct {
	TaskID string
	Reason WorkflowContinuationRejectionReason
}

func (e *WorkflowContinuationRejectionError) Error() string {
	if e == nil {
		return "workflow continuation was rejected"
	}
	return fmt.Sprintf("workflow continuation for Task %q was rejected: %s", e.TaskID, e.Reason)
}
