package client

import (
	"context"
	"fmt"

	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"

	"google.golang.org/protobuf/proto"
)

// WorkflowLabelError retains the generated detail selected by the operation.
// Task creation's remaining JSON label error is a separate containing contract.
type WorkflowLabelError struct {
	Detail proto.Message
}

func workflowLabelRequestValidation(projectID string) func(error) error {
	return func(err error) error {
		if detail := protoapi.WorkflowProjectLabelValidationDetail(projectID, err); detail != nil {
			return &WorkflowLabelError{Detail: detail}
		}
		return err
	}
}

func (e *WorkflowLabelError) Error() string {
	var reason string
	switch detail := e.Detail.(type) {
	case *pb.LabelInvalidNameDetails:
		reason = "invalid_name"
	case *pb.LabelNameConflictDetails:
		reason = "name_conflict"
	case *pb.LabelCatalogLimitDetails:
		reason = "catalog_limit"
	case *pb.LabelProjectNotFoundDetails:
		reason = "project_not_found"
	case *pb.LabelNotFoundDetails:
		reason = "label_not_found"
	case *pb.LabelInvalidMutationDetails:
		reason = "invalid_mutation"
	case *taskpb.TaskNotFoundDetails:
		reason = "task_not_found"
	case *taskpb.LabelErrorDetails:
		switch detail.Reason {
		case taskpb.LabelErrorReason_LABEL_ERROR_REASON_INVALID_NAME:
			reason = "invalid_name"
		case taskpb.LabelErrorReason_LABEL_ERROR_REASON_NAME_CONFLICT:
			reason = "name_conflict"
		case taskpb.LabelErrorReason_LABEL_ERROR_REASON_CATALOG_LIMIT:
			reason = "catalog_limit"
		case taskpb.LabelErrorReason_LABEL_ERROR_REASON_PROJECT_NOT_FOUND:
			reason = "project_not_found"
		case taskpb.LabelErrorReason_LABEL_ERROR_REASON_LABEL_NOT_FOUND:
			reason = "label_not_found"
		case taskpb.LabelErrorReason_LABEL_ERROR_REASON_TASK_NOT_FOUND:
			reason = "task_not_found"
		case taskpb.LabelErrorReason_LABEL_ERROR_REASON_WRONG_PROJECT:
			reason = "wrong_project"
		case taskpb.LabelErrorReason_LABEL_ERROR_REASON_INVALID_FILTER:
			reason = "invalid_filter"
		case taskpb.LabelErrorReason_LABEL_ERROR_REASON_INVALID_MUTATION:
			reason = "invalid_mutation"
		default:
			return fmt.Sprintf("unsupported Workflow label error reason %v", detail.Reason)
		}
	default:
		return fmt.Sprintf("unsupported Workflow label error detail %T", e.Detail)
	}
	return "workflow label error: " + reason
}

func (c *Remote) GetWorkflowTaskLabels(ctx context.Context, req *taskpb.LabelsGetRequest) (*taskpb.LabelsGetSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_read_proto.Services().ByName("TaskLabelReadService").Methods().ByName("Get")
	response, err := callGeneratedBinary(c, ctx, method, req, &taskpb.LabelsGetResult{},
		func(failure *taskpb.LabelsGetError) error {
			if detail := failure.GetTaskNotFound(); detail != nil {
				return &WorkflowLabelError{Detail: detail}
			}
			return generatedOperationFailure(failure.Code)
		})
	if err != nil {
		return nil, err
	}
	if response.Assignment.TaskId != req.TaskId {
		return nil, fmt.Errorf("label assignment Task %q does not match request %q", response.Assignment.TaskId, req.TaskId)
	}
	return response, nil
}

func (c *Remote) UpdateWorkflowTaskLabels(ctx context.Context, req *taskpb.LabelsUpdateRequest) (*taskpb.LabelsUpdateSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskLabelService").Methods().ByName("Update")
	response, err := callGeneratedBinary(c, ctx, method, req, &taskpb.LabelsUpdateResult{},
		func(failure *taskpb.LabelsUpdateError) error {
			switch detail := failure.Detail.(type) {
			case *taskpb.LabelsUpdateError_TaskNotFound:
				return &WorkflowLabelError{Detail: detail.TaskNotFound}
			case *taskpb.LabelsUpdateError_Label:
				return &WorkflowLabelError{Detail: detail.Label}
			default:
				return generatedOperationFailure(failure.Code)
			}
		}, func(err error) error {
			if detail := protoapi.WorkflowTaskLabelValidationDetail(req.GetTaskId(), err); detail != nil {
				return &WorkflowLabelError{Detail: detail}
			}
			return err
		})
	if err != nil {
		return nil, err
	}
	if response.Assignment.TaskId != req.TaskId {
		return nil, fmt.Errorf("label assignment Task %q does not match request %q", response.Assignment.TaskId, req.TaskId)
	}
	return response, nil
}

