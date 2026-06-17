package patch

import (
	"os"

	"core/server/tools"
)

type OutsideWorkspaceErrorLabels = tools.FSGuardErrorLabels
type OutsideWorkspaceFailureFactory = tools.FSGuardFailureFactory
type OutsideWorkspaceGuard = tools.FSGuard

func NewOutsideWorkspaceGuard(workspaceRoot string, workspaceRootReal string, workspaceRootInfo os.FileInfo, workspaceOnly bool, allowOutsideWorkspace bool, approver OutsideWorkspaceApprover, sessionAllowed func() bool, setSessionAllowed func(bool), rejectionInstruction string, errorLabels OutsideWorkspaceErrorLabels, failures OutsideWorkspaceFailureFactory, temporaryPathAllowed func(string) bool, onApproved func(OutsideWorkspaceRequest, string)) OutsideWorkspaceGuard {
	if failures.NoPermission == nil {
		failures.NoPermission = noPermissionFailure
	}
	if failures.DefaultApprovalFailed == nil {
		failures.DefaultApprovalFailed = approvalFailedFailure
	}
	if failures.DefaultUserDenied == nil {
		failures.DefaultUserDenied = userDeniedFailure
	}
	return tools.NewFSGuard(
		workspaceRoot,
		workspaceRootReal,
		workspaceRootInfo,
		workspaceOnly,
		allowOutsideWorkspace,
		tools.FSGuardApprover(approver),
		sessionAllowed,
		setSessionAllowed,
		rejectionInstruction,
		errorLabels,
		failures,
		temporaryPathAllowed,
		func(req tools.FSGuardRequest, reason string) {
			if onApproved != nil {
				onApproved(req, reason)
			}
		},
	)
}
