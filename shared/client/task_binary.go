package client

import (
	"fmt"

	projectpb "core/shared/protoapi/gen/kent/api/project"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
)

type TaskListError struct {
	Failure *taskpb.ListError
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
