package serverapi

import (
	"fmt"
)

type WorkflowTaskInitialBranchErrorReason string

const (
	WorkflowTaskInitialBranchErrorReasonInvalidName                   WorkflowTaskInitialBranchErrorReason = "invalid_name"
	WorkflowTaskInitialBranchErrorReasonLocalCollision                WorkflowTaskInitialBranchErrorReason = "local_collision"
	WorkflowTaskInitialBranchErrorReasonRemoteTrackingCollision       WorkflowTaskInitialBranchErrorReason = "remote_tracking_collision"
	WorkflowTaskInitialBranchErrorReasonNoManagedTarget               WorkflowTaskInitialBranchErrorReason = "no_managed_target"
	WorkflowTaskInitialBranchErrorReasonOperationCannotCreateWorktree WorkflowTaskInitialBranchErrorReason = "operation_cannot_create_worktree"
	WorkflowTaskInitialBranchErrorReasonPostCreationMismatch          WorkflowTaskInitialBranchErrorReason = "post_creation_mismatch"
)

type WorkflowTaskInitialBranchError struct {
	Reason             WorkflowTaskInitialBranchErrorReason
	BranchName         string
	Ref                *string
	Remote             *string
	ExistingBranchName *string
}

func (e *WorkflowTaskInitialBranchError) Error() string {
	if e == nil {
		return "workflow task initial branch failed"
	}
	return fmt.Sprintf("workflow task initial branch %q failed: %s", e.BranchName, e.Reason)
}
