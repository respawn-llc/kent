package serverapi

import (
	"errors"
	"fmt"
	"strings"
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

func (e *WorkflowTaskInitialBranchError) Validate() error {
	if e == nil {
		return errors.New("workflow task initial branch error is required")
	}
	if strings.TrimSpace(e.BranchName) == "" {
		return errors.New("workflow task initial branch error branch name is required")
	}
	switch e.Reason {
	case WorkflowTaskInitialBranchErrorReasonInvalidName,
		WorkflowTaskInitialBranchErrorReasonNoManagedTarget,
		WorkflowTaskInitialBranchErrorReasonOperationCannotCreateWorktree:
		if e.Ref != nil || e.Remote != nil || e.ExistingBranchName != nil {
			return errors.New("workflow task initial branch error has inapplicable facts")
		}
	case WorkflowTaskInitialBranchErrorReasonLocalCollision:
		if !validInitialBranchErrorString(e.Ref) || e.Remote != nil || e.ExistingBranchName != nil {
			return errors.New("workflow task local branch collision facts are invalid")
		}
	case WorkflowTaskInitialBranchErrorReasonRemoteTrackingCollision:
		if !validInitialBranchErrorString(e.Ref) || !validInitialBranchErrorString(e.Remote) || e.ExistingBranchName != nil {
			return errors.New("workflow task remote-tracking branch collision facts are invalid")
		}
	case WorkflowTaskInitialBranchErrorReasonPostCreationMismatch:
		if !validInitialBranchErrorString(e.Ref) || e.Remote != nil || !validInitialBranchErrorString(e.ExistingBranchName) {
			return errors.New("workflow task post-creation branch mismatch facts are invalid")
		}
	default:
		return errors.New("workflow task initial branch error reason is invalid")
	}
	return nil
}

func validInitialBranchErrorString(value *string) bool {
	return value != nil && strings.TrimSpace(*value) != ""
}