func (c *Remote) CreateWorkflowProjectLabel(ctx context.Context, req *pb.ProjectLabelCreateRequest) (*pb.ProjectLabelCreateSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("ProjectLabelService", "Create"), req, &pb.ProjectLabelCreateResult{},
		func(failure *pb.ProjectLabelCreateError) error {
			switch detail := failure.Detail.(type) {
			case *pb.ProjectLabelCreateError_InvalidName:
				return &WorkflowLabelError{Detail: detail.InvalidName}
			case *pb.ProjectLabelCreateError_NameConflict:
				return &WorkflowLabelError{Detail: detail.NameConflict}
			case *pb.ProjectLabelCreateError_CatalogLimit:
				return &WorkflowLabelError{Detail: detail.CatalogLimit}
			case *pb.ProjectLabelCreateError_ProjectNotFound:
				return &WorkflowLabelError{Detail: detail.ProjectNotFound}
			case *pb.ProjectLabelCreateError_InvalidMutation:
				return &WorkflowLabelError{Detail: detail.InvalidMutation}
			default:
				return generatedOperationFailure(failure.Code)
			}
		}, workflowLabelRequestValidation(req.GetProjectId()))
}

func (c *Remote) ListWorkflowProjectLabels(ctx context.Context, req *pb.ProjectLabelCatalogRequest) (*pb.ProjectLabelCatalogSuccess, error) {
	response, err := callGeneratedBinary(c, ctx, workflowMethod("ProjectLabelService", "List"), req, &pb.ProjectLabelCatalogResult{},
		func(failure *pb.ProjectLabelCatalogError) error {
			if detail := failure.GetProjectNotFound(); detail != nil {
				return &WorkflowLabelError{Detail: detail}
			}
			return generatedOperationFailure(failure.Code)
		})
	if err != nil {
		return nil, err
	}
	if response.Catalog.ProjectId != req.ProjectId {
		return nil, fmt.Errorf("label catalog Project %q does not match request %q", response.Catalog.ProjectId, req.ProjectId)
	}
	return response, nil
}

func (c *Remote) RenameWorkflowProjectLabel(ctx context.Context, req *pb.ProjectLabelRenameRequest) (*pb.ProjectLabelRenameSuccess, error) {
	response, err := callGeneratedBinary(c, ctx, workflowMethod("ProjectLabelService", "Rename"), req, &pb.ProjectLabelRenameResult{},
		func(failure *pb.ProjectLabelRenameError) error {
			switch detail := failure.Detail.(type) {
			case *pb.ProjectLabelRenameError_InvalidName:
				return &WorkflowLabelError{Detail: detail.InvalidName}
			case *pb.ProjectLabelRenameError_NameConflict:
				return &WorkflowLabelError{Detail: detail.NameConflict}
			case *pb.ProjectLabelRenameError_ProjectNotFound:
				return &WorkflowLabelError{Detail: detail.ProjectNotFound}
			case *pb.ProjectLabelRenameError_LabelNotFound:
				return &WorkflowLabelError{Detail: detail.LabelNotFound}
			case *pb.ProjectLabelRenameError_InvalidMutation:
				return &WorkflowLabelError{Detail: detail.InvalidMutation}
			default:
				return generatedOperationFailure(failure.Code)
			}
		}, workflowLabelRequestValidation(req.GetProjectId()))
	if err != nil {
		return nil, err
	}
	if response.Label.Id != req.LabelId {
		return nil, fmt.Errorf("renamed label %q does not match request %q", response.Label.Id, req.LabelId)
	}
	return response, nil
}

func (c *Remote) DeleteWorkflowProjectLabel(ctx context.Context, req *pb.ProjectLabelDeleteRequest) (*pb.ProjectLabelDeleteSuccess, error) {
	response, err := callGeneratedBinary(c, ctx, workflowMethod("ProjectLabelService", "Delete"), req, &pb.ProjectLabelDeleteResult{},
		func(failure *pb.ProjectLabelDeleteError) error {
			switch detail := failure.Detail.(type) {
			case *pb.ProjectLabelDeleteError_ProjectNotFound:
				return &WorkflowLabelError{Detail: detail.ProjectNotFound}
			case *pb.ProjectLabelDeleteError_LabelNotFound:
				return &WorkflowLabelError{Detail: detail.LabelNotFound}
			case *pb.ProjectLabelDeleteError_InvalidMutation:
				return &WorkflowLabelError{Detail: detail.InvalidMutation}
			default:
				return generatedOperationFailure(failure.Code)
			}
		}, workflowLabelRequestValidation(req.GetProjectId()))
	if err != nil {
		return nil, err
	}
	if response.LabelId != req.LabelId {
		return nil, fmt.Errorf("deleted label %q does not match request %q", response.LabelId, req.LabelId)
	}
	return response, nil
}

func (c *Remote) ReorderWorkflowProjectLabels(ctx context.Context, req *pb.ProjectLabelReorderRequest) (*pb.ProjectLabelReorderSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("ProjectLabelService", "Reorder"), req, &pb.ProjectLabelReorderResult{},
		func(failure *pb.ProjectLabelReorderError) error {
			switch detail := failure.Detail.(type) {
			case *pb.ProjectLabelReorderError_ProjectNotFound:
				return &WorkflowLabelError{Detail: detail.ProjectNotFound}
			case *pb.ProjectLabelReorderError_InvalidMutation:
				return &WorkflowLabelError{Detail: detail.InvalidMutation}
			default:
				return generatedOperationFailure(failure.Code)
			}
		}, workflowLabelRequestValidation(req.GetProjectId()))
}
