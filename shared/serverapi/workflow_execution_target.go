package serverapi

import (
	"fmt"

	definitionpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"

	"google.golang.org/protobuf/types/known/emptypb"
)

type WorkflowExecutionTargetMode string

const (
	WorkflowExecutionTargetModeNone                WorkflowExecutionTargetMode = "none"
	WorkflowExecutionTargetModeHead                WorkflowExecutionTargetMode = "head"
	WorkflowExecutionTargetModeDefaultBranch       WorkflowExecutionTargetMode = "default_branch"
	WorkflowExecutionTargetModeCustomRef           WorkflowExecutionTargetMode = "custom_ref"
	WorkflowExecutionTargetModeAskOnFirstExecution WorkflowExecutionTargetMode = "ask_on_first_execution"
)

type WorkflowExecutionTargetUnavailableCause string

const (
	WorkflowExecutionTargetUnavailableCauseInvalidRevision        WorkflowExecutionTargetUnavailableCause = "invalid_revision"
	WorkflowExecutionTargetUnavailableCauseNonCommit              WorkflowExecutionTargetUnavailableCause = "non_commit"
	WorkflowExecutionTargetUnavailableCauseDefaultBranchMissing   WorkflowExecutionTargetUnavailableCause = "default_branch_missing"
	WorkflowExecutionTargetUnavailableCauseDefaultBranchAmbiguous WorkflowExecutionTargetUnavailableCause = "default_branch_ambiguous"
	WorkflowExecutionTargetUnavailableCauseGitFailure             WorkflowExecutionTargetUnavailableCause = "git_failure"
)

type WorkflowExecutionTargetResolutionErrorCode string

const (
	WorkflowExecutionTargetResolutionErrorInvalidRevision WorkflowExecutionTargetResolutionErrorCode = "invalid_revision"
	WorkflowExecutionTargetResolutionErrorNonCommit       WorkflowExecutionTargetResolutionErrorCode = "non_commit"
	WorkflowExecutionTargetResolutionErrorGitFailure      WorkflowExecutionTargetResolutionErrorCode = "git_failure"
)

type WorkflowExecutionTargetResolutionError struct {
	Code         WorkflowExecutionTargetResolutionErrorCode
	RequestedRef string
}

type WorkflowLockedExecutionTargetCause string

const (
	WorkflowLockedExecutionTargetCauseDetachedHead     WorkflowLockedExecutionTargetCause = "detached_head"
	WorkflowLockedExecutionTargetCauseMissingBranch    WorkflowLockedExecutionTargetCause = "missing_branch"
	WorkflowLockedExecutionTargetCauseInvalidRoot      WorkflowLockedExecutionTargetCause = "invalid_root"
	WorkflowLockedExecutionTargetCauseRootInaccessible WorkflowLockedExecutionTargetCause = "root_inaccessible"
	WorkflowLockedExecutionTargetCauseConflict         WorkflowLockedExecutionTargetCause = "conflict"
	WorkflowLockedExecutionTargetCauseGitFailure       WorkflowLockedExecutionTargetCause = "git_failure"
)

type WorkflowLockedExecutionTargetError struct {
	Cause WorkflowLockedExecutionTargetCause
}

func (e *WorkflowLockedExecutionTargetError) Error() string {
	if e == nil {
		return "locked workflow execution target is unavailable"
	}
	return "locked workflow execution target is unavailable: " + string(e.Cause)
}

func (e *WorkflowExecutionTargetResolutionError) Error() string {
	if e == nil {
		return "workflow execution target resolution failed"
	}
	return "workflow execution target resolution failed: " + string(e.Code)
}

func NewWorkflowPolicyTargetSelectionRequirement() *taskpb.SelectionRequired {
	return &taskpb.SelectionRequired{
		Reason: &taskpb.SelectionRequired_PolicyRequiresSelection{PolicyRequiresSelection: &emptypb.Empty{}},
	}
}

func NewWorkflowOriginalTargetSelectionRequirement(cause WorkflowLockedExecutionTargetCause) *taskpb.SelectionRequired {
	return &taskpb.SelectionRequired{
		Reason: &taskpb.SelectionRequired_OriginalTargetUnavailable{
			OriginalTargetUnavailable: WorkflowLockedTargetDetails(cause),
		},
	}
}

func WorkflowLockedTargetDetails(cause WorkflowLockedExecutionTargetCause) *taskpb.LockedExecutionTargetDetails {
	return &taskpb.LockedExecutionTargetDetails{Cause: originalTargetCauses[cause]}
}

