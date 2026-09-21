package transport

import (
	"errors"

	"core/server/workflow/label"
	"core/server/workflowstore"
	"core/shared/apicontract"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"

	"google.golang.org/protobuf/proto"
)

func registerWorkflowLabelGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	service := pb.File_kent_api_workflow_definition_workflow_definition_proto.Services().ByName("ProjectLabelService")
	return errors.Join(
		registerWorkflowUnary(bindings, service, "Create",
			func() *pb.ProjectLabelCreateRequest { return &pb.ProjectLabelCreateRequest{} },
			apicontract.WorkflowService.CreateWorkflowProjectLabel, binaryWorkflowProjectLabelFailure[*pb.ProjectLabelCreateRequest],
			binaryWorkflowProjectLabelValidation[*pb.ProjectLabelCreateRequest]),
		registerWorkflowUnary(bindings, service, "List",
			func() *pb.ProjectLabelCatalogRequest { return &pb.ProjectLabelCatalogRequest{} },
			apicontract.WorkflowService.ListWorkflowProjectLabels, binaryWorkflowProjectLabelFailure[*pb.ProjectLabelCatalogRequest]),
		registerWorkflowUnary(bindings, service, "Rename",
			func() *pb.ProjectLabelRenameRequest { return &pb.ProjectLabelRenameRequest{} },
			apicontract.WorkflowService.RenameWorkflowProjectLabel, binaryWorkflowProjectLabelFailure[*pb.ProjectLabelRenameRequest],
			binaryWorkflowProjectLabelValidation[*pb.ProjectLabelRenameRequest]),
		registerWorkflowUnary(bindings, service, "Delete",
			func() *pb.ProjectLabelDeleteRequest { return &pb.ProjectLabelDeleteRequest{} },
			apicontract.WorkflowService.DeleteWorkflowProjectLabel, binaryWorkflowProjectLabelFailure[*pb.ProjectLabelDeleteRequest],
			binaryWorkflowProjectLabelValidation[*pb.ProjectLabelDeleteRequest]),
		registerWorkflowUnary(bindings, service, "Reorder",
			func() *pb.ProjectLabelReorderRequest { return &pb.ProjectLabelReorderRequest{} },
			apicontract.WorkflowService.ReorderWorkflowProjectLabels, binaryWorkflowProjectLabelFailure[*pb.ProjectLabelReorderRequest],
			binaryWorkflowProjectLabelValidation[*pb.ProjectLabelReorderRequest]),
		registerWorkflowUnary(bindings, taskpb.File_kent_api_workflow_task_read_proto.Services().ByName("TaskLabelReadService"), "Get",
			func() *taskpb.LabelsGetRequest { return &taskpb.LabelsGetRequest{} },
			apicontract.WorkflowService.GetWorkflowTaskLabels, binaryWorkflowTaskLabelFailure[*taskpb.LabelsGetRequest]),
		registerWorkflowUnary(bindings, taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskLabelService"), "Update",
			func() *taskpb.LabelsUpdateRequest { return &taskpb.LabelsUpdateRequest{} },
			apicontract.WorkflowService.UpdateWorkflowTaskLabels, binaryWorkflowTaskLabelFailure[*taskpb.LabelsUpdateRequest],
			func(request *taskpb.LabelsUpdateRequest, err error) proto.Message {
				if detail := protoapi.WorkflowTaskLabelValidationDetail(request.GetTaskId(), err); detail != nil {
					return detail
				}
				return nil
			}),
	)
}

func binaryWorkflowTaskLabelFailure[Request interface{ GetTaskId() string }](request Request, err error) proto.Message {
	if errors.Is(err, serverapi.ErrServerAuthRequired) {
		return &authpb.AuthRequiredDetails{}
	}
	var task workflowstore.TaskLabelTaskNotFoundError
	var missing workflowstore.TaskLabelNotFoundError
	var foreign workflowstore.TaskLabelWrongProjectError
	var mutation workflowstore.TaskLabelMutationError
	switch {
	case errors.As(err, &task):
		return &taskpb.TaskNotFoundDetails{TaskId: &task.TaskID}
	case errors.As(err, &missing):
		return &taskpb.LabelErrorDetails{
			Reason: taskpb.LabelErrorReason_LABEL_ERROR_REASON_LABEL_NOT_FOUND, LabelId: &missing.LabelID,
		}
	case errors.As(err, &foreign):
		return &taskpb.LabelErrorDetails{
			Reason:    taskpb.LabelErrorReason_LABEL_ERROR_REASON_WRONG_PROJECT,
			ProjectId: &foreign.TaskProjectID, TaskId: &foreign.TaskID, LabelId: &foreign.LabelID,
		}
	case errors.As(err, &mutation):
		taskID := request.GetTaskId()
		field := mutation.Field
		if mutation.Reason == workflowstore.TaskLabelMutationOverlap {
			field = "remove_label_ids"
		}
		detail := &taskpb.LabelErrorDetails{
			Reason: taskpb.LabelErrorReason_LABEL_ERROR_REASON_INVALID_MUTATION,
			TaskId: &taskID, LabelId: mutation.LabelID, Field: &field,
		}
		if mutation.Limit != nil {
			limit := int32(*mutation.Limit)
			detail.Limit = &limit
		}
		return detail
	default:
		return binaryInternalFailure(err)
	}
}

func binaryWorkflowProjectLabelValidation[Request interface{ GetProjectId() string }](request Request, err error) proto.Message {
	return protoapi.WorkflowProjectLabelValidationDetail(request.GetProjectId(), err)
}

func binaryWorkflowProjectLabelFailure[Request interface{ GetProjectId() string }](request Request, err error) proto.Message {
	if errors.Is(err, serverapi.ErrServerAuthRequired) {
		return &authpb.AuthRequiredDetails{}
	}
	var name *label.NameError
	var conflict workflowstore.ProjectLabelNameConflictError
	var limit workflowstore.ProjectLabelLimitError
	var missing workflowstore.ProjectLabelNotFoundError
	var order workflowstore.ProjectLabelOrderError
	switch {
	case errors.As(err, &name):
		return &pb.LabelInvalidNameDetails{ProjectId: request.GetProjectId(), Field: "name"}
	case errors.As(err, &conflict):
		return &pb.LabelNameConflictDetails{ProjectId: conflict.ProjectID}
	case errors.As(err, &limit):
		return &pb.LabelCatalogLimitDetails{ProjectId: limit.ProjectID, Limit: int32(limit.Limit)}
	case errors.As(err, &missing):
		return &pb.LabelNotFoundDetails{ProjectId: &missing.ProjectID, LabelId: missing.LabelID}
	case errors.As(err, &order):
		return &pb.LabelInvalidMutationDetails{ProjectId: &order.ProjectID, Field: "label_ids"}
	case errors.Is(err, serverapi.ErrProjectNotFound):
		return &pb.LabelProjectNotFoundDetails{ProjectId: request.GetProjectId()}
	default:
		return binaryInternalFailure(err)
	}
}
