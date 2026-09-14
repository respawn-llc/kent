package client

import (
	"fmt"

	"core/shared/serverapi"
)

func FormatWorkflowContinuationRejection(
	rejection serverapi.WorkflowContinuationRejectionError,
) string {
	switch rejection.Reason {
	case serverapi.WorkflowContinuationWaitingForApproval:
		return fmt.Sprintf(
			"Task %q is waiting for Transition Approval for the selected Workflow Node; review the pending Approval.",
			rejection.TaskID,
		)
	case serverapi.WorkflowContinuationNotCurrentNode:
		return fmt.Sprintf(
			"Task %q no longer has this Session on a Current Node; inspect the Task's current state.",
			rejection.TaskID,
		)
	default:
		return fmt.Sprintf("Workflow continuation for Task %q was rejected.", rejection.TaskID)
	}
}