func WorkflowLockedTargetCause(value taskpb.LockedExecutionTargetCause) (WorkflowLockedExecutionTargetCause, error) {
	for cause, code := range originalTargetCauses {
		if code == value {
			return cause, nil
		}
	}
	return "", fmt.Errorf("unknown original target unavailable cause: %d", value)
}

func WorkflowUnavailableTargetCause(value taskpb.ExecutionTargetUnavailableCause) (WorkflowExecutionTargetUnavailableCause, error) {
	for cause, code := range unavailableTargetCauses {
		if code == value {
			return cause, nil
		}
	}
	return "", fmt.Errorf("unknown configured target unavailable cause: %d", value)
}

func NewWorkflowConfiguredTargetSelectionRequirement(mode WorkflowExecutionTargetMode, requestedRef *string, cause WorkflowExecutionTargetUnavailableCause) *taskpb.SelectionRequired {
	return &taskpb.SelectionRequired{
		Reason: &taskpb.SelectionRequired_ConfiguredTargetUnavailable{
			ConfiguredTargetUnavailable: WorkflowConfiguredTargetUnavailableDetails(mode, requestedRef, cause),
		},
	}
}

func WorkflowConfiguredTargetUnavailableDetails(mode WorkflowExecutionTargetMode, requestedRef *string, cause WorkflowExecutionTargetUnavailableCause) *taskpb.ConfiguredExecutionTargetUnavailableDetails {
	return &taskpb.ConfiguredExecutionTargetUnavailableDetails{
		Mode: configuredTargetModes[mode], RequestedRef: requestedRef, Cause: unavailableTargetCauses[cause],
	}
}

var originalTargetCauses = map[WorkflowLockedExecutionTargetCause]taskpb.LockedExecutionTargetCause{
	WorkflowLockedExecutionTargetCauseDetachedHead:     taskpb.LockedExecutionTargetCause_LOCKED_EXECUTION_TARGET_CAUSE_DETACHED_HEAD,
	WorkflowLockedExecutionTargetCauseInvalidRoot:      taskpb.LockedExecutionTargetCause_LOCKED_EXECUTION_TARGET_CAUSE_INVALID_ROOT,
	WorkflowLockedExecutionTargetCauseRootInaccessible: taskpb.LockedExecutionTargetCause_LOCKED_EXECUTION_TARGET_CAUSE_ROOT_INACCESSIBLE,
	WorkflowLockedExecutionTargetCauseMissingBranch:    taskpb.LockedExecutionTargetCause_LOCKED_EXECUTION_TARGET_CAUSE_MISSING_BRANCH,
	WorkflowLockedExecutionTargetCauseConflict:         taskpb.LockedExecutionTargetCause_LOCKED_EXECUTION_TARGET_CAUSE_CONFLICT,
	WorkflowLockedExecutionTargetCauseGitFailure:       taskpb.LockedExecutionTargetCause_LOCKED_EXECUTION_TARGET_CAUSE_GIT_FAILURE,
}

var configuredTargetModes = map[WorkflowExecutionTargetMode]definitionpb.ExecutionTargetMode{
	WorkflowExecutionTargetModeNone:          definitionpb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_NONE,
	WorkflowExecutionTargetModeHead:          definitionpb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_HEAD,
	WorkflowExecutionTargetModeDefaultBranch: definitionpb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_DEFAULT_BRANCH,
	WorkflowExecutionTargetModeCustomRef:     definitionpb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_CUSTOM_REF,
}

var unavailableTargetCauses = map[WorkflowExecutionTargetUnavailableCause]taskpb.ExecutionTargetUnavailableCause{
	WorkflowExecutionTargetUnavailableCauseInvalidRevision:        taskpb.ExecutionTargetUnavailableCause_EXECUTION_TARGET_UNAVAILABLE_CAUSE_INVALID_REVISION,
	WorkflowExecutionTargetUnavailableCauseNonCommit:              taskpb.ExecutionTargetUnavailableCause_EXECUTION_TARGET_UNAVAILABLE_CAUSE_NON_COMMIT,
	WorkflowExecutionTargetUnavailableCauseDefaultBranchMissing:   taskpb.ExecutionTargetUnavailableCause_EXECUTION_TARGET_UNAVAILABLE_CAUSE_DEFAULT_BRANCH_MISSING,
	WorkflowExecutionTargetUnavailableCauseDefaultBranchAmbiguous: taskpb.ExecutionTargetUnavailableCause_EXECUTION_TARGET_UNAVAILABLE_CAUSE_DEFAULT_BRANCH_AMBIGUOUS,
	WorkflowExecutionTargetUnavailableCauseGitFailure:             taskpb.ExecutionTargetUnavailableCause_EXECUTION_TARGET_UNAVAILABLE_CAUSE_GIT_FAILURE,
}
