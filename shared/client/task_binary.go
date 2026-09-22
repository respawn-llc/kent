package client

import (
	"fmt"

	"core/shared/protoapi"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"
)

type TaskListError struct {
	Failure *taskpb.ListError
}

func taskEntityGeneratedError[Failure interface {
	GetCode() string
	GetTaskNotFound() *taskpb.TaskNotFoundDetails
}](failure Failure) error {
	if failure.GetTaskNotFound() != nil {
		return serverapi.ErrWorkflowTaskNotFound
	}
	return generatedOperationFailure(failure.GetCode())
}

type taskMutationFailure interface {
	worktreeFailure
	GetTaskNotFound() *taskpb.TaskNotFoundDetails
	GetSelfTarget() *taskpb.SelfTargetDetails
}

func taskMutationGeneratedError[Failure taskMutationFailure](failure Failure) error {
	switch failure.GetCode() {
	case "task_not_found":
		return serverapi.ErrWorkflowTaskNotFound
	case "self_target":
		return &serverapi.WorkflowTaskMutationSelfTargetError{TaskID: failure.GetSelfTarget().TaskId}
	default:
		return worktreeError(failure)
	}
}

func taskExecutionGeneratedError[Failure interface {
	taskMutationFailure
	GetExecutionTargetResolution() *taskpb.ExecutionTargetResolutionDetails
	GetLockedExecutionTarget() *taskpb.LockedExecutionTargetDetails
}](failure Failure) error {
	switch failure.GetCode() {
	case "execution_target_resolution":
		return taskExecutionResolutionGeneratedError(failure.GetExecutionTargetResolution())
	case "locked_execution_target":
		return taskLockedTargetGeneratedError(failure.GetLockedExecutionTarget())
	default:
		return taskMutationGeneratedError(failure)
	}
}

func taskExecutionResolutionGeneratedError(detail *taskpb.ExecutionTargetResolutionDetails) error {
	code, err := protoapi.TaskExecutionResolutionCode.Decode(detail.Code)
	if err != nil {
		return err
	}
	return &serverapi.WorkflowExecutionTargetResolutionError{
		Code: serverapi.WorkflowExecutionTargetResolutionErrorCode(code), RequestedRef: detail.RequestedRef,
	}
}

func taskLockedTargetGeneratedError(detail *taskpb.LockedExecutionTargetDetails) error {
	cause, err := serverapi.WorkflowLockedTargetCause(detail.Cause)
	if err != nil {
		return err
	}
	return &serverapi.WorkflowLockedExecutionTargetError{Cause: cause}
}

func taskInitialBranchGeneratedError(detail *taskpb.InitialBranchDetails) error {
	reason, err := protoapi.TaskInitialBranchReason.Decode(detail.Reason)
	if err != nil {
		return err
	}
	return &serverapi.WorkflowTaskInitialBranchError{
		Reason: serverapi.WorkflowTaskInitialBranchErrorReason(reason), BranchName: detail.BranchName,
		Ref: detail.Ref, Remote: detail.Remote, ExistingBranchName: detail.ExistingBranchName,
	}
}

type TaskCreateError struct {
	Failure *taskpb.CreateError
}

type TaskDependencyError struct {
	Detail *taskpb.DependencyErrorDetails
}

func (e *TaskDependencyError) Error() string {
	reason, err := protoapi.TaskDependencyErrorReason.Decode(e.Detail.Reason)
	if err != nil {
		return err.Error()
	}
	if e.Detail.MissingTaskId != nil {
		return fmt.Sprintf("workflow task dependency error: %s (%s)", reason, *e.Detail.MissingTaskId)
	}
	return "workflow task dependency error: " + reason
}

func taskDependencyGeneratedError[Failure interface {
	GetCode() string
	GetDependency() *taskpb.DependencyErrorDetails
}](failure Failure) error {
	if failure.GetDependency() != nil {
		return &TaskDependencyError{Detail: failure.GetDependency()}
	}
	return generatedOperationFailure(failure.GetCode())
}

func (e *TaskCreateError) Error() string {
	return fmt.Sprintf("task creation failed with code %q", e.Failure.Code)
}

func (e *TaskListError) Error() string {
	return fmt.Sprintf("task list failed with code %q", e.Failure.Code)
}

func taskReadGeneratedError[Failure interface {
	GetCode() string
	GetLabelNotFound() *taskpb.LabelNotFoundDetails
	GetWrongProject() *taskpb.WrongProjectDetails
	GetInvalidFilter() *taskpb.InvalidFilterDetails
	GetProjectNotFound() *projectpb.ProjectNotFoundDetails
}](failure Failure) error {
	switch failure.GetCode() {
	case "label_not_found":
		return &WorkflowLabelError{Detail: failure.GetLabelNotFound()}
	case "wrong_project":
		return &WorkflowLabelError{Detail: failure.GetWrongProject()}
	case "invalid_filter":
		return &WorkflowLabelError{Detail: failure.GetInvalidFilter()}
	default:
		return projectNotFoundGeneratedError(failure.GetCode(), failure.GetProjectNotFound())
	}
}
