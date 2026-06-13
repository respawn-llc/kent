package patch

import (
	"os"

	"core/server/tools/fsguard"
)

type OutsideWorkspaceErrorLabels = fsguard.ErrorLabels
type OutsideWorkspaceFailureFactory = fsguard.FailureFactory
type OutsideWorkspaceGuard = fsguard.Guard

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
	return fsguard.New(
		workspaceRoot,
		workspaceRootReal,
		workspaceRootInfo,
		workspaceOnly,
		allowOutsideWorkspace,
		fsguard.Approver(approver),
		sessionAllowed,
		setSessionAllowed,
		rejectionInstruction,
		errorLabels,
		failures,
		temporaryPathAllowed,
		func(req fsguard.Request, reason string) {
			if onApproved != nil {
				onApproved(req, reason)
			}
		},
	)
}
